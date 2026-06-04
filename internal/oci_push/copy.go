// Package oci_push performs daemon-free registry-to-registry image copies via
// go-containerregistry. Used by oci output handlers to ship cached images to
// oci-push:: destinations.
package oci_push

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
)

// Options control a single Copy call. Plain-HTTP access for either side is
// the caller's decision — Copy applies name.Insecure when these flags are set
// and never auto-detects from the URL.
type Options struct {
	// SourceInsecure permits plain-HTTP access for the source ref.
	SourceInsecure bool

	// DestinationInsecure permits plain-HTTP access for the destination ref.
	// Callers compute this from their configured insecure_registries list.
	DestinationInsecure bool

	// MaxAttempts caps total tries; transient errors are retried up to this
	// count with exponential backoff. Defaults to 3.
	MaxAttempts int

	// InitialBackoff is the wait before the first retry; doubles per
	// attempt. Defaults to 500ms.
	InitialBackoff time.Duration
}

// Copy ships an image from source to destination. Returns (skipped, nil) when
// the destination already holds the same manifest digest as the source.
func Copy(ctx context.Context, source, destination string, opts Options) (bool, error) {
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 3
	}
	if opts.InitialBackoff <= 0 {
		opts.InitialBackoff = 500 * time.Millisecond
	}

	srcRef, err := parseRef(source, opts.SourceInsecure)
	if err != nil {
		return false, fmt.Errorf("parse source %q: %w", source, err)
	}
	dstRef, err := parseRef(destination, opts.DestinationInsecure)
	if err != nil {
		return false, fmt.Errorf("parse destination %q: %w", destination, err)
	}

	srcImg, err := remote.Image(srcRef,
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
	)
	if err != nil {
		return false, fmt.Errorf("fetch source manifest %q: %w", source, err)
	}

	srcDigest, err := srcImg.Digest()
	if err != nil {
		return false, fmt.Errorf("read source digest: %w", err)
	}

	dstDesc, err := remote.Head(dstRef,
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
	)
	if err == nil && dstDesc != nil && dstDesc.Digest == srcDigest {
		return true, nil
	}

	backoff := opts.InitialBackoff
	var lastErr error
	for attempt := 1; attempt <= opts.MaxAttempts; attempt++ {
		err := writeImage(ctx, dstRef, srcImg)
		if err == nil {
			return false, nil
		}
		lastErr = err
		if !isTransient(err) || attempt == opts.MaxAttempts {
			return false, wrapInsecureHint(destination, opts.DestinationInsecure, err)
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
	}
	return false, wrapInsecureHint(destination, opts.DestinationInsecure, lastErr)
}

func writeImage(ctx context.Context, dst name.Reference, img v1.Image) error {
	return remote.Write(dst, img,
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
	)
}

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

// isTransient reports whether err is worth retrying: network failures and 5xx
// responses retry; auth (401/403) and manifest validation (4xx) do not.
func isTransient(err error) bool {
	if err == nil {
		return false
	}
	if terr, ok := errors.AsType[*transport.Error](err); ok {
		return terr.StatusCode >= 500 || terr.StatusCode == http.StatusRequestTimeout || terr.StatusCode == http.StatusTooManyRequests
	}
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

// wrapInsecureHint annotates a push failure with a pointer at
// oci.insecure_registries when the failure looks like an HTTPS attempt against
// a plain-HTTP server. Only fires when the caller did NOT already mark the
// destination as insecure — once the user has opted in, the wrong hint would
// just be noise.
func wrapInsecureHint(destination string, destinationInsecure bool, err error) error {
	if destinationInsecure || !looksLikeTLSToHTTP(err) {
		return err
	}
	return fmt.Errorf(
		"push to %q failed: %w (if this is an HTTP-only registry, add its host to oci.insecure_registries in grog.toml)",
		destination, err,
	)
}

// looksLikeTLSToHTTP matches the canonical errors Go's TLS client emits when
// it tries to handshake against a server that immediately responds in plain
// HTTP. Catching these specifically (rather than any push error) keeps the
// hint useful: a true auth/network failure on an HTTPS registry still surfaces
// its own message.
func looksLikeTLSToHTTP(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "tls: first record does not look like a TLS handshake") ||
		strings.Contains(msg, "http: server gave HTTP response to HTTPS client")
}
