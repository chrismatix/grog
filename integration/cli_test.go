package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/sergi/go-diff/diffmatchpatch"
	"gopkg.in/yaml.v3"
)

var updateAll = flag.Bool("update-all", false, "update all fixture files")
var update = flag.String("update", "", "update only the specified fixture file")

var binaryName = "dist/grog"

var binaryPath = ""

func fixturePath(t *testing.T, testName string) string {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("problems recovering caller information")
	}

	return filepath.Join(filepath.Dir(filename), "fixtures", testName+".txt")
}

func writeFixture(t *testing.T, fixture string, content []byte) {
	err := os.WriteFile(fixturePath(t, fixture), content, 0644)
	if err != nil {
		t.Fatalf(
			"could not write fixture %s: %v",
			fixturePath(t, fixture),
			err)
	}
}

func loadFixture(t *testing.T, testName string) (string, error) {
	content, err := os.ReadFile(fixturePath(t, testName))
	return string(content), err
}

type TestTable struct {
	Name  string `yaml:"name"`
	Repo  string `yaml:"repo"`
	Cases []TestStep

	// Only run this test when REQUIRES_CREDENTIALS is set
	// (for tests that require cloud credentials)
	RequiresCredentials bool `yaml:"requires_credentials"`
	// Whether to skip the clean step
	SkipClean bool `yaml:"skip_clean"`
}

// TestStep defines a single test step
// It can either run a grog command using GrogArgs or some other command using SetupCommand
// If SetupCommand is defined and GrogArgs is empty the step will only run the setup command (without fixtures)
type TestStep struct {
	// Names must be unique as they determine the fixture file name
	Name         string   `yaml:"name"`
	SetupCommand string   `yaml:"setup_command"`
	GrogArgs     []string `yaml:"grog_args"`
	EnvVars      []string `yaml:"env_vars"`
	ExpectFail   bool     `yaml:"expect_fail"`
	// Whether to run this test step against a temporary repo directory.
	TempDir bool `yaml:"temp_dir"`
	// Some tests have machine-specific outputs which makes
	// fixture checking difficult.
	SkipFixture bool `yaml:"skip_fixture"`
}

func TestCliScenarios(t *testing.T) {
	// Read all .yaml files from the cases/ directory
	files, err := filepath.Glob("integration/test_scenarios/*.yaml")
	if err != nil {
		t.Fatalf("could not read cases directory: %v", err)
	}

	var testTables []TestTable
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("could not read %s: %v", file, err)
		}

		var testTable TestTable
		err = yaml.Unmarshal(data, &testTable)
		if err != nil {
			t.Fatalf("could not unmarshal %s: %v", file, err)
		}

		testTables = append(testTables, testTable)
	}

	if len(testTables) == 0 {
		t.Fatalf("no test tables found")
	}

	repoCounts := make(map[string]int)
	for _, tt := range testTables {
		repoCounts[tt.Repo]++
	}

	for _, tt := range testTables {
		if tt.RequiresCredentials {
			if os.Getenv("REQUIRES_CREDENTIALS") == "" {
				continue
			}
		}

		tt := tt

		t.Run(tt.Name, func(t *testing.T) {
			if repoCounts[tt.Repo] == 1 {
				t.Parallel()
			}

			// Create a per-test coverage directory to avoid race conditions
			// when parallel tests write coverage metadata files.
			baseCoverDir, err := getCoverDir()
			if err != nil {
				t.Fatalf("could not get cover dir: %v", err)
			}
			sanitized := strings.ReplaceAll(tt.Name, "/", "_")
			sanitized = strings.ReplaceAll(sanitized, " ", "_")
			coverDir := filepath.Join(baseCoverDir, sanitized)
			if err := os.MkdirAll(coverDir, 0755); err != nil {
				t.Fatalf("could not create cover dir: %v", err)
			}

			// Clear repository cache
			if !tt.SkipClean {
				output, err := runBinary([]string{"clean"}, tt.Repo, []string{}, coverDir)
				if err != nil {
					t.Fatalf(
						"could not run `grog clean` on repo %s: %v\nCommand output:\n%s",
						tt.Repo,
						err,
						output)
				}
			}

			testCaseNames := make(map[string]bool)
			for _, tc := range tt.Cases {
				repoPath := tt.Repo
				cleanupFilePath, err := repoCleanupFilePath(tt.Repo)
				if err != nil {
					t.Fatalf("could not get cleanup file path for repo %s: %v", tt.Repo, err)
				}
				setupEnvVars := []string{"GROGTEST_CLEANUP_FILE=" + cleanupFilePath}
				if tc.TempDir {
					repoPath = t.TempDir()
					setupEnvVars = append(setupEnvVars, "GROGTEST_TEMP_DIR="+repoPath)
				}

				// Run the setup command if it is defined
				if tc.SetupCommand != "" {
					output, err := runSetupCommand(tc.SetupCommand, tt.Repo, setupEnvVars)
					registerCleanupFile(t, cleanupFilePath)
					if err != nil {
						t.Fatalf(
							"could not run setup command %s on repo %s: %v\nCommand output:\n%s",
							tc.SetupCommand,
							tt.Repo,
							err,
							output)
					}

					if len(tc.GrogArgs) == 0 {
						// Skip the test step if the setup command only runs
						continue
					}
				}

				// Check for duplicate test case names
				if _, ok := testCaseNames[tc.Name]; ok {
					t.Fatalf("duplicate test case name in table %s: %s", tt.Name, tc.Name)
				}
				testCaseNames[tc.Name] = true

				t.Run(tc.Name, func(t *testing.T) {

					output, err := runBinary(tc.GrogArgs, repoPath, tc.EnvVars, coverDir)

					if err != nil && !tc.ExpectFail {
						fmt.Printf("Command output: %s\n", output)
						t.Fatal(err)
					}

					if tc.SkipFixture {
						return
					}

					if *updateAll {
						writeFixture(t, tc.Name, output)
						fmt.Printf("Updated fixture %s\n", tc.Name)
					} else if *update != "" && *update == tc.Name {
						writeFixture(t, tc.Name, output)
						fmt.Printf("Updated fixture %s\n", tc.Name)
					}

					actual := string(output)

					expected, err := loadFixture(t, tc.Name)
					if err != nil {
						t.Fatalf("could not load fixture %s: %v\ncommand output:\n%s", tc.Name, err, actual)
					}

					if !reflect.DeepEqual(actual, expected) {
						dmp := diffmatchpatch.New()
						diffs := dmp.DiffMain(actual, expected, false)
						t.Fatalf("outputs do not match. Diff:\n%s\nactual:\n%s\nexpected:\n%s", dmp.DiffPrettyText(diffs), actual, expected)
					}
				})
			}
		})
	}
}

