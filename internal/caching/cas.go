package caching

import (
	"bytes"
	"context"
	"grog/internal/caching/backends"
	"io"
)

// Cas is a content-addressable store.
// That is: Every record is identified by its digest
type Cas struct {
	backend backends.CacheBackend
	// Cache for exists queries since we assume that during the runtime of a build
	// the cache backend cannot lose a digest (grog does not delete during a build)
	keyExistsCache map[string]bool
}

func NewCas(
	cache backends.CacheBackend,
) *Cas {
	return &Cas{
		backend:        cache,
		keyExistsCache: make(map[string]bool),
	}
}

func (c *Cas) GetBackend() backends.CacheBackend {
	return c.backend
}

// Write writes a digest for a given reader
func (c *Cas) Write(ctx context.Context, digest string, reader io.Reader) error {
	if exists, err := c.Exists(ctx, digest); exists && err == nil {
		// If the digest already exists, we don't need to write it again
		return nil
	}
	return c.backend.Set(ctx, "cas", digest, reader)
}

// WriteBytes writes a digest for a given reader
func (c *Cas) WriteBytes(ctx context.Context, digest string, content []byte) error {
	return c.Write(ctx, digest, bytes.NewReader(content))
}

// Load loads the content for a given digest
func (c *Cas) Load(ctx context.Context, digest string) (io.ReadCloser, error) {
	return c.backend.Get(ctx, "cas", digest)
}

// LoadBytes loads the content for a given digest directly into a byte slice
func (c *Cas) LoadBytes(ctx context.Context, digest string) ([]byte, error) {
	reader, err := c.backend.Get(ctx, "cas", digest)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

func (c *Cas) Exists(ctx context.Context, digest string) (bool, error) {
	if cached, ok := c.keyExistsCache[digest]; ok && cached {
		return cached, nil
	}

	exists, err := c.backend.Exists(ctx, "cas", digest)
	if err != nil {
		return false, err
	}

	if exists {
		// Only cache if the key exists
		c.keyExistsCache[digest] = exists
	}
	return exists, nil
}
