package oci_push

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
)

func TestLooksInsecure(t *testing.T) {
	cases := []struct {
		ref  string
		want bool
	}{
		{"localhost/repo/image:1", true},
		{"localhost:5555/repo/image:1", true},
		{"127.0.0.1:5555/repo/image:1", true},
		{"127.0.0.1/repo/image:1", true},
		{"[::1]:5555/repo/image:1", true},
		{"foo.localhost:5000/repo/image:1", true},
		{"registry.local:5000/repo/image:1", true},
		{"10.0.0.1:5000/repo/image:1", true},
		{"192.168.1.10:5000/repo/image:1", true},
		{"172.16.0.1:5000/repo/image:1", true},
		{"gcr.io/proj/image:1", false},
		{"us-east1-docker.pkg.dev/proj/repo/image:1", false},
		{"8.8.8.8/repo/image:1", false},
		{"", false},
	}
	for _, c := range cases {
		t.Run(c.ref, func(t *testing.T) {
			if got := looksInsecure(c.ref); got != c.want {
				t.Errorf("looksInsecure(%q) = %v, want %v", c.ref, got, c.want)
			}
		})
	}
}

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
