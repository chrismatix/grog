package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func TestReloadGlobalFromViper(t *testing.T) {
	previousGlobal := Global
	t.Cleanup(func() {
		Global = previousGlobal
		viper.Reset()
	})
	viper.Reset()
	workspaceRoot := t.TempDir()
	configurationPath := filepath.Join(workspaceRoot, "grog.dev.toml")
	environmentPath := filepath.Join(workspaceRoot, "environment.env")
	if operationError := os.WriteFile(environmentPath, []byte("FILE_VALUE=one\n"), 0o644); operationError != nil {
		t.Fatal(operationError)
	}
	if operationError := os.WriteFile(configurationPath, []byte("os = \"linux\"\narch = \"amd64\"\nplatform_tag = [\"first\"]\nenvironment_variables_file = \"environment.env\"\n[environment_variables]\nBUILD_VALUE = \"one\"\n"), 0o644); operationError != nil {
		t.Fatal(operationError)
	}
	viper.SetConfigFile(configurationPath)
	viper.Set("workspace_root", workspaceRoot)
	if operationError := viper.ReadInConfig(); operationError != nil {
		t.Fatal(operationError)
	}
	if operationError := LoadGlobalFromViper(); operationError != nil {
		t.Fatal(operationError)
	}

	if operationError := os.WriteFile(configurationPath, []byte("os = \"linux\"\narch = \"amd64\"\nplatform_tag = [\"second\"]\nenvironment_variables_file = \"environment.env\"\n[environment_variables]\nBUILD_VALUE = \"two\"\n"), 0o644); operationError != nil {
		t.Fatal(operationError)
	}
	if operationError := os.WriteFile(environmentPath, []byte("FILE_VALUE=two\n"), 0o644); operationError != nil {
		t.Fatal(operationError)
	}
	if operationError := ReloadGlobalFromViper(); operationError != nil {
		t.Fatal(operationError)
	}
	if len(Global.PlatformTags) != 1 || Global.PlatformTags[0] != "second" || Global.EnvironmentVariables["BUILD_VALUE"] != "two" || Global.EnvironmentVariables["FILE_VALUE"] != "two" {
		t.Fatalf("unexpected reloaded config: %#v", Global)
	}
}
