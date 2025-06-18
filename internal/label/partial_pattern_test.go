package label

import "testing"

func TestParsePartialTargetPattern(t *testing.T) {
	currentPkg := "foo/bar"

	cases := []struct {
		pattern   string
		prefix    string
		target    string
		recursive bool
	}{
		{":baz", "foo/bar", "baz", false},
		{"//foo:b", "foo", "b", false},
		{"//foo/...", "foo", "", true},
		{"//foo/...:b", "foo", "b", true},
		{"qux", "foo/bar", "qux", false},
	}

	for _, c := range cases {
		p := ParsePartialTargetPattern(currentPkg, c.pattern)
		if p.prefix != c.prefix || p.targetPattern != c.target || p.recursive != c.recursive {
			t.Errorf("ParsePartialTargetPattern(%q) = {prefix:%q target:%q recursive:%v}; want {prefix:%q target:%q recursive:%v}", c.pattern, p.prefix, p.targetPattern, p.recursive, c.prefix, c.target, c.recursive)
		}
	}
}
