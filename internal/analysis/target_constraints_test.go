package analysis

import (
	"github.com/stretchr/testify/assert"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/output"
	"os"
	"testing"

	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"grog/internal/label"
	"grog/internal/model"
)

func setupWorkspaceRoot(t *testing.T) (string, func()) {
	t.Helper()
	// Create a temporary directory to simulate the workspace root
	workspaceRoot, err := os.MkdirTemp("", "workspace")
	if err != nil {
		t.Fatalf("Failed to create temporary workspace root: %v", err)
	}

	// Set the workspace_root config value
	//viper.Set("workspace_root", workspaceRoot)
	config.Global.WorkspaceRoot = workspaceRoot

	cleanup := func() {
		os.RemoveAll(workspaceRoot)
		viper.Reset()
	}

	return workspaceRoot, cleanup
}

func mustParseOutputs(outputs []string) []model.Output {
	parsedOutputs, err := output.ParseOutputs(outputs)
	if err != nil {
		panic(err)
	}
	return parsedOutputs
}

func TestCheckPathConstraints(t *testing.T) {
	tests := []struct {
		name               string
		targetMap          model.TargetMap
		expectWarningCount int
		expectErrorCount   int
	}{
		{
			name: "valid target without warnings or errors",
			targetMap: model.TargetMap{
				label.TL("", "target1"): &model.Target{
					Label:        label.TL("", "target1"),
					Inputs:       []string{"src/file.go"},
					Dependencies: []label.TargetLabel{label.TL("", "dep1")},
					Outputs:      mustParseOutputs([]string{"bin/output1"}),
				},
			},
			expectWarningCount: 0,
			expectErrorCount:   0,
		},
		{
			name: "target without inputs or dependencies does NOT generate warnings",
			targetMap: model.TargetMap{
				label.TL("", "target1"): &model.Target{
					Label: label.TL("", "target1"),
				},
			},
			expectWarningCount: 1,
			expectErrorCount:   0,
		},
		{
			name: "target inputs include an absolute path",
			targetMap: model.TargetMap{
				label.TL("", "target1"): &model.Target{
					Label:   label.TL("", "target1"),
					Inputs:  []string{"/abs/path/file.go"},
					Outputs: mustParseOutputs([]string{"bin/output1"}), // Added output to prevent warning
				},
			},
			expectWarningCount: 0,
			expectErrorCount:   1,
		},
		{
			name: "target inputs escape package path",
			targetMap: model.TargetMap{
				label.TL("", "target1"): &model.Target{
					Label:   label.TL("", "target1"),
					Inputs:  []string{"../../outside/package/file.go"},
					Outputs: mustParseOutputs([]string{"bin/output1"}), // Added output to prevent warning
				},
			},
			expectWarningCount: 0,
			expectErrorCount:   1,
		},
		{
			name: "target outputs outside repository",
			targetMap: model.TargetMap{
				label.TL("", "target1"): &model.Target{
					Label:   label.TL("", "target1"),
					Inputs:  []string{"src/file.go"},
					Outputs: mustParseOutputs([]string{"/outside/repo/output"}),
				},
			},
			expectWarningCount: 0,
			expectErrorCount:   1,
		},
		{
			name: "target with both warnings and errors",
			targetMap: model.TargetMap{
				label.TL("", "target1"): &model.Target{
					Label:   label.TL("", "target1"),
					Inputs:  []string{"/abs/path/file.go", "../../outside/package/file.go"},
					Outputs: mustParseOutputs([]string{"bin/output1"}),
				},
			},
			expectWarningCount: 0,
			expectErrorCount:   2,
		},
		{
			name: "target with outputs that are relative and inside the workspace",
			targetMap: model.TargetMap{
				label.TL("", "target1"): &model.Target{
					Label:   label.TL("", "target1"),
					Inputs:  []string{"src/file.go"},
					Outputs: mustParseOutputs([]string{"bin/output1", "bin/output2"}),
				},
			},
			expectWarningCount: 0,
			expectErrorCount:   0,
		},
		{
			name: "target with outputs that tries to escape workspace",
			targetMap: model.TargetMap{
				label.TL("", "target1"): &model.Target{
					Label:   label.TL("", "target1"),
					Inputs:  []string{"src/file.go"},
					Outputs: mustParseOutputs([]string{"../output1"}),
				},
			},
			expectWarningCount: 0,
			expectErrorCount:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup workspace root
			workspaceRoot, cleanup := setupWorkspaceRoot(t)
			defer cleanup()

			// Given
			observedZapCore, observedLogs := observer.New(zap.WarnLevel)
			observedLogger := zap.New(observedZapCore).Sugar()

			// Execute the function under test
			errs := CheckTargetConstraints(observedLogger, tt.targetMap)

			// Check logged warnings count
			if observedLogs.Len() != tt.expectWarningCount {
				t.Errorf("expected %d warnings, got %d", tt.expectWarningCount, observedLogs.Len())
			}

			// Check returned errors count
			errorCount := len(errs)
			if errorCount != tt.expectErrorCount {
				t.Errorf("expected %d errors, got %d %s", tt.expectErrorCount, errorCount, console.FormatErrors(errs))
			}

			// Cleanup
			err := os.RemoveAll(workspaceRoot)
			if err != nil {
				t.Fatalf("could not clear workspace root after test")
			}
		})
	}
}

