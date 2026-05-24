package session

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"grog/internal/caching"
	"grog/internal/caching/backends"
	"grog/internal/config"
	"grog/internal/dockerproxy"
)

// newTempCAS builds a filesystem-backed CAS rooted in a temp dir.
func newTempCAS(t *testing.T) *caching.Cas {
	t.Helper()
	prev := config.Global
	t.Cleanup(func() { config.Global = prev })
	config.Global = config.WorkspaceConfig{
		Root:          t.TempDir(),
		WorkspaceRoot: t.TempDir(),
		HashAlgorithm: config.HashAlgorithmXXH3,
	}
	backend, err := backends.GetCacheBackend(context.Background(), config.Global.Cache)
	if err != nil {
		t.Fatalf("cache backend: %v", err)
	}
	return caching.NewCas(backend)
}

// seedImageIntoCAS pushes a random image into the CAS via a loopback proxy
// (exactly how grog's docker handler populates CAS during a build) and returns
// its manifest digest.
func seedImageIntoCAS(t *testing.T, cas *caching.Cas) string {
	t.Helper()
	img, err := random.Image(2048, 3)
	if err != nil {
		t.Fatalf("random image: %v", err)
	}
	digest, err := img.Digest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}

	proxy, err := dockerproxy.New(context.Background(), cas)
	if err != nil {
		t.Fatalf("proxy: %v", err)
	}
	defer proxy.Close()

	ref, err := name.NewTag(proxy.Addr()+"/grog:seed", name.Insecure)
	if err != nil {
		t.Fatalf("tag: %v", err)
	}
	if err := remote.Write(ref, img, remote.WithContext(context.Background())); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	return digest.String()
}

func TestPushFromCASRoundTrip(t *testing.T) {
	ctx := context.Background()
	cas := newTempCAS(t)
	digest := seedImageIntoCAS(t, cas)

	// Stand up an in-memory destination registry.
	dst := httptest.NewServer(registry.New())
	defer dst.Close()
	dstHost := strings.TrimPrefix(dst.URL, "http://")
	repo := dstHost + "/team/api"

	// name.Insecure for the destination so the test's plain-HTTP registry works.
	res, err := pushFromCAS(ctx, cas, authn.DefaultKeychain, PushOptions{
		ManifestDigest: digest,
		Repository:     repo,
		Tags:           []string{"v1", "latest"},
	})
	if err != nil {
		t.Fatalf("pushFromCAS: %v", err)
	}

	if res.Digest != digest {
		t.Errorf("Digest = %q, want %q", res.Digest, digest)
	}
	if !strings.HasSuffix(res.Reference, "@"+digest) {
		t.Errorf("Reference = %q, want suffix @%s", res.Reference, digest)
	}
	if len(res.Tags) != 2 {
		t.Errorf("Tags = %v, want 2", res.Tags)
	}
	if res.Skipped {
		t.Error("first push should not be skipped")
	}

	// Verify the digest is really resolvable at the destination.
	dstRef, err := name.NewDigest(repo+"@"+digest, name.Insecure)
	if err != nil {
		t.Fatalf("dst digest ref: %v", err)
	}
	if _, err := remote.Head(dstRef, remote.WithContext(ctx)); err != nil {
		t.Fatalf("digest not present at destination: %v", err)
	}

	// Second push is convergent: digest + tags already present → skipped.
	res2, err := pushFromCAS(ctx, cas, authn.DefaultKeychain, PushOptions{
		ManifestDigest: digest,
		Repository:     repo,
		Tags:           []string{"v1", "latest"},
	})
	if err != nil {
		t.Fatalf("second pushFromCAS: %v", err)
	}
	if !res2.Skipped {
		t.Error("second push should be skipped (convergent)")
	}
}

func TestPushFromCASValidation(t *testing.T) {
	ctx := context.Background()
	cas := newTempCAS(t)

	if _, err := pushFromCAS(ctx, cas, authn.DefaultKeychain, PushOptions{
		ManifestDigest: "not-a-digest",
		Repository:     "example.com/x",
	}); err == nil {
		t.Error("expected error for bad digest")
	}

	if _, err := pushFromCAS(ctx, cas, authn.DefaultKeychain, PushOptions{
		ManifestDigest: "sha256:" + strings.Repeat("a", 64),
		Repository:     "",
	}); err == nil {
		t.Error("expected error for empty repository")
	}
}
