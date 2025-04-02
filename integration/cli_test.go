package main

import (
	"flag"
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"reflect"
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

func loadFixture(t *testing.T, testName string) string {
	content, err := os.ReadFile(fixturePath(t, testName))
	if err != nil {
		t.Fatalf(
			"could not read fixture %s: %v",
			fixturePath(t, testName),
			err)
	}

	return string(content)
}

type TestTable struct {
	Name  string `yaml:"name"`
	Cases []TestCase
}
type TestCase struct {
	// Names must be unique as they determine the fixture file name
	Name       string   `yaml:"name"`
	Repo       string   `yaml:"repo"`
	Args       []string `yaml:"args"`
	ExpectFail bool     `yaml:"expect_fail"`
}

func TestCliArgs(t *testing.T) {

	// Read all .yaml files from the cases/ directory
	files, err := filepath.Glob("integration/test_tables/*.yaml")
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

	for _, tt := range testTables {
		t.Run(tt.Name, func(t *testing.T) {

			testCaseNames := make(map[string]bool)
			for _, tc := range tt.Cases {

				// Check for duplicate test case names
				if _, ok := testCaseNames[tc.Name]; ok {
					t.Fatalf("duplicate test case name in table %s: %s", tt.Name, tc.Name)
				}
				testCaseNames[tc.Name] = true

				t.Run(tc.Name, func(t *testing.T) {

					binaryCwd := filepath.Join("./integration/test_repos", tc.Repo)
					output, err := runBinary(tc.Args, binaryCwd)

					if err != nil && !tc.ExpectFail {
						fmt.Printf("Command ouput: %s\n", output)
						t.Fatal(err)
					}

					if *updateAll {
						writeFixture(t, tc.Name, output)
						fmt.Printf("Updated fixture %s\n", tc.Name)
					} else if *update != "" && *update == tc.Name {
						writeFixture(t, tc.Name, output)
						fmt.Printf("Updated fixture %s\n", tc.Name)
					}

					actual := string(output)

					expected := loadFixture(t, tc.Name)

					if !reflect.DeepEqual(actual, expected) {
						t.Fatalf("actual:\n%s\nexpected:\n%s", actual, expected)
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

func runBinary(args []string, repoPath string) ([]byte, error) {
	// Debug print the command invocation
	fmt.Printf("Running command: %s %v in directory: %s\n", binaryPath, args, repoPath)

	cmd := exec.Command(binaryPath, args...)

	// Set the environment variable for the coverage directory
	// so that the coverage report is written to the correct location
	coverDir, err := getCoverDir()
	if err != nil {
		return nil, err
	}
	cmd.Env = append(os.Environ(), "GOCOVERDIR="+coverDir)
	cmd.Env = append(cmd.Env, "DISABLE_TIME_LOGGING=true")

	// Uncomment to enable debug logging
	// TODO move to makefile flag
	// cmd.Env = append(cmd.Env, "LOG_LEVEL=debug")
	cmd.Dir = repoPath
	return cmd.CombinedOutput()
}

func getCoverDir() (string, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("problems recovering caller information")
	}

	return filepath.Join(filepath.Dir(filename), "../.coverdata/integration"), nil
}
