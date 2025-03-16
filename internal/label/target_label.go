package label

import (
	"fmt"
	"strings"
)

// TargetLabel represents a Bazel target label, e.g. "//package/path:target".
// It supports shorthand notation "//package/path" which is equivalent to "//package/path:basename",
// where "basename" is the last element of the package path.
type TargetLabel struct {
	Package string // e.g. "package/path" (can be non-empty; for root package, it may be empty)
	Name    string // e.g. "target"
}

// ParseTargetLabel parses a Bazel target label.
// It accepts both explicit labels ("//pkg:target") and shorthand labels ("//pkg"),
// where the target name is inferred as the last element of the package path.
func ParseTargetLabel(label string) (TargetLabel, error) {
	if !strings.HasPrefix(label, "//") {
		return TargetLabel{}, fmt.Errorf("invalid label %q: must start with \"//\"", label)
	}
	body := label[2:]
	colonIndex := strings.Index(body, ":")
	var pkg, name string
	if colonIndex == -1 {
		// Shorthand: infer target name from the last element of the package path.
		pkg = body
		if pkg == "" {
			return TargetLabel{}, fmt.Errorf("invalid shorthand label %q: package path is empty", label)
		}
		parts := strings.Split(pkg, "/")
		name = parts[len(parts)-1]
	} else {
		pkg = body[:colonIndex]
		name = body[colonIndex+1:]
		if name == "" {
			return TargetLabel{}, fmt.Errorf("invalid label %q: target name is empty", label)
		}
	}
	return TargetLabel{Package: pkg, Name: name}, nil
}

// String returns the canonical form "//pkg:target".
func (t TargetLabel) String() string {
	return "//" + t.Package + ":" + t.Name
}
