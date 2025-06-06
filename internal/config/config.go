package config

import (
	"fmt"
	"os"
	"path/filepath"
)

type WorkspaceConfig struct {
	Root          string `mapstructure:"root"`
	WorkspaceRoot string `mapstructure:"workspace_root"`

	// Execution
	FailFast    bool   `mapstructure:"fail_fast"`
	StreamLogs  bool   `mapstructure:"stream_logs"`
	NumWorkers  int    `mapstructure:"num_workers"`
	LoadOutputs string `mapstructure:"load_outputs"`

	// Logging
	LogLevel      string `mapstructure:"log_level"`
	LogOutputPath string `mapstructure:"log_output_path"`

	// Caching
	EnableCache bool        `mapstructure:"enable_cache"`
	Cache       CacheConfig `mapstructure:"cache"`

	// Docker
	Docker DockerConfig `mapstructure:"docker"`

	// Environment
	// Not officially supported in grog.toml but exposed via env variables
	OS   string `mapstructure:"os"`
	Arch string `mapstructure:"arch"`

	Tags        []string `mapstructure:"tag"`
	ExcludeTags []string `mapstructure:"exclude_tag"`

	// Internal configs
	// Used for integration testing:
	// Due to the concurrent nature of grog's execution we don't want to include
	// logs that don't have a guaranteed order
	DisableNonDeterministicLogging bool `mapstructure:"disable_non_deterministic_logging"`
}

var Global WorkspaceConfig

func (w WorkspaceConfig) GetCacheDirectory() string {
	if w.Root == "" {
		// This can only ever happen if the user intentionally sets the Root to ""
		// or if we have an initialization bug.
		// In any case we should exit with an error because otherwise we might end up
		// writing/removing files in the wrong place.
		fmt.Println("GROG_ROOT is not set (or set to \"\").")
		os.Exit(1)
	}
	return filepath.Join(w.Root, "cache")
}

func (w WorkspaceConfig) IsDebug() bool {
	return w.LogLevel == "debug"
}

func (w WorkspaceConfig) GetCurrentPackage() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current working directory: %w", err)
	}

	rel, err := filepath.Rel(w.WorkspaceRoot, cwd)
	if err != nil {
		return "", fmt.Errorf("failed to get relative path: %w", err)
	}

	if rel == "." {
		// Since we are returning the package we omit the dot prefix.
		return "", nil
	}

	return rel, nil
}

func (w WorkspaceConfig) GetPlatform() string {
	return fmt.Sprintf("%s/%s", w.OS, w.Arch)
}

func (w WorkspaceConfig) Validate() error {
	if w.Docker.Backend != "" &&
		(w.Docker.Backend != DockerBackendFSTarball && w.Docker.Backend != DockerBackendRegistry) {
		return fmt.Errorf("invalid docker backend: %s. Must be either %s or %s",
			w.Docker.Backend, DockerBackendFSTarball, DockerBackendRegistry)
	}

	// assert that tags and exclude tags do not overlap
	for _, tag := range w.Tags {
		for _, excludeTag := range w.ExcludeTags {
			if tag == excludeTag {
				return fmt.Errorf("tag %s cannot both be selected and excluded", tag)
			}
		}
	}

	// Validate LoadOutputs
	_, err := ParseLoadOutputsMode(w.LoadOutputs)
	if err != nil {
		return err
	}

	return nil
}

func (w WorkspaceConfig) GetLoadOutputsMode() LoadOutputsMode {
	mode, err := ParseLoadOutputsMode(w.LoadOutputs)
	if err != nil {
		// This should never happen because we validate the value in Validate()
		// But just in case, return the default value
		return LoadOutputsAll
	}
	return mode
}

type CacheBackend string

const (
	GCSCacheBackend CacheBackend = "gcs"
	S3CacheBackend  CacheBackend = "s3"
)

type CacheConfig struct {
	Backend CacheBackend   `mapstructure:"backend"`
	GCS     GCSCacheConfig `mapstructure:"gcs"`
	S3      S3CacheConfig  `mapstructure:"s3"`
}

type GCSCacheConfig struct {
	Bucket          string `mapstructure:"bucket"`
	Prefix          string `mapstructure:"prefix"`
	CredentialsFile string `mapstructure:"credentials_file"`
}

type S3CacheConfig struct {
	Bucket          string `mapstructure:"bucket"`
	Prefix          string `mapstructure:"prefix"`
	CredentialsFile string `mapstructure:"credentials_file"`
}

const (
	DockerBackendFSTarball = "tarball"
	DockerBackendRegistry  = "registry"
)

type DockerConfig struct {
	Backend string `mapstructure:"backend"`

	Registry string `mapstructure:"registry"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
}
