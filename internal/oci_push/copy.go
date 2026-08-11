// Package oci_push performs daemon-free registry-to-registry image copies
// via go-containerregistry.
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

// Options control a single Copy call. Plain-HTTP is opt-in per side; Copy
// never auto-detects insecurity from the URL.
type Options struct {
	SourceInsecure      bool
	DestinationInsecure bool

	// MaxAttempts caps total tries (default 3); InitialBackoff is the wait
	// before the first retry, doubling per attempt (default 500ms).
	MaxAttempts    int
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

	srcDesc, err := remote.Get(srcRef, remoteOptions(ctx)...)
	if err != nil {
		return false, fmt.Errorf("fetch source manifest %q: %w", source, err)
	}

	sourceManifest, err := sourceFor(srcDesc)
	if err != nil {
		return false, fmt.Errorf("read source %q: %w", source, err)
	}

	dstDesc, err := remote.Head(dstRef, remoteOptions(ctx)...)
	if err == nil && dstDesc != nil && dstDesc.Digest == srcDesc.Digest {
		return true, nil
	}

	if err := retryTransient(ctx, opts, func() error {
		return remote.Push(dstRef, sourceManifest, remoteOptions(ctx)...)
	}); err != nil {
		return false, wrapInsecureHint(destination, opts.DestinationInsecure, err)
	}

	// The write landed, so a read-back that fails or lags gets the same
	// patience the write itself got before the push is called a failure.
	return false, retryTransient(ctx, opts, func() error {
		return verifyDestination(ctx, dstRef, srcDesc.Digest, destination)
	})
}

// retryTransient runs attempt until it succeeds, fails for a reason not worth
// retrying, or runs out of attempts.
func retryTransient(ctx context.Context, opts Options, attempt func() error) error {
	backoff := opts.InitialBackoff
	var lastErr error
	for try := 1; try <= opts.MaxAttempts; try++ {
		err := attempt()
		if err == nil {
			return nil
		}
		lastErr = err
		if !isTransient(err) || try == opts.MaxAttempts {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
	}
	return lastErr
}

// verifyDestination guards the one failure a green build log cannot reveal: a
// write the registry accepted without making it visible under the tag.
func verifyDestination(ctx context.Context, dst name.Reference, want v1.Hash, destination string) error {
	desc, err := remote.Head(dst, remoteOptions(ctx)...)
	if err != nil {
		return fmt.Errorf("push to %q reported success but the destination could not be read back: %w", destination, err)
	}
	if desc.Digest != want {
		return fmt.Errorf("push to %q reported success but the registry serves %s, not the pushed %s",
			destination, desc.Digest, want)
	}
	return nil
}

func remoteOptions(ctx context.Context) []remote.Option {
	return []remote.Option{
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
	}
}

// sourceFor keeps a multi-platform index whole. Resolving it to a single child
// image would ship one platform and land a digest the build never produced.
func sourceFor(desc *remote.Descriptor) (remote.Taggable, error) {
	if desc.MediaType.IsIndex() {
		return desc.ImageIndex()
	}
	return desc.Image()
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

// wrapInsecureHint points the user at oci.insecure_registries when the
// failure looks like an HTTPS attempt against a plain-HTTP server.
func wrapInsecureHint(destination string, destinationInsecure bool, err error) error {
	if destinationInsecure || !looksLikeTLSToHTTP(err) {
		return err
	}
	return fmt.Errorf(
		"push to %q failed: %w (if this is an HTTP-only registry, add its host to oci.insecure_registries in grog.toml)",
		destination, err,
	)
}

// looksLikeTLSToHTTP matches the canonical TLS-on-HTTP errors.
func looksLikeTLSToHTTP(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "tls: first record does not look like a TLS handshake") ||
		strings.Contains(msg, "http: server gave HTTP response to HTTPS client")
}
