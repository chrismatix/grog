package tracing

import (
	"fmt"
	"strings"

	"grog/internal/config"
)

// PathResolver constructs DuckDB-readable glob paths for Parquet trace files
// based on the configured storage backend.
type PathResolver struct {
	buildsBase string
	spansBase  string
}

// NewPathResolver creates a PathResolver from the current configuration.
func NewPathResolver() *PathResolver {
	tracesConfig := config.Global.Traces
	cacheConfig := config.Global.Cache

	// Use traces-specific backend if configured, otherwise fall back to cache backend
	backend := tracesConfig.Backend
	if backend == "" {
		backend = cacheConfig.Backend
	}

	switch backend {
	case config.S3CacheBackend:
		s3Config := tracesConfig.S3
		if tracesConfig.Backend == "" {
			s3Config = cacheConfig.S3
		}
		base := buildS3Base(s3Config)
		return &PathResolver{
			buildsBase: base + "/traces/builds",
			spansBase:  base + "/traces/spans",
		}
	case config.GCSCacheBackend:
		gcsConfig := tracesConfig.GCS
		if tracesConfig.Backend == "" {
			gcsConfig = cacheConfig.GCS
		}
		base := buildGCSBase(gcsConfig)
		return &PathResolver{
			buildsBase: base + "/traces/builds",
			spansBase:  base + "/traces/spans",
		}
	default:
		// Filesystem backend
		cacheDir := config.Global.GetWorkspaceCacheDirectory()
		return &PathResolver{
			buildsBase: cacheDir + "/traces/builds",
			spansBase:  cacheDir + "/traces/spans",
		}
	}
}

// BuildsGlob returns a DuckDB-readable glob for all build Parquet files.
func (p *PathResolver) BuildsGlob() string {
	return p.buildsBase + "/**/*.parquet"
}

// SpansGlob returns a DuckDB-readable glob for all span Parquet files.
func (p *PathResolver) SpansGlob() string {
	return p.spansBase + "/**/*.parquet"
}

func buildS3Base(cfg config.S3CacheConfig) string {
	prefix := strings.Trim(cfg.Prefix, "/")

	var workspacePrefix string
	if !cfg.SharedCache {
		workspacePrefix = strings.Trim(config.GetWorkspaceCachePrefix(config.Global.WorkspaceRoot), "/")
	}

	parts := []string{}
	if prefix != "" {
		parts = append(parts, prefix)
	}
	if workspacePrefix != "" {
		parts = append(parts, workspacePrefix)
	}

	base := fmt.Sprintf("s3://%s", cfg.Bucket)
	if len(parts) > 0 {
		base += "/" + strings.Join(parts, "/")
	}
	return base
}

func buildGCSBase(cfg config.GCSCacheConfig) string {
	// DuckDB reads GCS via S3 compatibility endpoint
	prefix := strings.Trim(cfg.Prefix, "/")

	var workspacePrefix string
	if !cfg.SharedCache {
		workspacePrefix = strings.Trim(config.GetWorkspaceCachePrefix(config.Global.WorkspaceRoot), "/")
	}

	parts := []string{}
	if prefix != "" {
		parts = append(parts, prefix)
	}
	if workspacePrefix != "" {
		parts = append(parts, workspacePrefix)
	}

	base := fmt.Sprintf("gcs://%s", cfg.Bucket)
	if len(parts) > 0 {
		base += "/" + strings.Join(parts, "/")
	}
	return base
}
