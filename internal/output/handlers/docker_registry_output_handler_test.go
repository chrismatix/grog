package handlers

import (
	"grog/internal/config"
	"grog/internal/label"
	"grog/internal/model"
	"testing"
)

// withRegistryConfig sets up a handler with a registry and pins the workspace
// root and platform so cache names are deterministic across the test run.
func withRegistryConfig(t *testing.T) *DockerRegistryOutputHandler {
	t.Helper()
	origRoot := config.Global.WorkspaceRoot
	config.Global.WorkspaceRoot = "/home/user/myproject"
	t.Cleanup(func() { config.Global.WorkspaceRoot = origRoot })

	origOS, origArch := config.Global.OS, config.Global.Arch
	config.Global.OS, config.Global.Arch = "linux", "amd64"
	t.Cleanup(func() { config.Global.OS, config.Global.Arch = origOS, origArch })

	return &DockerRegistryOutputHandler{
		config: config.OCIConfig{
			Registry: "123456.dkr.ecr.us-west-2.amazonaws.com",
		},
	}
}

func TestCacheImageName_Declared_IsContentAddressedTag(t *testing.T) {
	handler := withRegistryConfig(t)

	target := model.Target{
		Label:      label.TargetLabel{Package: "services/api", Name: "build_image"},
		OciPush:    map[string][]string{"api": {"registry.org/api:${GIT_SHA}"}},
		Outputs:    []model.Output{{Type: "oci", Identifier: "api"}},
		ChangeHash: "deadbeef",
	}

	// The declared destination's repository is used verbatim; the deploy tag is
	// dropped and replaced with grog's platform-qualified content hash.
	got := handler.cacheImageName(target, "api", "sha256:ignored")
	want := "registry.org/api:linux-amd64-deadbeef"
	if got != want {
		t.Fatalf("declared cache name:\n  got  %q\n  want %q", got, want)
	}
}

func TestCacheImageName_Declared_NewChangeHashYieldsNewTag(t *testing.T) {
	handler := withRegistryConfig(t)

	target := model.Target{
		Label:   label.TargetLabel{Package: "services/api", Name: "build_image"},
		OciPush: map[string][]string{"api": {"registry.org/api:latest"}},
		Outputs: []model.Output{{Type: "oci", Identifier: "api"}},
	}

	target.ChangeHash = "aaaa"
	first := handler.cacheImageName(target, "api", "sha256:same")
	target.ChangeHash = "bbbb"
	second := handler.cacheImageName(target, "api", "sha256:same")

	// A changed input (new ChangeHash) must produce a different, immutable tag
	// so a stale image can never be restored as the cache.
	if first == second {
		t.Fatalf("expected distinct tags per ChangeHash, both got %q", first)
	}
	if want := "registry.org/api:linux-amd64-aaaa"; first != want {
		t.Fatalf("first:\n  got  %q\n  want %q", first, want)
	}
	if want := "registry.org/api:linux-amd64-bbbb"; second != want {
		t.Fatalf("second:\n  got  %q\n  want %q", second, want)
	}
}

func TestCacheImageName_Declared_DistinctPerPlatform(t *testing.T) {
	handler := withRegistryConfig(t)

	target := model.Target{
		Label:      label.TargetLabel{Package: "services/api", Name: "build_image"},
		OciPush:    map[string][]string{"api": {"registry.org/api:v1"}},
		Outputs:    []model.Output{{Type: "oci", Identifier: "api"}},
		ChangeHash: "cafe",
	}

	config.Global.OS, config.Global.Arch = "linux", "amd64"
	amd64Name := handler.cacheImageName(target, "api", "")
	config.Global.OS, config.Global.Arch = "linux", "arm64"
	arm64Name := handler.cacheImageName(target, "api", "")

	if amd64Name == arm64Name {
		t.Fatalf("expected distinct names per platform, both got %q", amd64Name)
	}
	if want := "registry.org/api:linux-amd64-cafe"; amd64Name != want {
		t.Fatalf("amd64:\n  got  %q\n  want %q", amd64Name, want)
	}
	if want := "registry.org/api:linux-arm64-cafe"; arm64Name != want {
		t.Fatalf("arm64:\n  got  %q\n  want %q", arm64Name, want)
	}
}

