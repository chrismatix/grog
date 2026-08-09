package oci_push

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

func startRegistry(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(registry.New())
	t.Cleanup(server.Close)
	return strings.TrimPrefix(server.URL, "http://")
}

func mustParse(t *testing.T, ref string) name.Reference {
	t.Helper()
	parsed, err := name.ParseReference(ref, name.Insecure)
	if err != nil {
		t.Fatalf("parse %q: %v", ref, err)
	}
	return parsed
}

func mustRandomImage(t *testing.T, byteSize, layers int64) v1.Image {
	t.Helper()
	img, err := random.Image(byteSize, layers)
	if err != nil {
		t.Fatalf("random image: %v", err)
	}
	return img
}

func mustRandomIndex(t *testing.T, children int64) v1.ImageIndex {
	t.Helper()
	index, err := random.Index(256, 1, children)
	if err != nil {
		t.Fatalf("random index: %v", err)
	}
	return index
}

func mustDigest(t *testing.T, artifact interface{ Digest() (v1.Hash, error) }) v1.Hash {
	t.Helper()
	digest, err := artifact.Digest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	return digest
}

func headDigest(t *testing.T, ref string) v1.Hash {
	t.Helper()
	desc, err := remote.Head(mustParse(t, ref), remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		t.Fatalf("head %q: %v", ref, err)
	}
	return desc.Digest
}

// TestCopyPreservesMultiPlatformIndex locks the contract that a copy lands the
// exact manifest the build produced. Resolving the index to a single child
// would ship one platform and a digest the user never built.
func TestCopyPreservesMultiPlatformIndex(t *testing.T) {
	sourceHost := startRegistry(t)
	destinationHost := startRegistry(t)

	sourceIndex := mustRandomIndex(t, 3)
	source := sourceHost + "/grog/source:1"
	if err := remote.WriteIndex(mustParse(t, source), sourceIndex); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	sourceDigest := mustDigest(t, sourceIndex)

	destination := destinationHost + "/grog/destination:1"
	skipped, err := Copy(context.Background(), source, destination, Options{
		SourceInsecure:      true,
		DestinationInsecure: true,
	})
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if skipped {
		t.Fatal("Copy reported a skip against an empty destination")
	}

	if got := headDigest(t, destination); got != sourceDigest {
		t.Errorf("destination digest = %s, want the source index digest %s", got, sourceDigest)
	}

	pushedIndex, err := remote.Index(mustParse(t, destination))
	if err != nil {
		t.Fatalf("read destination index: %v", err)
	}
	pushedManifest, err := pushedIndex.IndexManifest()
	if err != nil {
		t.Fatalf("read destination index manifest: %v", err)
	}
	if len(pushedManifest.Manifests) != 3 {
		t.Errorf("destination holds %d children, want all 3 platforms", len(pushedManifest.Manifests))
	}
}

// TestVerifyDestinationRejectsMismatch covers the case a green push log cannot
// show: the write returned success but the tag serves something else.
func TestVerifyDestinationRejectsMismatch(t *testing.T) {
	destinationHost := startRegistry(t)

	served := mustRandomImage(t, 256, 1)
	destination := destinationHost + "/grog/destination:1"
	if err := remote.Write(mustParse(t, destination), served); err != nil {
		t.Fatalf("seed destination: %v", err)
	}

	otherDigest := mustDigest(t, mustRandomImage(t, 512, 2))
	if err := verifyDestination(context.Background(), mustParse(t, destination), otherDigest, destination); err == nil {
		t.Fatal("verifyDestination accepted a destination serving a different digest")
	}

	if err := verifyDestination(context.Background(), mustParse(t, destination), mustDigest(t, served), destination); err != nil {
		t.Errorf("verifyDestination rejected a matching destination: %v", err)
	}
}

func TestCopySkipsWhenDestinationIsCurrent(t *testing.T) {
	sourceHost := startRegistry(t)
	destinationHost := startRegistry(t)

	sourceIndex := mustRandomIndex(t, 2)
	source := sourceHost + "/grog/source:1"
	if err := remote.WriteIndex(mustParse(t, source), sourceIndex); err != nil {
		t.Fatalf("seed source: %v", err)
	}

	destination := destinationHost + "/grog/destination:1"
	opts := Options{SourceInsecure: true, DestinationInsecure: true}
	if _, err := Copy(context.Background(), source, destination, opts); err != nil {
		t.Fatalf("first Copy: %v", err)
	}

	skipped, err := Copy(context.Background(), source, destination, opts)
	if err != nil {
		t.Fatalf("second Copy: %v", err)
	}
	if !skipped {
		t.Error("second Copy re-pushed an already current destination")
	}
}

func TestCopyUpdatesStaleDestination(t *testing.T) {
	sourceHost := startRegistry(t)
	destinationHost := startRegistry(t)

	destination := destinationHost + "/grog/destination:1"
	if err := remote.Write(mustParse(t, destination), mustRandomImage(t, 256, 1)); err != nil {
		t.Fatalf("seed destination: %v", err)
	}

	fresh := mustRandomImage(t, 256, 1)
	source := sourceHost + "/grog/source:1"
	if err := remote.Write(mustParse(t, source), fresh); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	freshDigest := mustDigest(t, fresh)

	skipped, err := Copy(context.Background(), source, destination, Options{
		SourceInsecure:      true,
		DestinationInsecure: true,
	})
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if skipped {
		t.Fatal("Copy skipped a stale destination")
	}
	if got := headDigest(t, destination); got != freshDigest {
		t.Errorf("destination digest = %s, want %s", got, freshDigest)
	}
}
