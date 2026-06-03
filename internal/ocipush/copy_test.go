package ocipush

import (
	"context"
	"errors"
	"net/http"
	"testing"

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