func TestMain(m *testing.M) {
	err := os.Chdir("..")
	if err != nil {
		fmt.Printf("could not change dir: %v", err)
		os.Exit(1)
	}

	dir, err := os.Getwd()
	if err != nil {
		fmt.Printf("could not get current dir: %v", err)
	}

	binaryPath = filepath.Join(dir, binaryName)

	os.Exit(m.Run())
}

func runBinary(args []string, repoPath string, extraEnvVars []string, coverDir string) ([]byte, error) {
	repoPath = resolveRepoPath(repoPath)

	// Debug print the command invocation
	fmt.Printf("Running command: %s %v in directory: %s\n", binaryPath, args, repoPath)

	cmd := exec.Command(binaryPath, args...)

	// Set the environment variable for the coverage directory
	// so that the coverage report is written to the correct location
	cmd.Env = append(os.Environ(), "GOCOVERDIR="+coverDir)
	cmd.Env = append(cmd.Env, "GROG_DISABLE_NON_DETERMINISTIC_LOGGING=true")
	// Give each test table its own GROG_ROOT so parallel scenarios don't
	// share the (now flat) target cache and clobber each other via `grog
	// clean`. coverDir is stable per test table, so cases within a table
	// continue to share cache state as they did before.
	cmd.Env = append(cmd.Env, "GROG_ROOT="+filepath.Join(coverDir, "grog_root"), extraEnvVars...)

	// Uncomment to enable debug logging
	// TODO move to makefile flag
	// cmd.Env = append(cmd.Env, "LOG_LEVEL=debug")
	cmd.Dir = repoPath
	return cmd.CombinedOutput()
}

func runSetupCommand(command string, repoPath string, extraEnvVars []string) ([]byte, error) {
	repoPath = resolveRepoPath(repoPath)
	fmt.Printf("Running setup command: %s in directory: %s\n", command, repoPath)

	cmd := exec.Command("sh", "-c", command)
	cmd.Env = append(os.Environ(), extraEnvVars...)
	cmd.Dir = repoPath
	return cmd.CombinedOutput()
}

func resolveRepoPath(repoPath string) string {
	if filepath.IsAbs(repoPath) {
		return repoPath
	}
	return filepath.Join("./integration/test_repos", repoPath)
}

func repoCleanupFilePath(repoPath string) (string, error) {
	repoDirectory, err := filepath.Abs(resolveRepoPath(repoPath))
	if err != nil {
		return "", err
	}
	return filepath.Join(repoDirectory, ".grog-test-cleanup"), nil
}

func registerCleanupFile(t *testing.T, cleanupFilePath string) {
	t.Helper()

	cleanupFileContents, err := os.ReadFile(cleanupFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("could not read cleanup file %s: %v", cleanupFilePath, err)
	}

	var directoriesToRemove []string
	for _, line := range strings.Split(string(cleanupFileContents), "\n") {
		directoryToRemove := strings.TrimSpace(line)
		if directoryToRemove == "" {
			continue
		}
		directoriesToRemove = append(directoriesToRemove, directoryToRemove)
	}
	if len(directoriesToRemove) == 0 {
		return
	}

	t.Cleanup(func() {
		for _, directoryToRemove := range directoriesToRemove {
			if err := os.RemoveAll(directoryToRemove); err != nil {
				t.Errorf("could not remove cleanup directory %s: %v", directoryToRemove, err)
			}
		}
		if err := os.Remove(cleanupFilePath); err != nil && !os.IsNotExist(err) {
			t.Errorf("could not remove cleanup file %s: %v", cleanupFilePath, err)
		}
	})
}

func getCoverDir() (string, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("problems recovering caller information")
	}

	return filepath.Join(filepath.Dir(filename), "../coverdata/integration"), nil
}
