package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	testCases := []struct {
		name          string
		config        WorkspaceConfig
		expectedError string
	}{
		{
			name:   "valid",
			config: WorkspaceConfig{HashAlgorithm: "SHA256", LoadOutputs: "minimal", OutputMode: "detailed", OCI: OCIConfig{Backend: OCIBackendRegistry}},
		},
		{
			name:   "defaults",
			config: WorkspaceConfig{LoadOutputs: "all"},
		},
		{
			name:          "invalid hash algorithm",
			config:        WorkspaceConfig{HashAlgorithm: "md5", LoadOutputs: "all"},
			expectedError: "invalid hash_algorithm",
		},
		{
			name:          "invalid oci backend",
			config:        WorkspaceConfig{LoadOutputs: "all", OCI: OCIConfig{Backend: "s3"}},
			expectedError: "invalid oci backend",
		},
		{
			name:          "overlapping tags",
			config:        WorkspaceConfig{LoadOutputs: "all", Tags: []string{"slow"}, ExcludeTags: []string{"slow"}},
			expectedError: "cannot both be selected and excluded",
		},
		{
			name:          "invalid load outputs",
			config:        WorkspaceConfig{LoadOutputs: "some"},
			expectedError: "invalid load_outputs mode",
		},
		{
			name:          "invalid output mode",
			config:        WorkspaceConfig{LoadOutputs: "all", OutputMode: "verbose"},
			expectedError: "invalid output_mode",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.config.Validate()
			if testCase.expectedError == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), testCase.expectedError) {
				t.Fatalf("expected error containing %q, got %v", testCase.expectedError, err)
			}
		})
	}
}

func TestValidateGrogVersion(t *testing.T) {
	testCases := []struct {
		name            string
		requiredVersion string
		currentVersion  string
		expectedError   string
	}{
		{name: "no constraint", currentVersion: "garbage"},
		{name: "satisfied", requiredVersion: ">=0.10.0", currentVersion: "v0.12.1"},
		{name: "invalid constraint", requiredVersion: "not a range", currentVersion: "0.12.1", expectedError: "invalid requires_grog"},
		{name: "invalid current version", requiredVersion: ">=0.10.0", currentVersion: "dev", expectedError: "invalid grog version"},
		{name: "unsatisfied", requiredVersion: ">=1.0.0", currentVersion: "0.12.1", expectedError: "does not satisfy"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := WorkspaceConfig{RequiredGrogVersion: testCase.requiredVersion}.ValidateGrogVersion(testCase.currentVersion)
			if testCase.expectedError == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), testCase.expectedError) {
				t.Fatalf("expected error containing %q, got %v", testCase.expectedError, err)
			}
		})
	}
}

func TestModeGettersFallBackToDefaults(t *testing.T) {
	invalidConfig := WorkspaceConfig{LoadOutputs: "bogus", OutputMode: "bogus"}
	if mode := invalidConfig.GetLoadOutputsMode(); mode != LoadOutputsAll {
		t.Fatalf("expected LoadOutputsAll, got %v", mode)
	}
	if mode := invalidConfig.GetOutputMode(); mode != OutputModeTerse {
		t.Fatalf("expected OutputModeTerse, got %v", mode)
	}

	validConfig := WorkspaceConfig{LoadOutputs: "minimal", OutputMode: "detailed"}
	if mode := validConfig.GetLoadOutputsMode(); mode != LoadOutputsMinimal {
		t.Fatalf("expected LoadOutputsMinimal, got %v", mode)
	}
	if mode := validConfig.GetOutputMode(); mode != OutputModeDetailed {
		t.Fatalf("expected OutputModeDetailed, got %v", mode)
	}
}

func TestGetCurrentPackageAndWorkspaceRoot(t *testing.T) {
	workspaceRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	packageDirectory := filepath.Join(workspaceRoot, "foo", "bar")
	if err := os.MkdirAll(packageDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspaceRoot, "grog.toml"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	workspaceConfig := WorkspaceConfig{WorkspaceRoot: workspaceRoot}

	t.Chdir(workspaceRoot)
	if currentPackage, err := workspaceConfig.GetCurrentPackage(); err != nil || currentPackage != "" {
		t.Fatalf("expected root package, got %q (err: %v)", currentPackage, err)
	}

	t.Chdir(packageDirectory)
	if currentPackage, err := workspaceConfig.GetCurrentPackage(); err != nil || currentPackage != filepath.Join("foo", "bar") {
		t.Fatalf("expected foo/bar, got %q (err: %v)", currentPackage, err)
	}
	if foundRoot := MustFindWorkspaceRoot(); foundRoot != workspaceRoot {
		t.Fatalf("expected workspace root %q, got %q", workspaceRoot, foundRoot)
	}
}
