// Package cachectx defines per-request flags that callers can attach to a
// context.Context to influence cache backend behavior for a single operation.
//
// The flags live in their own leaf package because the cache backends sit
// underneath the caching package in the import graph and cannot import it.
package cachectx

import "context"

type ctxKey int

const (
	skipRemoteFetchKey ctxKey = iota
	skipRemoteUploadKey
	skipCASFetchKey
)

// WithSkipRemoteFetch returns a context that instructs the cache backend
// wrapper to skip the remote backend on read paths (Get/Exists/Size). Only
// the local FS cache is consulted; a local miss is reported as such instead
// of falling through to remote.
//
// Driven by the `no-cache-fetch` and `no-remote-cache` target tags.
func WithSkipRemoteFetch(ctx context.Context) context.Context {
	return context.WithValue(ctx, skipRemoteFetchKey, true)
}

// WithSkipRemoteUpload returns a context that instructs the cache backend
// wrapper to skip the remote backend on write paths (Set/BeginWrite). The
// local FS cache is still populated; the remote backend is never touched.
//
// Driven by the `no-remote-cache` target tag.
func WithSkipRemoteUpload(ctx context.Context) context.Context {
	return context.WithValue(ctx, skipRemoteUploadKey, true)
}

// WithSkipCASFetch returns a context that instructs the CAS to refuse to
// materialize outputs from any backend (local or remote). The on-disk
// fast-path in the output handlers still treats matching files as a cache
// hit; only when the handler would otherwise pull bytes from the CAS does
// this flag short-circuit with an error, causing the target to re-run.
//
// Driven by the `no-cache-fetch` target tag.
func WithSkipCASFetch(ctx context.Context) context.Context {
	return context.WithValue(ctx, skipCASFetchKey, true)
}

func IsSkipRemoteFetch(ctx context.Context) bool {
	v, _ := ctx.Value(skipRemoteFetchKey).(bool)
	return v
}

func IsSkipRemoteUpload(ctx context.Context) bool {
	v, _ := ctx.Value(skipRemoteUploadKey).(bool)
	return v
}

func IsSkipCASFetch(ctx context.Context) bool {
	v, _ := ctx.Value(skipCASFetchKey).(bool)
	return v
}
