package config

import (
	"fmt"
	"os"
	"path/filepath"
)

type WorkspaceConfig struct {
	GrogRoot      string `mapstructure:"grog_root"`
	WorkspaceRoot string `mapstructure:"workspace_root"`

	// Execution
	FailFast   bool `mapstructure:"fail_fast"`
	NumWorkers int  `mapstructure:"num_workers"`
	// Logging
	LogLevel      string `mapstructure:"log_level"`
	LogOutputPath string `mapstructure:"log_output_path"`

	Cache CacheConfig `mapstructure:"cache"`
}

var Global WorkspaceConfig

func (w WorkspaceConfig) GetCacheDirectory() string {
	if w.GrogRoot == "" {
		// This can only ever happen if the user intentionally sets the GrogRoot to ""
		// or if we have an initialization bug.
		// In any case we should exit with an error because otherwise we might end up
		// writing/removing files in the wrong place.
		fmt.Println("GROG_ROOT is not set (or set to \"\").")
		os.Exit(1)
	}
	return filepath.Join(w.GrogRoot, "cache")
}

type CacheBackend string

const (
	LocalCacheBackend CacheBackend = "" // Default to local cache
	GCSCacheBackend   CacheBackend = "gcs"
	S3CacheBackend    CacheBackend = "s3"
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
