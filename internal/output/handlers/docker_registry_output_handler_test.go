package handlers

import (
	"grog/internal/config"
	"grog/internal/label"
	"grog/internal/model"
	"testing"
)

func TestCacheImageName_ContentMode(t *testing.T) {
	origRoot := config.Global.WorkspaceRoot
	config.Global.WorkspaceRoot = "/home/user/myproject"
	t.Cleanup(func() { config.Global.WorkspaceRoot = origRoot })

	handler := &DockerRegistryOutputHandler{
		config: config.OCIConfig{
			Registry:  "123456.dkr.ecr.us-west-2.amazonaws.com",
			CacheMode: config.OCICacheModeContent,
		},
	}

	target := model.Target{
		Label: label.TargetLabel{Package: "services/api", Name: "build_image"},
	}

	got := handler.cacheImageName(target, "sha256:abcdef1234567890")
	// Content mode: should use the digest (without sha256: prefix) in the name.
	prefix := config.GetWorkspaceCachePrefix("/home/user/myproject")
	want := "123456.dkr.ecr.us-west-2.amazonaws.com/" + prefix + "-abcdef1234567890"
	if got != want {
		t.Fatalf("content mode:\n  got  %q\n  want %q", got, want)
	}
}

func TestCacheImageName_TargetMode(t *testing.T) {
	origRoot := config.Global.WorkspaceRoot
	config.Global.WorkspaceRoot = "/home/user/myproject"
	t.Cleanup(func() { config.Global.WorkspaceRoot = origRoot })

	handler := &DockerRegistryOutputHandler{
		config: config.OCIConfig{
			Registry:  "123456.dkr.ecr.us-west-2.amazonaws.com",
			CacheMode: config.OCICacheModeTarget,
		},
	}

	target := model.Target{
		Label: label.TargetLabel{Package: "services/api", Name: "build_image"},
	}

	got := handler.cacheImageName(target, "sha256:abcdef1234567890")
	// Target mode: should use the sanitized target label, not the digest.
	prefix := config.GetWorkspaceCachePrefix("/home/user/myproject")
	want := "123456.dkr.ecr.us-west-2.amazonaws.com/" + prefix + "-services-api-build_image:latest"
	if got != want {
		t.Fatalf("target mode:\n  got  %q\n  want %q", got, want)
	}
}

func TestCacheImageName_TargetMode_StableAcrossDigests(t *testing.T) {
	origRoot := config.Global.WorkspaceRoot
	config.Global.WorkspaceRoot = "/home/user/myproject"
	t.Cleanup(func() { config.Global.WorkspaceRoot = origRoot })

	handler := &DockerRegistryOutputHandler{
		config: config.OCIConfig{
			Registry:  "123456.dkr.ecr.us-west-2.amazonaws.com",
			CacheMode: config.OCICacheModeTarget,
		},
	}

	target := model.Target{
		Label: label.TargetLabel{Package: "services/api", Name: "build_image"},
	}

	// Two different digests should produce the same cache image name in target mode.
	name1 := handler.cacheImageName(target, "sha256:aaaa")
	name2 := handler.cacheImageName(target, "sha256:bbbb")
	if name1 != name2 {
		t.Fatalf("target mode should produce stable names across digests:\n  digest1: %q\n  digest2: %q", name1, name2)
	}
}

func TestCacheImageName_ContentMode_DifferentDigests(t *testing.T) {
	origRoot := config.Global.WorkspaceRoot
	config.Global.WorkspaceRoot = "/home/user/myproject"
	t.Cleanup(func() { config.Global.WorkspaceRoot = origRoot })

	handler := &DockerRegistryOutputHandler{
		config: config.OCIConfig{
			Registry:  "123456.dkr.ecr.us-west-2.amazonaws.com",
			CacheMode: config.OCICacheModeContent,
		},
	}

	target := model.Target{
		Label: label.TargetLabel{Package: "services/api", Name: "build_image"},
	}

	// Two different digests should produce different cache image names in content mode.
	name1 := handler.cacheImageName(target, "sha256:aaaa")
	name2 := handler.cacheImageName(target, "sha256:bbbb")
	if name1 == name2 {
		t.Fatalf("content mode should produce different names for different digests: both got %q", name1)
	}
}

func TestCacheImageName_DefaultCacheModeIsContent(t *testing.T) {
	origRoot := config.Global.WorkspaceRoot
	config.Global.WorkspaceRoot = "/home/user/myproject"
	t.Cleanup(func() { config.Global.WorkspaceRoot = origRoot })

	// Empty CacheMode should behave like content mode (the default).
	handler := &DockerRegistryOutputHandler{
		config: config.OCIConfig{
			Registry:  "123456.dkr.ecr.us-west-2.amazonaws.com",
			CacheMode: "",
		},
	}

	target := model.Target{
		Label: label.TargetLabel{Package: "pkg", Name: "tgt"},
	}

	name1 := handler.cacheImageName(target, "sha256:aaaa")
	name2 := handler.cacheImageName(target, "sha256:bbbb")
	if name1 == name2 {
		t.Fatalf("empty cache_mode should default to content-addressed: both got %q", name1)
	}
}

