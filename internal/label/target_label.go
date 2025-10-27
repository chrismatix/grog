package label

import (
	"fmt"
	"sort"
	"strings"
)

// TargetLabel represents a Bazel target label, e.g. "//package/path:target".
// It supports shorthand notation "//package/path" which is equivalent to "//package/path:basename",
// where "basename" is the last element of the package path.
type TargetLabel struct {
	Package string `json:"package"` // e.g. "package/path" (for root package, it may be empty)
	Name    string `json:"name"`    // e.g. "target"
}

// ParseTargetLabel parses a Bazel-style target label relative to the given packagePath.
// It accepts both explicit labels ("//pkg:target"), shorthand labels ("//pkg"),
// and relative labels (":target") for the current packagePath.
// I.e. "path", ":target" -> "//path:target"
func ParseTargetLabel(packagePath, label string) (TargetLabel, error) {
	if strings.HasPrefix(label, ":") {
		if packagePath == "." {
			// For the root package we omit the "."
			packagePath = ""
		}

		// Relative label: use the current package path with the given target name.
		targetName := label[1:]
		if targetName == "" {
			return TargetLabel{}, fmt.Errorf("invalid relative label %q: target name is empty", label)
		}
		if err := validateName(targetName); err != nil {
			return TargetLabel{}, err
		}
		return TargetLabel{Package: packagePath, Name: targetName}, nil
	}

	if !strings.HasPrefix(label, "//") {
		return TargetLabel{}, fmt.Errorf("invalid label %q: must start with \"//\" or \":\"", label)
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

	if err := validateName(name); err != nil {
		return TargetLabel{}, err
	}
	return TargetLabel{Package: pkg, Name: name}, nil
}

func validateName(name string) error {
	if name == "" {
		return fmt.Errorf("target name is empty")
	}
	for _, c := range name {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '_' || c == '-' || c == '.':
		default:
			return fmt.Errorf("invalid character %q in target name %q: only alphanumeric characters, underscores, dots, and dashes are allowed", c, name)
		}
	}
	return nil
}

// TL is a convenience shorthand for tests
func TL(packagePath, label string) TargetLabel {
	return TargetLabel{Package: packagePath, Name: label}
}

// String returns the canonical form "//pkg:target".
func (t TargetLabel) String() string {
	return "//" + t.Package + ":" + t.Name
}

// CanBeShortened returns true if the //foo/bar:bar -> //foo/bar shorthand can be used for this label.
func (t TargetLabel) CanBeShortened() bool {
	packageDir := strings.Split(t.Package, "/")[len(strings.Split(t.Package, "/"))-1]
	return t.Name == packageDir
}

func (t TargetLabel) IsTest() bool {
	return strings.HasSuffix(t.Name, "test")
}

func PrintSorted(labels []TargetLabel) {
	var result []string
	for _, label := range labels {
		result = append(result, label.String())
	}

	sort.Strings(result)
	for _, s := range result {
		fmt.Println(s)
	}
}
