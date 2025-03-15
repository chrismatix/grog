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

func fixturePath(t *testing.T, fixture string) string {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("problems recovering caller information")
	}

	return filepath.Join(filepath.Dir(filename), "fixtures", fixture)
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

func loadFixture(t *testing.T, fixture string) string {
	content, err := os.ReadFile(fixturePath(t, fixture))
	if err != nil {
		t.Fatalf(
			"could not read fixture %s: %v",
			fixturePath(t, fixture),
			err)
	}

	return string(content)
}

type TestTable struct {
	Cases []TestCase
}
type TestCase struct {
	Name    string   `yaml:"name"`
	Repo    string   `yaml:"repo"`
	Args    []string `yaml:"args"`
	Fixture string   `yaml:"fixture"`
}

func TestCliArgs(t *testing.T) {

	// Unmarshal the test cases from test_table.yaml
	data, err := os.ReadFile("integration/test_table.yaml")
	if err != nil {
		t.Fatalf("could not read integration/test_table.yaml: %v", err)
	}

	var testTable TestTable
	err = yaml.Unmarshal(data, &testTable)
	if err != nil {
		t.Fatalf("could not unmarshal integration/test_table.yaml: %v", err)
	}

	tests := testTable.Cases

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {

			binaryCwd := filepath.Join("./integration/test_repos", tt.Repo)
			output, err := runBinary(tt.Args, binaryCwd)

			if err != nil {
				t.Fatal(err)
			}

			if *updateAll {
				writeFixture(t, tt.Fixture, output)
				fmt.Printf("Updated fixture %s\n", tt.Fixture)
			} else if *update != "" && *update == tt.Fixture {
				writeFixture(t, tt.Fixture, output)
				fmt.Printf("Updated fixture %s\n", tt.Fixture)
			}

			actual := string(output)

			expected := loadFixture(t, tt.Fixture)

			if !reflect.DeepEqual(actual, expected) {
				t.Fatalf("actual = %s, expected = %s", actual, expected)
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
	cmd.Dir = repoPath
	return cmd.CombinedOutput()
}

func getCoverDir() (string, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("problems recovering caller information")
	}

	return filepath.Join(filepath.Dir(filename), "../.coverdata"), nil
}
