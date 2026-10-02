package loading

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"grog/internal/console"
	"grog/internal/label"
	"grog/internal/model"

	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
)

func TestResolveInputs(t *testing.T) {
	// Helper function to create files in the temporary directory.
	createFile := func(tmpDir string, path string, content string) {
		fullPath := filepath.Join(tmpDir, path)
		dir := filepath.Dir(fullPath)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			if err := os.MkdirAll(dir, 0755); err != nil {
				panic(fmt.Sprintf("Failed to create dir %s: %v", dir, err))
			}
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			panic(fmt.Sprintf("Failed to create file %s: %v", path, err)) // Panic to stop the test immediately
		}
	}

	// Test cases.
	testCases := []struct {
		name            string
		inputs          []string
		excludeInputs   []string
		expected        []string
		createTestFiles func(tmpDir string)
		expectedError   bool
	}{
		{
			name:   "NoGlobs",
			inputs: []string{"file1.txt", "file2.txt"},
			createTestFiles: func(tmpDir string) {
				createFile(tmpDir, "file1.txt", "content1")
				createFile(tmpDir, "file2.txt", "content2")
			},
			expected:      []string{"file1.txt", "file2.txt"},
			expectedError: false,
		},
		{
			name:   "SingleGlob",
			inputs: []string{"*.txt"},
			createTestFiles: func(tmpDir string) {
				createFile(tmpDir, "file1.txt", "content1")
				createFile(tmpDir, "file2.txt", "content2")
				createFile(tmpDir, "file3.md", "content3")
			},
			expected:      []string{"file1.txt", "file2.txt"},
			expectedError: false,
		},
		{
			name:   "SubdirectoryGlob",
			inputs: []string{"subdir/*.txt"},
			createTestFiles: func(tmpDir string) {
				createFile(tmpDir, "subdir/file1.txt", "content1")
				createFile(tmpDir, "subdir/file2.txt", "content2")
				createFile(tmpDir, "file1.txt", "content3")
			},
			expected:      []string{"subdir/file1.txt", "subdir/file2.txt"},
			expectedError: false,
		},
		{
			name:   "DoubleStarGlob",
			inputs: []string{"**/*.txt"},
			createTestFiles: func(tmpDir string) {
				createFile(tmpDir, "file1.txt", "content1")
				createFile(tmpDir, "subdir/file2.txt", "content2")
				createFile(tmpDir, "subdir/nested/file3.txt", "content3")
				createFile(tmpDir, "file1.md", "content4")
			},
			expected:      []string{"file1.txt", "subdir/file2.txt", "subdir/nested/file3.txt"},
			expectedError: false,
		},
		{
			name:   "MultipleInputs",
			inputs: []string{"file1.txt", "subdir/*.txt"},
			createTestFiles: func(tmpDir string) {
				createFile(tmpDir, "file1.txt", "content1")
				createFile(tmpDir, "subdir/file2.txt", "content2")
				createFile(tmpDir, "subdir/file3.txt", "content3")
			},
			expected:      []string{"file1.txt", "subdir/file2.txt", "subdir/file3.txt"},
			expectedError: false,
		},
		{
			name:   "NoMatch",
			inputs: []string{"*.go"},
			createTestFiles: func(tmpDir string) {
				createFile(tmpDir, "file1.txt", "content1")
				createFile(tmpDir, "file2.txt", "content2")
			},
			expected:      []string{},
			expectedError: false,
		},
		{
			name:   "InvalidGlobPattern",
			inputs: []string{"[***"}, // Invalid glob pattern
			createTestFiles: func(tmpDir string) {
			},
			expected:      []string{},
			expectedError: true,
		},
		{
			name:   "EmptyInputs",
			inputs: []string{},
			createTestFiles: func(tmpDir string) {
			},
			expected:      []string{},
			expectedError: false,
		},
		{
			name:   "GlobWithDotFiles",
			inputs: []string{".*"},
			createTestFiles: func(tmpDir string) {
				createFile(tmpDir, ".file1", "content1")
				createFile(tmpDir, "file2.txt", "content2")
			},
			expected:      []string{".file1"},
			expectedError: false,
		},
		{
			name:          "ExcludeSingleFile",
			inputs:        []string{"*.txt"},
			excludeInputs: []string{"file2.txt"},
			createTestFiles: func(tmpDir string) {
				createFile(tmpDir, "file1.txt", "content1")
				createFile(tmpDir, "file2.txt", "content2")
				createFile(tmpDir, "file3.txt", "content3")
			},
			expected:      []string{"file1.txt", "file3.txt"},
			expectedError: false,
		},
		{
			name:          "ExcludeGlob",
			inputs:        []string{"**/*.txt"},
			excludeInputs: []string{"subdir/*.txt"},
			createTestFiles: func(tmpDir string) {
				createFile(tmpDir, "file1.txt", "content1")
				createFile(tmpDir, "subdir/file2.txt", "content2")
				createFile(tmpDir, "subdir/file3.txt", "content3")
				createFile(tmpDir, "subdir/nested/file4.txt", "content4")
			},
			expected:      []string{"file1.txt", "subdir/nested/file4.txt"},
			expectedError: false,
		},
		{
			name:          "ExcludeMultiplePatterns",
			inputs:        []string{"**/*.txt"},
			excludeInputs: []string{"**/file2.txt", "**/file4.txt"},
			createTestFiles: func(tmpDir string) {
				createFile(tmpDir, "file1.txt", "content1")
				createFile(tmpDir, "file2.txt", "content2")
				createFile(tmpDir, "subdir/file3.txt", "content3")
				createFile(tmpDir, "subdir/file4.txt", "content4")
			},
			expected:      []string{"file1.txt", "subdir/file3.txt"},
			expectedError: false,
		},
		{
			name:          "ExcludeAll",
			inputs:        []string{"*.txt"},
			excludeInputs: []string{"*.txt"},
			createTestFiles: func(tmpDir string) {
				createFile(tmpDir, "file1.txt", "content1")
				createFile(tmpDir, "file2.txt", "content2")
			},
			expected:      []string{},
			expectedError: false,
		},
		{
			name:          "ExcludeDirectoryTree",
			inputs:        []string{"**/*.js"},
			excludeInputs: []string{"**/node_modules/**"},
			createTestFiles: func(tmpDir string) {
				createFile(tmpDir, "index.js", "content1")
				createFile(tmpDir, "node_modules/left-pad/index.js", "content2")
				createFile(tmpDir, "app/node_modules/react/index.js", "content3")
				createFile(tmpDir, "app/main.js", "content4")
			},
			expected:      []string{"app/main.js", "index.js"},
			expectedError: false,
		},
		{
			name:          "ExcludeDirectoryChildrenKeepsNestedFiles",
			inputs:        []string{"**/*.txt"},
			excludeInputs: []string{"subdir/*"},
			createTestFiles: func(tmpDir string) {
				createFile(tmpDir, "subdir/file1.txt", "content1")
				createFile(tmpDir, "subdir/nested/file2.txt", "content2")
			},
			expected:      []string{"subdir/nested/file2.txt"},
			expectedError: false,
		},
		{
			name:          "InvalidExcludePattern",
			inputs:        []string{"*.txt"},
			excludeInputs: []string{"[invalid"},
			createTestFiles: func(tmpDir string) {
				createFile(tmpDir, "file1.txt", "content1")
			},
			expectedError: true,
		},
	}

	testLogger := console.NewFromSugared(zaptest.NewLogger(t).Sugar(), zapcore.DebugLevel)
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a temporary directory for the test case.
			tmpDir := t.TempDir()

			// Setup test files.
			tc.createTestFiles(tmpDir)

			// Execute the function.
			actual, err := resolveInputs(testLogger, tmpDir, tc.inputs, tc.excludeInputs)

			if tc.expectedError {
				if err == nil {
					t.Fatalf("Expected error, but got nil")
				}
				// Check the error message, if needed.
				if err != nil && strings.Contains(tc.name, "InvalidGlobPattern") {
					expectedErrorMessage := fmt.Sprintf("failed to resolve glob pattern %s: ", tc.inputs[0])
					if !strings.Contains(err.Error(), expectedErrorMessage) {
						t.Errorf("Error message is not as expected. \nExpected to contain: %s\nGot: %s", expectedErrorMessage, err.Error())
					}
				}
			} else {
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
			}

			// Sort the results for reliable comparison
			sort.Strings(actual)
			sort.Strings(tc.expected)

			// DeepEqual does not work on empty slices, so check separately
			if len(tc.expected) == 0 && len(actual) == 0 {
				return
			}

			if !reflect.DeepEqual(actual, tc.expected) {
				t.Errorf("Unexpected resolved inputs.\nExpected: %v\nActual:   %v", tc.expected, actual)
			}
		})
	}
}

