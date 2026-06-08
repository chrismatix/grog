package label

import "testing"

func TestTL_AndAccessors(t *testing.T) {
	l := TL("pkg/sub", "name")
	if l.Package != "pkg/sub" || l.Name != "name" {
		t.Fatalf("got %+v", l)
	}
	if l.String() != "//pkg/sub:name" {
		t.Fatalf("got %q", l.String())
	}
}

func TestTargetPattern_Accessors(t *testing.T) {
	p, err := ParseTargetPattern("", "//foo/...")
	if err != nil {
		t.Fatal(err)
	}
	if p.Prefix() != "foo" {
		t.Fatalf("prefix %q", p.Prefix())
	}
	if !p.Recursive() {
		t.Fatal("expected recursive")
	}
	if p.Target() != "" {
		t.Fatalf("target %q", p.Target())
	}
	if p.IsPrefixPartial() {
		t.Fatal("not partial")
	}

	p2, err := ParseTargetPattern("", "//foo:bar")
	if err != nil {
		t.Fatal(err)
	}
	if p2.Recursive() {
		t.Fatal("not recursive")
	}
	if p2.Target() != "bar" {
		t.Fatalf("target %q", p2.Target())
	}
}
