package tracing

import (
	"grog/internal/config"
)

// PathResolver constructs DuckDB-readable glob paths for Parquet trace files.
//
// DuckDB always reads from the local filesystem cache. When a remote backend
// (S3, GCS, Azure) is configured, grog's RemoteWrapper writes to both local
// and remote, so the local cache always has a copy of all traces written by
// this machine.
type PathResolver struct {
	buildsBase string
	spansBase  string
}

// NewPathResolver creates a PathResolver that points at the local filesystem cache.
func NewPathResolver() *PathResolver {
	cacheDir := config.Global.GetWorkspaceCacheDirectory()
	return &PathResolver{
		buildsBase: cacheDir + "/traces/builds",
		spansBase:  cacheDir + "/traces/spans",
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
