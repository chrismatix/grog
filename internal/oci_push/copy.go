// Package oci_push performs daemon-free registry-to-registry image copies via
// go-containerregistry. Used by docker output handlers to ship cached images
// to oci-push:: destinations.
package oci_push

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
)

// Options control a single Copy call. Plain-HTTP access for loopback / RFC1918
// / *.local hosts is auto-detected on both source and destination; no flag
// needed for the common cases (grog's loopback proxy, on-prem registries).
type Options struct {
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

	srcRef, err := parseRef(source)
	if err != nil {
		return false, fmt.Errorf("parse source %q: %w", source, err)
	}
	dstRef, err := parseRef(destination)
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

func parseRef(ref string) (name.Reference, error) {
	var opts []name.Option
	if looksInsecure(ref) {
		opts = append(opts, name.Insecure)
	}
	if strings.Contains(ref, "@") {
		return name.NewDigest(ref, opts...)
	}
	return name.ParseReference(ref, opts...)
}

// reLocalSuffix matches "*.localhost" or "*.local" registry hostnames, both of
// which container tooling treats as plain HTTP by convention.
var reLocalSuffix = regexp.MustCompile(`\.(localhost|local)(?::\d{1,5})?$`)

// looksInsecure reports whether ref's registry host is one of the patterns
// commonly served over plain HTTP: localhost, the loopback IPs, a *.local /
// *.localhost suffix, or any RFC1918 private IP. Tagging name.Insecure for
// these short-circuits go-containerregistry's parallel-ping discovery (it
// would otherwise race an HTTPS attempt that will never succeed) and lets
// pushes to non-public registries work without extra configuration.
func looksInsecure(ref string) bool {
	host := registryHost(ref)
	if host == "" {
		return false
	}
	if host == "localhost" || strings.HasPrefix(host, "localhost:") {
		return true
	}
	if reLocalSuffix.MatchString(host) {
		return true
	}
	if ip := net.ParseIP(stripPort(host)); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() {
			return true
		}
	}
	return false
}

// stripPort drops a trailing ":port" from host and unwraps a bracketed IPv6
// literal (`[::1]:5555` → `::1`). Returns host unchanged when there is no port.
func stripPort(host string) string {
	if strings.HasPrefix(host, "[") {
		if i := strings.Index(host, "]"); i > 0 {
			return host[1:i]
		}
		return host
	}
	if i := strings.LastIndex(host, ":"); i >= 0 {
		return host[:i]
	}
	return host
}

// registryHost returns the registry portion of an OCI reference (everything
// before the first "/"). Empty for malformed refs.
func registryHost(ref string) string {
	if i := strings.IndexByte(ref, '/'); i > 0 {
		return ref[:i]
	}
	return ""
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