func TestCheckInputPathsRelative(t *testing.T) {
	tests := []struct {
		name             string
		target           *model.Target
		expectErrorCount int
	}{
		{
			name:             "inputs are relative and inside package",
			target:           &model.Target{Label: label.TL("", "target1"), Inputs: []string{"src/file.go", "src/subdir/file2.go"}},
			expectErrorCount: 0,
		},
		{
			name:             "inputs contain an absolute path",
			target:           &model.Target{Label: label.TL("", "target1"), Inputs: []string{"/absolute/path/to/file.go"}},
			expectErrorCount: 1,
		},
		{
			name:             "inputs try to escape package",
			target:           &model.Target{Label: label.TL("", "target1"), Inputs: []string{"../../outside/package/file.go"}},
			expectErrorCount: 1,
		},
		{
			name:             "mixed valid and invalid inputs",
			target:           &model.Target{Label: label.TL("", "target1"), Inputs: []string{"src/file.go", "/abs/path.go", "../../escape/file.go"}},
			expectErrorCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := checkInputPathsRelative(tt.target)

			errorCount := len(errs)

			if errorCount != tt.expectErrorCount {
				t.Errorf("expected %d errors, got %d", tt.expectErrorCount, errorCount)
			}
		})
	}
}

func TestPathTriesToEscapePackage(t *testing.T) {
	tests := []struct {
		name       string
		relPath    string
		escapesPkg bool
	}{
		{
			name:       "valid relative path",
			relPath:    "src/file.go",
			escapesPkg: false,
		},
		{
			name:       "path trying to escape package",
			relPath:    "../../outside/package/file.go",
			escapesPkg: true,
		},
		{
			name:       "clean relative path with mixed dots",
			relPath:    "./src/../src/file.go",
			escapesPkg: false,
		},
		{
			name:       "edge case - exactly '..'",
			relPath:    "..",
			escapesPkg: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := pathTriesToEscape(tt.relPath)
			if result != tt.escapesPkg {
				t.Errorf("pathTriesToEscape(%s) = %v; want %v", tt.relPath, result, tt.escapesPkg)
			}
		})
	}
}

func TestIsWithinWorkspace(t *testing.T) {
	tests := []struct {
		name          string
		workspaceRoot string
		packagePath   string
		relPath       string
		expectWithin  bool
		expectError   bool
	}{
		{
			name:          "within workspace",
			workspaceRoot: "/path/to/workspace",
			packagePath:   "pkg",
			relPath:       "file.go",
			expectWithin:  true,
			expectError:   false,
		},
		{
			name:          "escaping workspace",
			workspaceRoot: "/path/to/workspace",
			packagePath:   "pkg",
			relPath:       "../file.go",
			expectWithin:  true,
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			within, err := isWithinWorkspace(tt.workspaceRoot, tt.packagePath, tt.relPath)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectWithin, within)
			}
		})
	}
}
