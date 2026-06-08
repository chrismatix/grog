package backends

import (
	"context"
	"testing"

	"grog/internal/config"
)

func setupFsTestConfig(t *testing.T) {
	t.Helper()
	prev := config.Global
	tmp := t.TempDir()
	config.Global = config.WorkspaceConfig{Root: tmp, WorkspaceRoot: tmp}
	t.Cleanup(func() { config.Global = prev })
}

func TestNewFileSystemCache(t *testing.T) {
	setupFsTestConfig(t)
	fs, err := NewFileSystemCache(context.Background())
	if err != nil {
		t.Fatalf("NewFileSystemCache: %v", err)
	}
	if fs == nil {
		t.Fatal("nil")
	}
	size, err := fs.GetCacheSizeBytes()
	if err != nil {
		t.Fatalf("GetCacheSizeBytes: %v", err)
	}
	_ = size
}

func TestNewFileSystemCacheForTest(t *testing.T) {
	fs := NewFileSystemCacheForTest(t.TempDir(), t.TempDir())
	if fs == nil {
		t.Fatal("nil")
	}
}

func TestGetCacheBackend_Default(t *testing.T) {
	setupFsTestConfig(t)
	backend, err := GetCacheBackend(context.Background(), config.CacheConfig{})
	if err != nil {
		t.Fatalf("GetCacheBackend: %v", err)
	}
	if backend == nil {
		t.Fatal("nil")
	}
}

func TestGetCacheBackend_BadGCSConfig(t *testing.T) {
	setupFsTestConfig(t)
	_, err := GetCacheBackend(context.Background(), config.CacheConfig{
		Backend: config.GCSCacheBackend,
		GCS:     config.GCSCacheConfig{},
	})
	if err == nil {
		t.Fatal("expected err — no bucket")
	}
}

func TestGetCacheBackend_BadS3Config(t *testing.T) {
	setupFsTestConfig(t)
	_, err := GetCacheBackend(context.Background(), config.CacheConfig{
		Backend: config.S3CacheBackend,
		S3:      config.S3CacheConfig{},
	})
	if err == nil {
		t.Fatal("expected err — no bucket")
	}
}

func TestGetCacheBackend_BadAzureConfig(t *testing.T) {
	setupFsTestConfig(t)
	_, err := GetCacheBackend(context.Background(), config.CacheConfig{
		Backend: config.AzureCacheBackend,
		Azure:   config.AzureCacheConfig{},
	})
	if err == nil {
		t.Fatal("expected err — no container")
	}
}

func TestNewAzureCache_NoContainer(t *testing.T) {
	_, err := NewAzureCache(context.Background(), config.AzureCacheConfig{})
	if err == nil {
		t.Fatal("expected err")
	}
}

func TestNewAzureCache_NoCredentialsConfigured(t *testing.T) {
	_, err := NewAzureCache(context.Background(), config.AzureCacheConfig{Container: "c"})
	if err == nil {
		t.Fatal("expected err — neither AccountURL nor ConnectionString")
	}
}

func TestNewS3Cache_NoBucket(t *testing.T) {
	_, err := NewS3Cache(context.Background(), config.S3CacheConfig{})
	if err == nil {
		t.Fatal("expected err")
	}
}

func TestDefaultIOConcurrency(t *testing.T) {
	c := DefaultIOConcurrency()
	if c <= 0 {
		t.Fatalf("got %d", c)
	}
}
