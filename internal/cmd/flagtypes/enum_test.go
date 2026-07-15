package flagtypes

import "testing"

func TestEnum(t *testing.T) {
	enum := NewEnum("one", "two")

	if err := enum.Set("two"); err != nil {
		t.Fatalf("set allowed value: %v", err)
	}
	if enum.Value != "two" {
		t.Fatalf("value = %q, want %q", enum.Value, "two")
	}
	if err := enum.Set("three"); err == nil {
		t.Fatal("expected disallowed value to fail")
	}
	if enum.Value != "two" {
		t.Fatalf("failed Set changed value to %q", enum.Value)
	}
}