func TestCacheImageName_TargetMode_RootPackage(t *testing.T) {
	origRoot := config.Global.WorkspaceRoot
	config.Global.WorkspaceRoot = "/home/user/myproject"
	t.Cleanup(func() { config.Global.WorkspaceRoot = origRoot })

	handler := &DockerRegistryOutputHandler{
		config: config.OCIConfig{
			Registry:  "123456.dkr.ecr.us-west-2.amazonaws.com",
			CacheMode: config.OCICacheModeTarget,
		},
	}

	// Target at root package (empty package path).
	target := model.Target{
		Label: label.TargetLabel{Package: "", Name: "build_image"},
	}

	got := handler.cacheImageName(target, "")
	prefix := config.GetWorkspaceCachePrefix("/home/user/myproject")
	want := "123456.dkr.ecr.us-west-2.amazonaws.com/" + prefix + "--build_image:latest"
	if got != want {
		t.Fatalf("root package:\n  got  %q\n  want %q", got, want)
	}
}

func TestSeedLayerCache_NoopInContentMode(t *testing.T) {
	handler := &DockerRegistryOutputHandler{
		config: config.OCIConfig{
			CacheMode: config.OCICacheModeContent,
		},
	}

	target := model.Target{
		Label: label.TargetLabel{Package: "pkg", Name: "tgt"},
	}

	// SeedLayerCache should return nil immediately in content mode
	// without touching the Docker client (which is nil here -- would panic if called).
	if err := handler.SeedLayerCache(nil, target, nil); err != nil {
		t.Fatalf("expected nil error in content mode, got %v", err)
	}
}

func TestSeedLayerCache_NoopWithEmptyCacheMode(t *testing.T) {
	handler := &DockerRegistryOutputHandler{
		config: config.OCIConfig{
			CacheMode: "",
		},
	}

	target := model.Target{
		Label: label.TargetLabel{Package: "pkg", Name: "tgt"},
	}

	// Empty cache mode defaults to content mode -- should be a no-op.
	if err := handler.SeedLayerCache(nil, target, nil); err != nil {
		t.Fatalf("expected nil error with empty cache mode, got %v", err)
	}
}

func TestSeedLayerCache_NoopWhenPrebuildLayerFetchDisabled(t *testing.T) {
	disabled := false
	handler := &DockerRegistryOutputHandler{
		config: config.OCIConfig{
			CacheMode:          config.OCICacheModeTarget,
			PrebuildLayerFetch: &disabled,
		},
	}

	target := model.Target{
		Label: label.TargetLabel{Package: "pkg", Name: "tgt"},
	}

	// Target mode with prebuild_layer_fetch explicitly disabled -- should be a no-op.
	if err := handler.SeedLayerCache(nil, target, nil); err != nil {
		t.Fatalf("expected nil error with prebuild_layer_fetch=false, got %v", err)
	}
}

func TestIsPrebuildLayerFetchEnabled(t *testing.T) {
	boolPtr := func(v bool) *bool { return &v }

	tests := []struct {
		name      string
		cacheMode string
		prefetch  *bool
		want      bool
	}{
		{
			name:      "target mode, unset defaults to true",
			cacheMode: config.OCICacheModeTarget,
			prefetch:  nil,
			want:      true,
		},
		{
			name:      "target mode, explicitly true",
			cacheMode: config.OCICacheModeTarget,
			prefetch:  boolPtr(true),
			want:      true,
		},
		{
			name:      "target mode, explicitly false",
			cacheMode: config.OCICacheModeTarget,
			prefetch:  boolPtr(false),
			want:      false,
		},
		{
			name:      "content mode, unset defaults to false",
			cacheMode: config.OCICacheModeContent,
			prefetch:  nil,
			want:      false,
		},
		{
			name:      "empty mode, unset defaults to false",
			cacheMode: "",
			prefetch:  nil,
			want:      false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			dockerConfig := config.OCIConfig{
				CacheMode:          testCase.cacheMode,
				PrebuildLayerFetch: testCase.prefetch,
			}
			got := dockerConfig.IsPrebuildLayerFetchEnabled()
			if got != testCase.want {
				t.Fatalf("IsPrebuildLayerFetchEnabled() = %v, want %v", got, testCase.want)
			}
		})
	}
}
