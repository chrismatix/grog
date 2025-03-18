package label

import (
	"fmt"
	"strings"
)

// TargetPattern represents a Bazel target pattern, e.g. "//pkg/..." or "//pkg/...:target".
// It supports recursive (hierarchical) matching using the "..." wildcard.
type TargetPattern struct {
	prefix        string // package prefix (without trailing slash)
	targetPattern string // target name filter (if empty, matches any target)
	recursive     bool   // true if "..." is used for recursive matching
}

// ParseTargetPattern parses a Bazel target pattern.
func ParseTargetPattern(pattern string) (TargetPattern, error) {
	if !strings.HasPrefix(pattern, "//") {
		return TargetPattern{}, fmt.Errorf("invalid pattern %q: must start with \"//\"", pattern)
	}
	body := pattern[2:]
	var prefix, targetPat string
	recursive := false

	// Look for the "..." wildcard.
	idx := strings.Index(body, "...")
	if idx != -1 {
		recursive = true
		prefix = body[:idx]
		// If there is a target filter after "..."
		if len(body) > idx+3 && body[idx+3] == ':' {
			targetPat = body[idx+4:]
			if targetPat == "" {
				return TargetPattern{}, fmt.Errorf("invalid pattern %q: target pattern after ':' is empty", pattern)
			}
		} else if len(body) > idx+3 {
			// Unexpected characters after "..."
			return TargetPattern{}, fmt.Errorf("invalid pattern %q: unexpected characters after '...'", pattern)
		}
	} else {
		// No "..." present: expect an exact package with an optional colon.
		colonIdx := strings.Index(body, ":")
		if colonIdx == -1 {
			// Shorthand: "//foo" is equivalent to "//foo:foo"
			prefix = body
			targetPat = body[strings.LastIndex(body, "/")+1:]
		} else {
			prefix = body[:colonIdx]
			targetPat = body[colonIdx+1:]
		}

		if targetPat == "" {
			return TargetPattern{}, fmt.Errorf("invalid pattern %q: target pattern is empty", pattern)
		}
	}

	// Normalize prefix by removing a trailing slash if present.
	if len(prefix) > 0 && prefix[len(prefix)-1] == '/' {
		prefix = prefix[:len(prefix)-1]
	}
	return TargetPattern{prefix: prefix, targetPattern: targetPat, recursive: recursive}, nil
}

// Matches returns true if the given TargetLabel matches the pattern.
func (p TargetPattern) Matches(t TargetLabel) bool {
	// Package matching.
	if p.recursive {
		// If prefix is non-empty, package must equal prefix or start with "prefix/".
		if p.prefix != "" {
			if t.Package != p.prefix && !strings.HasPrefix(t.Package, p.prefix+"/") {
				return false
			}
		}
		// If prefix is empty, pattern is "//...", which matches any package.
	} else {
		// Non-recursive: require an exact package match.
		if t.Package != p.prefix {
			return false
		}
	}
	// Target name matching.
	if p.targetPattern == "" {
		return true
	}
	// Allow wildcard-like target patterns "*"/"all" to match any target.
	if p.targetPattern == "*" || p.targetPattern == "all" {
		return true
	}
	return t.Name == p.targetPattern
}

// String returns a string representation of the TargetPattern.
func (p TargetPattern) String() string {
	base := "//" + p.prefix
	if p.recursive {
		base += "/..."
	}
	if p.targetPattern != "" {
		base += ":" + p.targetPattern
	}
	return base
}
