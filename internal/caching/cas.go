package caching

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"

	"grog/internal/caching/backends"
)

// Cas is a content-addressable store.
// That is: Every record is identified by its digest
type Cas struct {
	backend backends.CacheBackend
	// Cache for exists queries since we assume that during the runtime of a build
	// the cache backend cannot lose a digest (grog does not delete during a build)
	keyExistsCache sync.Map
	// asyncRemote controls whether remote writes should be deferred.
	// When true, HasRemoteBackend() returns true to signal handlers to use
	// WriteLocal + deferred UploadRemote. When false, Write() goes through
	// the full RemoteWrapper path as before.
	asyncRemote bool
}

func NewCas(
	cache backends.CacheBackend,
) *Cas {
	return &Cas{
		backend: cache,
	}
}

// SetAsyncRemote enables async remote mode. When set, HasRemoteBackend()
// returns true and handlers should use WriteLocal + deferred UploadRemote.
func (c *Cas) SetAsyncRemote(enabled bool) {
	c.asyncRemote = enabled
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

	err := c.backend.Set(ctx, "cas", digest, reader)
	if err == nil {
		// Mark the digest as existing in case later targets create the same digest
		c.keyExistsCache.Store(digest, true)
	}
	return err
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

// HasRemoteBackend returns true if the CAS backend is a RemoteWrapper
// AND async remote mode is enabled.
func (c *Cas) HasRemoteBackend() bool {
	if !c.asyncRemote {
		return false
	}
	_, ok := c.backend.(*backends.RemoteWrapper)
	return ok
}

// WriteLocal writes only to the local FS backend.
// For RemoteWrapper: writes to the local FS only.
// For plain FS: same as Write().
func (c *Cas) WriteLocal(ctx context.Context, digest string, reader io.Reader) error {
	if exists, err := c.Exists(ctx, digest); exists && err == nil {
		return nil
	}

	var err error
	if rw, ok := c.backend.(*backends.RemoteWrapper); ok {
		err = rw.GetFS().Set(ctx, "cas", digest, reader)
	} else {
		err = c.backend.Set(ctx, "cas", digest, reader)
	}
	if err == nil {
		c.keyExistsCache.Store(digest, true)
	}
	return err
}

// UploadRemote reads from local FS and writes to remote.
// For RemoteWrapper: reads from local, writes to remote.
// For plain FS: no-op.
func (c *Cas) UploadRemote(ctx context.Context, digest string) error {
	rw, ok := c.backend.(*backends.RemoteWrapper)
	if !ok {
		return nil
	}

	reader, err := rw.GetFS().Get(ctx, "cas", digest)
	if err != nil {
		return fmt.Errorf("failed to read digest %s from local cache: %w", digest, err)
	}
	defer reader.Close()

	return rw.GetRemote().Set(ctx, "cas", digest, reader)
}

func (c *Cas) Exists(ctx context.Context, digest string) (bool, error) {
	if cached, ok := c.keyExistsCache.Load(digest); ok && cached.(bool) {
		return cached.(bool), nil
	}

	exists, err := c.backend.Exists(ctx, "cas", digest)
	if err != nil {
		return false, err
	}

	if exists {
		// Only cache if the key exists
		c.keyExistsCache.Store(digest, exists)
	}
	return exists, nil
}
