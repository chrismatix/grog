// Package oci_push performs daemon-free registry-to-registry image copies via
// go-containerregistry. Used by docker output handlers to ship cached images
// to oci-push:: destinations.
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

// Options control a single Copy call.
type Options struct {
	// SourceInsecure permits plain-HTTP access for the source (e.g. the
	// loopback dockerproxy). Destination is always treated as secure.
	SourceInsecure bool

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
	dstRef, err := parseRef(destination, false)
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
			return false, fmt.Errorf("push %q -> %q: %w", source, destination, err)
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
	}
	return false, fmt.Errorf("push %q -> %q after %d attempts: %w", source, destination, opts.MaxAttempts, lastErr)
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
