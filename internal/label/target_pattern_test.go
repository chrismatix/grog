package label

import "testing"

func TestTargetPatternMatching(t *testing.T) {
	// Helper to parse a TargetLabel for tests.
	tl := func(label string) TargetLabel {
		tlabel, err := ParseTargetLabel("", label)
		if err != nil {
			t.Fatalf("Failed to parse test label %q: %v", label, err)
		}
		return tlabel
	}

	// Pattern: all targets under "//foo/..."
	patternFooAll, err := ParseTargetPattern("", "//foo/...")
	if err != nil {
		t.Fatalf("Failed to parse pattern: %v", err)
	}
	matches := []string{
		"//foo:target1",
		"//foo:target2",
		"//foo/sub:lib",
		"//foo/sub/deeper:lib",
	}
	for _, label := range matches {
		if !patternFooAll.Matches(tl(label)) {
			t.Errorf("Pattern %q should match %q", patternFooAll.String(), label)
		}
	}
	nonMatches := []string{
		"//foobar:xyz",
		"//fo:abc",
		"//bar/foo:abc",
	}
	for _, label := range nonMatches {
		if patternFooAll.Matches(tl(label)) {
			t.Errorf("Pattern %q should NOT match %q", patternFooAll.String(), label)
		}
	}

	// Pattern: all targets in entire repo ("//...")
	patternAll, err := ParseTargetPattern("", "//...")
	if err != nil {
		t.Fatalf("Failed to parse pattern: %v", err)
	}
	if !patternAll.Matches(tl("//foo:bar")) || !patternAll.Matches(tl("//foo/bar:baz")) {
		t.Error("//... should match any target in any package")
	}

	if patternAll.String() != "//..." {
		t.Errorf("PatternAll.String() should be \"//...\", got %q", patternAll.String())
	}

	// Pattern with specific target filter: "//foo/...:lib" matches only targets named "lib"
	patFooLib, err := ParseTargetPattern("", "//foo/...:lib")
	if err != nil {
		t.Fatalf("Failed to parse pattern: %v", err)
	}
	if !patFooLib.Matches(tl("//foo:lib")) || !patFooLib.Matches(tl("//foo/sub:lib")) {
		t.Error("Pattern //foo/...:lib should match targets named lib in package foo or its subpackages")
	}
	if patFooLib.Matches(tl("//foo:other")) {
		t.Error("Pattern //foo/...:lib should NOT match target //foo:other (name differs)")
	}

	// Pattern without recursion (exact package match): e.g. "//foo:bar"
	patternExact, err := ParseTargetPattern("", "//foo:bar")
	if err != nil {
		t.Fatalf("Failed to parse pattern: %v", err)
	}
	if patternExact.Matches(tl("//foo:baz")) {
		t.Error("Pattern //foo:bar should not match //foo:baz")
	}
	if !patternExact.Matches(tl("//foo:bar")) {
		t.Error("Pattern //foo:bar should match //foo:bar exactly")
	}

	// Pattern with short-hand notation: "//foo"
	patternShortHandExact, err := ParseTargetPattern("", "//foo")
	if err != nil {
		t.Fatalf("Failed to parse pattern: %v", err)
	}
	if !patternShortHandExact.Matches(tl("//foo:foo")) {
		t.Error("Pattern //foo should match //foo:foo exactly")
	}
	if patternShortHandExact.Matches(tl("//foo:bar")) {
		t.Error("Pattern //foo should not match //foo:bar")
	}
}

func TestCurrentPackageTargetPattern(t *testing.T) {
	currentPkg := "pkg/path"

	// Relative pattern without colon is interpreted as shorthand for ":target"
	patternRelative, err := ParseTargetPattern(currentPkg, "foo")
	if err != nil {
		t.Fatalf("Failed to parse relative pattern: %v", err)
	}

	// Should match a label in the current package with target "foo"
	if !patternRelative.Matches(TargetLabel{Package: currentPkg, Name: "foo"}) {
		t.Error("Relative pattern 'foo' should match target 'foo' in current package")
	}
	// Should not match labels in other packages even if the target name is "foo"
	if patternRelative.Matches(TargetLabel{Package: "other/pkg", Name: "foo"}) {
		t.Error("Relative pattern 'foo' should not match targets in a different package")
	}

	// Relative pattern with explicit colon, e.g. ":bar", should have target "bar"
	patternColon, err := ParseTargetPattern(currentPkg, ":bar")
	if err != nil {
		t.Fatalf("Failed to parse relative pattern with colon: %v", err)
	}
	if !patternColon.Matches(TargetLabel{Package: currentPkg, Name: "bar"}) {
		t.Error("Relative pattern ':bar' should match target 'bar' in current package")
	}
	if patternColon.Matches(TargetLabel{Package: currentPkg, Name: "foo"}) {
		t.Error("Relative pattern ':bar' should not match target 'foo' in current package")
	}
}
