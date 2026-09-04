package config

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/viper"
	"github.com/subosito/gotenv"
)

// SelectedConfigNames returns configuration basenames in precedence order.
func SelectedConfigNames() []string {
	names := []string{"grog"}
	if os.Getenv("CI") == "1" {
		names = append([]string{"grog.ci"}, names...)
	}
	if profile := viper.GetString("profile"); profile != "" {
		names = append([]string{"grog." + profile}, names...)
	}
	return names
}

// SelectedWorkspaceConfigPath returns the selected configuration path within a workspace.
func SelectedWorkspaceConfigPath(workspaceRoot string) (string, error) {
	for _, name := range SelectedConfigNames() {
		path := filepath.Join(workspaceRoot, name+".toml")
		if _, operationError := os.Stat(path); operationError == nil {
			return path, nil
		} else if !errors.Is(operationError, os.ErrNotExist) {
			return "", operationError
		}
	}
	return "", nil
}

// ReadSelectedConfigFromViper applies profile and CI precedence to Viper's config search.
func ReadSelectedConfigFromViper() error {
	if configFile := viper.ConfigFileUsed(); configFile != "" {
		viper.AddConfigPath(filepath.Dir(configFile))
	}
	names := SelectedConfigNames()
	for _, name := range names {
		viper.SetConfigName(name)
		if operationError := viper.ReadInConfig(); operationError != nil {
			var configFileNotFoundError viper.ConfigFileNotFoundError
			if errors.As(operationError, &configFileNotFoundError) {
				continue
			}
			return operationError
		}
		return nil
	}
	return fmt.Errorf("no grog config file found (tried: %v)", names)
}

// LoadGlobalFromViper refreshes Global from Viper's current settings.
func LoadGlobalFromViper() error {
	var workspaceConfig WorkspaceConfig
	if operationError := viper.Unmarshal(&workspaceConfig); operationError != nil {
		return fmt.Errorf("failed to parse config: %w", operationError)
	}
	workspaceConfig.HashAlgorithm = strings.ToLower(workspaceConfig.HashAlgorithm)
	platform := viper.GetString("platform")
	if workspaceConfig.AllPlatforms && platform != "" {
		return fmt.Errorf("--platform cannot be used with --all-platforms")
	}
	if platform != "" {
		parts := strings.SplitN(platform, "/", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid platform %s, expected os/arch", platform)
		}
		workspaceConfig.OS = parts[0]
		workspaceConfig.Arch = parts[1]
	}

	environmentVariables := make(map[string]string)
	if workspaceConfig.EnvironmentVariablesFile != "" {
		environmentFilePath := workspaceConfig.EnvironmentVariablesFile
		if !filepath.IsAbs(environmentFilePath) {
			environmentFilePath = filepath.Join(workspaceConfig.WorkspaceRoot, environmentFilePath)
		}
		file, operationError := os.Open(environmentFilePath)
		if operationError != nil {
			return fmt.Errorf("failed to open environment_variables_file %q: %w", environmentFilePath, operationError)
		}
		maps.Copy(environmentVariables, gotenv.Parse(file))
		_ = file.Close()
	}
	if len(workspaceConfig.EnvironmentVariables) > 0 {
		configurationBytes, operationError := os.ReadFile(viper.ConfigFileUsed())
		if operationError != nil {
			return operationError
		}
		var configuration struct {
			EnvironmentVariables map[string]string `toml:"environment_variables"`
		}
		if operationError := toml.Unmarshal(configurationBytes, &configuration); operationError != nil {
			return operationError
		}
		maps.Copy(environmentVariables, configuration.EnvironmentVariables)
	}
	workspaceConfig.EnvironmentVariables = environmentVariables
	Global = workspaceConfig
	return nil
}

// ReloadGlobalFromViper reselects the active configuration file and refreshes Global.
func ReloadGlobalFromViper() error {
	if viper.ConfigFileUsed() == "" {
		return nil
	}
	if operationError := ReadSelectedConfigFromViper(); operationError != nil {
		return operationError
	}
	return LoadGlobalFromViper()
}
