// Package ocipush implements daemon-free registry-to-registry image copies for
// oci-push:: outputs.
//
// The copier streams blobs and manifests over the OCI Distribution v2 API
// using go-containerregistry. Layers that already exist at the destination are
// skipped (HEAD-probe + cross-repo blob mount when the source and destination
// share a registry host). The local Docker daemon is never involved — bytes
// flow directly from the source registry (or grog's in-process loopback proxy
// for fs cache backends) to the destination.
package ocipush

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
)

// Options control a single Copy call.
type Options struct {
	// SourceInsecure permits plain-HTTP access for the source. Used for
	// grog's loopback dockerproxy, which serves OCI over plain HTTP on a
	// localhost port. Destination is always treated as secure.
	SourceInsecure bool

	// MaxAttempts caps total push tries (initial + retries). Must be >= 1.
	// Retries apply only to transient errors (5xx, network). Non-transient
	// errors (4xx auth/validation) fail immediately. Defaults to 4.
	MaxAttempts int

	// InitialBackoff is the wait before the first retry. Doubles per
	// retry attempt. Defaults to 500ms.
	InitialBackoff time.Duration
}

// Copy ships an image from source to destination. Both refs are full registry
// paths (e.g. "us-east1-docker.pkg.dev/proj/repo/image:1.2.3"). When source and
// destination resolve to the same registry host, layers are blob-mounted server-
// side instead of being streamed through the client; otherwise blobs stream
// through the process but never through the local Docker daemon.
//
// Copy is a no-op when the destination already holds an image with a manifest
// digest identical to the source's: the HEAD probe sees a match and skips.
// This makes Copy idempotent for re-runs of the same build invocation.
func Copy(ctx context.Context, source, destination string, opts Options) error {
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 4
	}
	if opts.InitialBackoff <= 0 {
		opts.InitialBackoff = 500 * time.Millisecond
	}

	srcRef, err := parseRef(source, opts.SourceInsecure)
	if err != nil {
		return fmt.Errorf("parse source %q: %w", source, err)
	}
	dstRef, err := parseRef(destination, false)
	if err != nil {
		return fmt.Errorf("parse destination %q: %w", destination, err)
	}

	srcImg, err := remote.Image(srcRef,
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
	)
	if err != nil {
		return fmt.Errorf("fetch source manifest %q: %w", source, err)
	}

	srcDigest, err := srcImg.Digest()
	if err != nil {
		return fmt.Errorf("read source digest: %w", err)
	}

	// HEAD probe: if the destination tag already points at the same manifest
	// we'd push, skip. Saves a registry round-trip on rebuilds of unchanged
	// images (a cache-hit build that re-issues --push under the same VERSION).
	dstDesc, err := remote.Head(dstRef,
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
	)
	if err == nil && dstDesc != nil && dstDesc.Digest == srcDigest {
		return nil
	}

	backoff := opts.InitialBackoff
	var lastErr error
	for attempt := 1; attempt <= opts.MaxAttempts; attempt++ {
		err := writeImage(ctx, dstRef, srcImg)
		if err == nil {
			return nil
		}
		lastErr = err
		if !isTransient(err) || attempt == opts.MaxAttempts {
			return fmt.Errorf("push %q -> %q: %w", source, destination, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
	}
	return fmt.Errorf("push %q -> %q after %d attempts: %w", source, destination, opts.MaxAttempts, lastErr)
}

// CopyWithCrane is a convenience that uses crane.Copy for a simpler call site
// when retries and probes aren't needed (kept for completeness / testing).
func CopyWithCrane(source, destination string, sourceInsecure bool) error {
	srcRef, err := parseRef(source, sourceInsecure)
	if err != nil {
		return err
	}
	dstRef, err := parseRef(destination, false)
	if err != nil {
		return err
	}
	return crane.Copy(srcRef.String(), dstRef.String(),
		crane.WithAuthFromKeychain(authn.DefaultKeychain),
	)
}

func writeImage(ctx context.Context, dst name.Reference, img v1.Image) error {
	return remote.Write(dst, img,
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
	)
}

// parseRef chooses Tag vs Digest parsing based on the "@" separator and
// applies name.Insecure when the caller permits plain HTTP for the source
// (grog's loopback proxy lives on 127.0.0.1:<port> and never speaks TLS).
func parseRef(ref string, insecure bool) (name.Reference, error) {
	var opts []name.Option
	if insecure {
		opts = append(opts, name.Insecure)
	}
	if strings.Contains(ref, "@") {
		return name.NewDigest(ref, opts...)
	}
	return name.ParseReference(ref, opts...)
}

// isTransient classifies errors as worth retrying. Network failures and 5xx
// responses are transient; auth (401/403) and manifest validation (400/404)
// are not — retrying them just delays the inevitable failure.
func isTransient(err error) bool {
	if err == nil {
		return false
	}
	if terr, ok := errors.AsType[*transport.Error](err); ok {
		if terr.StatusCode >= 500 || terr.StatusCode == http.StatusRequestTimeout || terr.StatusCode == http.StatusTooManyRequests {
			return true
		}
		return false
	}
	// Non-transport errors are typically network/dial failures — treat as
	// transient. Context cancellation is also wrapped this way but the outer
	// loop checks ctx.Done() before each retry so we won't loop forever.
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}
