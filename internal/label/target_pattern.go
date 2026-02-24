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

	// Used for partial target patterns. true if the package path is incomplete
	isPrefixPartial bool
}

// ParseTargetPattern parses a Bazel target pattern.
func ParseTargetPattern(currentPackage string, pattern string) (TargetPattern, error) {
	if !strings.HasPrefix(pattern, "//") {
		colonIdx := strings.Index(pattern, ":")
		if colonIdx == -1 {
			return TargetPattern{}, fmt.Errorf("invalid pattern %q: relative patterns must use ':'", pattern)
		}

		targetName := pattern[colonIdx+1:]
		if targetName != "..." {
			if err := validateName(targetName); err != nil {
				return TargetPattern{}, err
			}
		}

		return TargetPattern{prefix: currentPackage, targetPattern: targetName}, nil
	}
	body := pattern[2:]
	var prefix, targetPattern string
	recursive := false

	colonIndex := strings.Index(body, ":")
	packagePart := body
	if colonIndex != -1 {
		packagePart = body[:colonIndex]
		targetPattern = body[colonIndex+1:]
		if targetPattern == "" {
			return TargetPattern{}, fmt.Errorf("invalid pattern %q: target pattern is empty", pattern)
		}
	}

	ellipsisIndex := strings.Index(packagePart, "...")
	if ellipsisIndex != -1 {
		if len(packagePart) > ellipsisIndex+3 {
			return TargetPattern{}, fmt.Errorf("invalid pattern %q: unexpected characters after '...'", pattern)
		}
		recursive = true
		prefix = packagePart[:ellipsisIndex]
	} else {
		prefix = packagePart
		if colonIndex == -1 {
			// Shorthand: "//foo" is equivalent to "//foo:foo"
			targetPattern = packagePart[strings.LastIndex(packagePart, "/")+1:]
			if targetPattern == "" {
				return TargetPattern{}, fmt.Errorf("invalid pattern %q: target pattern is empty", pattern)
			}
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

// ParsePartialTargetPattern parses a target pattern but is lenient towards
// incomplete patterns. It is primarily used for shell completions where the
// user might not have typed the full pattern yet. Any missing parts are simply
// returned empty without an error.
func ParsePartialTargetPattern(currentPackage, pattern string) TargetPattern {
	if strings.HasPrefix(pattern, ":") {
		if currentPackage == "." {
			currentPackage = ""
		}
		return TargetPattern{prefix: currentPackage, targetPattern: pattern[1:]}
	}

	var colonIndex int
	if !strings.HasPrefix(pattern, "//") {
		// Relative pattern without explicit ":" or shorthand.
		colonIndex = strings.Index(pattern, ":")
		var targetName string
		if colonIndex == -1 {
			targetName = pattern
		} else {
			targetName = pattern[colonIndex+1:]
		}
		return TargetPattern{prefix: currentPackage, targetPattern: targetName}
	}

	body := pattern[2:]
	prefix := body
	targetPattern := ""
	recursive := false
	isPrefixPartial := false

	colonIndex = strings.Index(body, ":")
	packagePart := body
	if colonIndex != -1 {
		packagePart = body[:colonIndex]
		targetPattern = body[colonIndex+1:]
	}

	if ellipsisIndex := strings.Index(packagePart, "..."); ellipsisIndex != -1 {
		recursive = true
		prefix = packagePart[:ellipsisIndex]
	} else {
		prefix = packagePart
	}

	if len(prefix) > 0 {
		if prefix[len(prefix)-1] == '/' {
			prefix = prefix[:len(prefix)-1]
		} else if colonIndex <= 0 && !recursive {
			// We are dealing with a partial package path, e.g. //foo
			isPrefixPartial = true
		}
	}

	return TargetPattern{prefix: prefix, targetPattern: targetPattern, recursive: recursive, isPrefixPartial: isPrefixPartial}
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
	// Allow "all" to match any target.
	if p.targetPattern == "all" || p.targetPattern == "..." {
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

// Prefix returns the package prefix of the pattern.
func (p TargetPattern) Prefix() string { return p.prefix }

// Target returns the target pattern portion.
func (p TargetPattern) Target() string { return p.targetPattern }

// Recursive reports whether the pattern matches recursively.
func (p TargetPattern) Recursive() bool { return p.recursive }

// IsPrefixPartial reports whether the package prefix was incomplete for partial patterns.
func (p TargetPattern) IsPrefixPartial() bool { return p.isPrefixPartial }
