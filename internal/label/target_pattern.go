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
func ParseTargetPattern(currentPackage string, pattern string) (TargetPattern, error) {
	if !strings.HasPrefix(pattern, "//") {
		// We are running a package in the current directory
		// i.e. running :foo in pkg/path
		colonIdx := strings.Index(pattern, ":")
		var targetName string
		if colonIdx == -1 {
			// Shorthand: "foo" is equivalent to ":foo"
			targetName = pattern
		} else {
			targetName = pattern[colonIdx+1:]
		}
		if err := validateName(targetName); err != nil {
			return TargetPattern{}, err
		}

		return TargetPattern{prefix: currentPackage, targetPattern: targetName}, nil
	}
	body := pattern[2:]
	var prefix, targetPattern string
	recursive := false

	// Look for the "..." wildcard.
	idx := strings.Index(body, "...")
	if idx != -1 {
		recursive = true
		prefix = body[:idx]
		// If there is a target filter after "..."
		if len(body) > idx+3 && body[idx+3] == ':' {
			targetPattern = body[idx+4:]
			if targetPattern == "" {
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
			targetPattern = body[strings.LastIndex(body, "/")+1:]
		} else {
			prefix = body[:colonIdx]
			targetPattern = body[colonIdx+1:]
		}

		if targetPattern == "" {
			return TargetPattern{}, fmt.Errorf("invalid pattern %q: target pattern is empty", pattern)
		}
	}

	// Normalize the prefix by removing a trailing slash if present.
	if len(prefix) > 0 && prefix[len(prefix)-1] == '/' {
		prefix = prefix[:len(prefix)-1]
	}
	return TargetPattern{prefix: prefix, targetPattern: targetPattern, recursive: recursive}, nil
}

type TargetPatternSet = []TargetPattern

func ParsePatternsOrMatchAll(currentPackage string, patterns []string) ([]TargetPattern, error) {
	var result []TargetPattern
	for _, pattern := range patterns {
		p, err := ParseTargetPattern(currentPackage, pattern)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	if len(result) == 0 {
		return []TargetPattern{GetMatchAllTargetPattern()}, nil
	}

	return result, nil
}

func PatternSetToString(patterns []TargetPattern) string {
	var result []string
	for _, p := range patterns {
		result = append(result, p.String())
	}
	return strings.Join(result, " ")
}

func TargetPatternFromLabel(label TargetLabel) TargetPattern {
	return TargetPattern{prefix: label.Package, targetPattern: label.Name, recursive: false}
}

func GetMatchAllTargetPattern() TargetPattern {
	return TargetPattern{prefix: "", recursive: true}
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
		if p.prefix != "" {
			base += "/..."
		} else {
			// For root wild-cards
			base += "..."
		}
	}
	if p.targetPattern != "" {
		base += ":" + p.targetPattern
	}
	return base
}
