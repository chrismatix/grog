package loading

import (
	"strings"
	"testing"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

func TestStarlarkListToStringSlice(t *testing.T) {

	good := starlark.NewList([]starlark.Value{starlark.String("a"), starlark.String("b")})
	got, err := starlarkListToStringSlice(good)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("unexpected: %v", got)
	}

	bad := starlark.NewList([]starlark.Value{starlark.MakeInt(5)})
	if _, err := starlarkListToStringSlice(bad); err == nil {
		t.Fatal("expected error")
	}

	empty := starlark.NewList(nil)
	got, err = starlarkListToStringSlice(empty)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %v", got)
	}
}

func TestStarlarkDictToStringMap(t *testing.T) {

	d := starlark.NewDict(2)
	_ = d.SetKey(starlark.String("k1"), starlark.String("v1"))
	_ = d.SetKey(starlark.String("k2"), starlark.String("v2"))
	m, err := starlarkDictToStringMap(d)
	if err != nil {
		t.Fatal(err)
	}
	if m["k1"] != "v1" || m["k2"] != "v2" {
		t.Fatalf("unexpected: %v", m)
	}

	badKey := starlark.NewDict(1)
	_ = badKey.SetKey(starlark.MakeInt(1), starlark.String("v"))
	if _, err := starlarkDictToStringMap(badKey); err == nil {
		t.Fatal("expected error for non-string key")
	}

	badVal := starlark.NewDict(1)
	_ = badVal.SetKey(starlark.String("k"), starlark.MakeInt(1))
	if _, err := starlarkDictToStringMap(badVal); err == nil {
		t.Fatal("expected error for non-string value")
	}
}

func TestStarlarkListToOutputChecks_Struct(t *testing.T) {

	s := starlarkstruct.FromStringDict(starlark.String("struct"), starlark.StringDict{
		"command":         starlark.String("cmd"),
		"expected_output": starlark.String("out"),
	})
	list := starlark.NewList([]starlark.Value{s})
	checks, err := starlarkListToOutputChecks(list)
	if err != nil {
		t.Fatal(err)
	}
	if len(checks) != 1 || checks[0].Command != "cmd" || checks[0].ExpectedOutput != "out" {
		t.Fatalf("unexpected: %+v", checks)
	}
}

func TestStarlarkListToOutputChecks_StructMissingCommand(t *testing.T) {
	s := starlarkstruct.FromStringDict(starlark.String("struct"), starlark.StringDict{
		"expected_output": starlark.String("out"),
	})
	list := starlark.NewList([]starlark.Value{s})
	_, err := starlarkListToOutputChecks(list)
	if err == nil || !strings.Contains(err.Error(), "missing 'command'") {
		t.Fatalf("expected missing command error, got %v", err)
	}
}

func TestStarlarkListToOutputChecks_StructNoExpectedOutput(t *testing.T) {
	s := starlarkstruct.FromStringDict(starlark.String("struct"), starlark.StringDict{
		"command": starlark.String("cmd"),
	})
	list := starlark.NewList([]starlark.Value{s})
	checks, err := starlarkListToOutputChecks(list)
	if err != nil {
		t.Fatal(err)
	}
	if checks[0].ExpectedOutput != "" {
		t.Fatalf("expected empty, got %q", checks[0].ExpectedOutput)
	}
}

func TestStarlarkListToOutputChecks_StructCommandWrongType(t *testing.T) {
	s := starlarkstruct.FromStringDict(starlark.String("struct"), starlark.StringDict{
		"command": starlark.MakeInt(1),
	})
	list := starlark.NewList([]starlark.Value{s})
	_, err := starlarkListToOutputChecks(list)
	if err == nil || !strings.Contains(err.Error(), "'command' must be string") {
		t.Fatalf("expected command type error, got %v", err)
	}
}
