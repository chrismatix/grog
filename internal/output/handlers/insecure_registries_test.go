package handlers

import "testing"

func TestMatchesInsecureRegistry(t *testing.T) {
	cases := []struct {
		name string
		ref  string
		list []string
		want bool
	}{
		{"empty list", "127.0.0.1:5555/repo:1", nil, false},
		{"empty ref", "", []string{"127.0.0.1:5555"}, false},
		{"exact host:port", "127.0.0.1:5555/repo:1", []string{"127.0.0.1:5555"}, true},
		{"different port", "127.0.0.1:5556/repo:1", []string{"127.0.0.1:5555"}, false},
		{"host-only matches any port", "localhost:9000/repo:1", []string{"localhost"}, true},
		{"host:port does not match other ports", "localhost:9000/repo:1", []string{"localhost:5000"}, false},
		{"ipv6 bracketed", "[::1]:5555/repo:1", []string{"::1"}, true},
		{"multiple entries", "registry.local:5000/repo:1", []string{"127.0.0.1:5555", "registry.local"}, true},
		{"public registry stays secure", "gcr.io/proj/image:1", []string{"127.0.0.1:5555", "localhost"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := matchesInsecureRegistry(c.ref, c.list); got != c.want {
				t.Errorf("matchesInsecureRegistry(%q, %v) = %v, want %v", c.ref, c.list, got, c.want)
			}
		})
	}
}