func TestCacheImageName_NoDeclaration_FallsBackToContentAddressed(t *testing.T) {
	handler := withRegistryConfig(t)

	target := model.Target{
		Label:   label.TargetLabel{Package: "services/api", Name: "build_image"},
		Outputs: []model.Output{{Type: "oci", Identifier: "api"}},
		// No OciPush declared for "api".
		ChangeHash: "deadbeef",
	}

	got := handler.cacheImageName(target, "api", "sha256:abcdef1234567890")
	prefix := config.GetWorkspaceCachePrefix("/home/user/myproject")
	want := "123456.dkr.ecr.us-west-2.amazonaws.com/" + prefix + "-abcdef1234567890"
	if got != want {
		t.Fatalf("content-addressed fallback:\n  got  %q\n  want %q", got, want)
	}
}

func TestCacheImageName_NoDeclaration_DifferentDigestsDiffer(t *testing.T) {
	handler := withRegistryConfig(t)

	target := model.Target{
		Label:   label.TargetLabel{Package: "services/api", Name: "build_image"},
		Outputs: []model.Output{{Type: "oci", Identifier: "api"}},
	}

	name1 := handler.cacheImageName(target, "api", "sha256:aaaa")
	name2 := handler.cacheImageName(target, "api", "sha256:bbbb")
	if name1 == name2 {
		t.Fatalf("content-addressed fallback should differ per digest, both got %q", name1)
	}
}

func TestDeclaredCacheRepo(t *testing.T) {
	tests := []struct {
		name       string
		ociPush    map[string][]string
		identifier string
		want       string
	}{
		{
			name:       "destination with deploy tag drops the tag",
			ociPush:    map[string][]string{"api": {"registry.org/api:${GIT_SHA}"}},
			identifier: "api",
			want:       "registry.org/api",
		},
		{
			name:       "destination without a tag is unchanged",
			ociPush:    map[string][]string{"api": {"registry.org/team/api"}},
			identifier: "api",
			want:       "registry.org/team/api",
		},
		{
			name:       "host port preserved, only final tag stripped",
			ociPush:    map[string][]string{"api": {"localhost:5000/api:v2"}},
			identifier: "api",
			want:       "localhost:5000/api",
		},
		{
			name:       "first destination wins for multi-destination",
			ociPush:    map[string][]string{"api": {"registry.org/api:v1", "mirror.org/api:v1"}},
			identifier: "api",
			want:       "registry.org/api",
		},
		{
			name:       "no declaration returns empty",
			ociPush:    map[string][]string{"other": {"registry.org/other:v1"}},
			identifier: "api",
			want:       "",
		},
		{
			name:       "nil map returns empty",
			ociPush:    nil,
			identifier: "api",
			want:       "",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			target := model.Target{OciPush: testCase.ociPush}
			got := declaredCacheRepo(target, testCase.identifier)
			if got != testCase.want {
				t.Fatalf("declaredCacheRepo() = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestRepoWithoutTag(t *testing.T) {
	tests := []struct {
		reference string
		want      string
	}{
		{"registry.org/api:tag", "registry.org/api"},
		{"registry.org/api", "registry.org/api"},
		{"localhost:5000/api:v2", "localhost:5000/api"},
		{"localhost:5000/api", "localhost:5000/api"},
		{"registry.org/team/sub/api:1.2.3", "registry.org/team/sub/api"},
		{"123456.dkr.ecr.us-west-2.amazonaws.com/repo:linux-amd64", "123456.dkr.ecr.us-west-2.amazonaws.com/repo"},
	}
	for _, testCase := range tests {
		t.Run(testCase.reference, func(t *testing.T) {
			if got := repoWithoutTag(testCase.reference); got != testCase.want {
				t.Fatalf("repoWithoutTag(%q) = %q, want %q", testCase.reference, got, testCase.want)
			}
		})
	}
}

func TestSeedLayerCache_NoopWhenPriorHashEmpty(t *testing.T) {
	handler := &DockerRegistryOutputHandler{config: config.OCIConfig{}}
	target := model.Target{
		Label:   label.TargetLabel{Package: "pkg", Name: "tgt"},
		OciPush: map[string][]string{"img": {"registry.org/img:v1"}},
		Outputs: []model.Output{{Type: "oci", Identifier: "img"}},
	}

	// With an empty prior change hash there is no donor to pull, so SeedLayerCache
	// must return immediately without touching the Docker client (nil here --
	// would panic if dereferenced).
	if err := handler.SeedLayerCache(nil, target, "", nil); err != nil {
		t.Fatalf("expected nil error when prior hash is empty, got %v", err)
	}
}