func TestExcludingFSHidesExcludedDirectories(t *testing.T) {
	filesystem := excludingFS{
		FS: fstest.MapFS{
			"index.js":                &fstest.MapFile{},
			"node_modules/a/index.js": &fstest.MapFile{},
			"src/main.js":             &fstest.MapFile{},
		},
		excludeInputs: []string{"node_modules/**", "src/*"},
	}
	entries, err := filesystem.ReadDir(".")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if !reflect.DeepEqual(names, []string{"index.js", "src"}) {
		t.Errorf("Unexpected entries: %v", names)
	}
}

func TestMergePackagesIncludesResources(t *testing.T) {
	resourceLabel := label.TL("pkg", "database")
	fromPackage := &model.Package{
		Resources: map[label.TargetLabel]*model.Resource{
			resourceLabel: {Label: resourceLabel, Up: "true"},
		},
	}
	intoPackage := &model.Package{
		Targets: map[label.TargetLabel]*model.Target{
			label.TL("pkg", "consumer"): {Label: label.TL("pkg", "consumer")},
		},
	}

	if err := mergePackages(fromPackage, intoPackage); err != nil {
		t.Fatalf("failed to merge packages: %v", err)
	}
	if intoPackage.Resources[resourceLabel] == nil {
		t.Fatal("resource was not merged")
	}
}

func TestMergePackagesRejectsResourceTargetCollisionsInBothOrders(t *testing.T) {
	resourceLabel := label.TL("pkg", "database")
	testCases := []struct {
		name        string
		fromPackage *model.Package
		intoPackage *model.Package
	}{
		{
			name: "resource into target",
			fromPackage: &model.Package{Resources: map[label.TargetLabel]*model.Resource{
				resourceLabel: {Label: resourceLabel, SourceFilePath: "resource.yaml"},
			}},
			intoPackage: &model.Package{Targets: map[label.TargetLabel]*model.Target{
				resourceLabel: {Label: resourceLabel, SourceFilePath: "target.yaml"},
			}},
		},
		{
			name: "target into resource",
			fromPackage: &model.Package{Targets: map[label.TargetLabel]*model.Target{
				resourceLabel: {Label: resourceLabel, SourceFilePath: "target.yaml"},
			}},
			intoPackage: &model.Package{Resources: map[label.TargetLabel]*model.Resource{
				resourceLabel: {Label: resourceLabel, SourceFilePath: "resource.yaml"},
			}},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := mergePackages(testCase.fromPackage, testCase.intoPackage); err == nil {
				t.Fatal("expected duplicate label error")
			}
		})
	}
}
