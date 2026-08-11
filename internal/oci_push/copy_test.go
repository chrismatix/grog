package oci_push

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
)

func TestIsTransient(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"context canceled", context.Canceled, false},
		{"context deadline", context.DeadlineExceeded, false},
		{"raw network", errors.New("dial tcp: connection refused"), true},
		{"401 auth", &transport.Error{StatusCode: http.StatusUnauthorized}, false},
		{"403 forbidden", &transport.Error{StatusCode: http.StatusForbidden}, false},
		{"404 not found", &transport.Error{StatusCode: http.StatusNotFound}, false},
		{"408 request timeout", &transport.Error{StatusCode: http.StatusRequestTimeout}, true},
		{"429 too many", &transport.Error{StatusCode: http.StatusTooManyRequests}, true},
		{"500 server", &transport.Error{StatusCode: http.StatusInternalServerError}, true},
		{"503 unavail", &transport.Error{StatusCode: http.StatusServiceUnavailable}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isTransient(c.err); got != c.want {
				t.Errorf("isTransient(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}

func TestRetryTransient(t *testing.T) {
	opts := Options{MaxAttempts: 3, InitialBackoff: time.Millisecond}

	t.Run("retries until success", func(t *testing.T) {
		calls := 0
		err := retryTransient(context.Background(), opts, func() error {
			calls++
			if calls < 3 {
				return &transport.Error{StatusCode: http.StatusServiceUnavailable}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("retryTransient: %v", err)
		}
		if calls != 3 {
			t.Errorf("attempted %d times, want 3", calls)
		}
	})

	t.Run("gives up on a permanent error", func(t *testing.T) {
		calls := 0
		err := retryTransient(context.Background(), opts, func() error {
			calls++
			return &transport.Error{StatusCode: http.StatusForbidden}
		})
		if err == nil {
			t.Fatal("expected the permanent error to surface")
		}
		if calls != 1 {
			t.Errorf("attempted %d times, want 1 — 403 is not worth retrying", calls)
		}
	})

	t.Run("stops at MaxAttempts", func(t *testing.T) {
		calls := 0
		err := retryTransient(context.Background(), opts, func() error {
			calls++
			return errors.New("dial tcp: connection refused")
		})
		if err == nil {
			t.Fatal("expected the last error to surface")
		}
		if calls != opts.MaxAttempts {
			t.Errorf("attempted %d times, want %d", calls, opts.MaxAttempts)
		}
	})
}
