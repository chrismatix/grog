package output

import (
	"context"
	"testing"

	"grog/internal/caching"
	"grog/internal/caching/backends"
	"grog/internal/config"
	"grog/internal/output/handlers"
)

func TestNewRegistry_RegistryBackend(t *testing.T) {
	prev := config.Global
	config.Global = config.WorkspaceConfig{OCI: config.OCIConfig{Backend: "registry"}}
	t.Cleanup(func() { config.Global = prev })

	fs := backends.NewFileSystemCacheForTest(t.TempDir(), t.TempDir())
	cas := caching.NewCas(fs)
	r := NewRegistry(context.Background(), cas)
	if r == nil {
		t.Fatal("nil")
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestRegistry_DuplicateRegisterPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic")
		}
	}()
	prev := config.Global
	config.Global = config.WorkspaceConfig{OCI: config.OCIConfig{Backend: "fs"}}
	t.Cleanup(func() { config.Global = prev })

	fs := backends.NewFileSystemCacheForTest(t.TempDir(), t.TempDir())
	cas := caching.NewCas(fs)
	r := NewRegistry(context.Background(), cas)
	r.Register(handlers.NewFileOutputHandler(cas))
}
