package traces

import (
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
		err  bool
	}{
		{"30d", 30 * 24 * time.Hour, false},
		{"72h", 72 * time.Hour, false},
		{"1d", 24 * time.Hour, false},
		{"", 0, true},
		{"x", 0, true},
		{"30x", 0, true},
		{"abch", 0, true},
	}
	for _, c := range cases {
		got, err := parseDuration(c.in)
		if c.err {
			if err == nil {
				t.Fatalf("%q: expected err", c.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%q: %v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("%q: got %v want %v", c.in, got, c.want)
		}
	}
}
