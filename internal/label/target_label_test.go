package label

import "testing"

func TestParseTargetLabel(t *testing.T) {
	cases := []struct {
		input       string
		packagePath string
		wantPkg     string
		wantName    string
	}{
		{"//foo:bar", "", "foo", "bar"},
		{"//foo/bar:baz", "", "foo/bar", "baz"},
		// Allow any type of package paths
		{"//foo $!?/bar:Baz_-01", "", "foo $!?/bar", "Baz_-01"},
		{"//:root", "", "", "root"},
		{":root", "", "", "root"},
		{":root", ".", "", "root"},
		{"//:root", ".", "", "root"},
		// Shorthand: "//foo" should become "//foo:foo"
		{"//foo", "", "foo", "foo"},
		// Shorthand: "//foo/bar" should become "//foo/bar:bar"
		{"//foo/bar", "", "foo/bar", "bar"},
		// Relative
		{":relative", "current/pkg", "current/pkg", "relative"},
	}
	for _, c := range cases {
		tl, err := ParseTargetLabel(c.packagePath, c.input)
		if err != nil {
			t.Errorf("ParseTargetLabel(%q) error = %v, want no error", c.input, err)
			continue
		}
		if tl.Package != c.wantPkg || tl.Name != c.wantName {
			t.Errorf("ParseTargetLabel(%q) = {Package:%q, Name:%q}, want {Package:%q, Name:%q}",
				c.input, tl.Package, tl.Name, c.wantPkg, c.wantName)
		}
		// Check canonical string form always includes explicit target after colon.
		canonical := tl.String()
		expected := "//" + c.wantPkg + ":" + c.wantName
		if canonical != expected {
			t.Errorf("TargetLabel.String() = %q, want %q", canonical, expected)
		}
	}

	// Test invalid shorthand: only "//" should fail.
	invalid := []struct {
		input       string
		packagePath string
	}{
		{"foo:bar", ""},  // missing "//"
		{"/foo:bar", ""}, // missing one "/"
		{"//", ""},       // shorthand with empty package
		{"//:", ""},      // explicit but empty target name
	}
	for _, inp := range invalid {
		if _, err := ParseTargetLabel(inp.packagePath, inp.input); err == nil {
			t.Errorf("ParseTargetLabel(%q) should have failed, but got no error", inp.input)
		}
	}
}

func TestValidateTargetName(t *testing.T) {
	failureCases := []string{"//foo:b ar", "//foo:bar$"}
	for _, c := range failureCases {
		if _, err := ParseTargetLabel("", c); err == nil {
			t.Errorf("ValidateTargetName(%q) should have failed, but got no error", c)
		}
	}
}
