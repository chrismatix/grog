package caching

import (
	"context"
	"grog/internal/caching/backends"
	"io"
)

// Cas is a content-addressable store.
// That is: Every record is identified by its digest
type Cas struct {
	backend backends.CacheBackend
}

func NewCas(
	cache backends.CacheBackend,
) *TargetCache {
	return &TargetCache{backend: cache}
}

func (c *Cas) GetBackend() backends.CacheBackend {
	return tc.backend
}

// Write writes a digest for a given reader
func (c *Cas) Write(ctx context.Context, digest string, reader io.Reader) error {
	return c.backend.Set(ctx, "cas", digest, reader)
}

// Load writes a digest for a given reader
func (c *Cas) Load(ctx context.Context, digest string) (io.ReadCloser, error) {
	return c.backend.Get(ctx, "cas", digest)
}

func (c *Cas) Exists(ctx context.Context, digest string) (bool, error) {
	return c.backend.Exists(ctx, "cas", digest)
}
