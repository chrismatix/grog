package label

import "testing"

func TestParsePartialTargetPattern(t *testing.T) {
	currentPkg := "foo/bar"

	cases := []struct {
		pattern       string
		prefix        string
		target        string
		recursive     bool
		prefixPartial bool
	}{
		{":baz", "foo/bar", "baz", false, false},
		{"//foo:b", "foo", "b", false, false},
		{"//foo:...", "foo", "...", false, false},
		{"//foo/...", "foo", "", true, false},
		{"//foo/...:b", "foo", "b", true, false},
		{"qux", "foo/bar", "qux", false, false},
		{"//fo", "fo", "", false, true},
		{"//foofoo", "foofoo", "", false, true},
	}

	for _, c := range cases {
		actual := ParsePartialTargetPattern(currentPkg, c.pattern)
		if actual.prefix != c.prefix || actual.targetPattern != c.target || actual.recursive != c.recursive || actual.isPrefixPartial != c.prefixPartial {
			t.Errorf("ParsePartialTargetPattern(%q) = {prefix:%q target:%q recursive:%v}; want {prefix:%q target:%q recursive:%v}", c.pattern, actual.prefix, actual.targetPattern, actual.recursive, c.prefix, c.target, c.recursive)
		}
	}
}
