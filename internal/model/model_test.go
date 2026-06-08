package model

import (
	"encoding/json"
	"strings"
	"testing"

	"grog/internal/label"
)

func mustLabel(t *testing.T, s string) label.TargetLabel {
	t.Helper()
	pkg, name, _ := strings.Cut(strings.TrimPrefix(s, "//"), ":")
	return label.TL(pkg, name)
}

func TestOutput_StringIsSetIsFile(t *testing.T) {
	o := NewOutput("file", "path/to.txt")
	if o.String() != "file::path/to.txt" {
		t.Fatalf("got %q", o.String())
	}
	if !o.IsSet() {
		t.Fatal("expected set")
	}
	if !o.IsFile() {
		t.Fatal("expected file")
	}
	empty := NewOutput("file", "")
	if empty.IsSet() {
		t.Fatal("empty should not be set")
	}
	notFile := NewOutput("oci", "img")
	if notFile.IsFile() {
		t.Fatal("oci is not file")
	}
}

func TestTarget_TagsAndHelpers(t *testing.T) {
	tt := &Target{
		Label:   mustLabel(t, "//pkg:t"),
		Command: "echo hi\nsecond line",
		Tags:    []string{TagNoCache, TagMultiplatformCache, TagTestOnly},
	}
	if !tt.HasTag(TagNoCache) || !tt.SkipsCache() {
		t.Fatal("nocache")
	}
	if !tt.IsMultiplatformCache() {
		t.Fatal("mp")
	}
	if !tt.IsTestOnly() {
		t.Fatal("testonly")
	}
	ce := tt.CommandEllipsis()
	if !strings.HasSuffix(ce, "...") {
		t.Fatalf("expected ellipsis got %q", ce)
	}

	long := &Target{Command: strings.Repeat("a", 100)}
	if got := long.CommandEllipsis(); len(got) != 70 || !strings.HasSuffix(got, "...") {
		t.Fatalf("got %q (len %d)", got, len(got))
	}
	short := &Target{Command: "single"}
	if short.CommandEllipsis() != "single" {
		t.Fatalf("got %q", short.CommandEllipsis())
	}
}

func TestTarget_OutputsAndBin(t *testing.T) {
	tt := &Target{
		Outputs: []Output{NewOutput("file", "out.txt"), NewOutput("oci", "img")},
	}
	if tt.HasBinOutput() {
		t.Fatal("no bin")
	}
	if len(tt.AllOutputs()) != 2 {
		t.Fatal("all outputs")
	}
	tt.BinOutput = NewOutput("file", "bin")
	if !tt.HasBinOutput() {
		t.Fatal("has bin")
	}
	if len(tt.AllOutputs()) != 3 {
		t.Fatal("all outputs with bin")
	}
	files := tt.FileOutputs()
	if len(files) != 2 || files[0] != "out.txt" {
		t.Fatalf("got %v", files)
	}
	defs := tt.OutputDefinitions()
	if len(defs) != 3 {
		t.Fatalf("got %d defs", len(defs))
	}
}

func TestTarget_HasOutputChecksOnly(t *testing.T) {
	tt := &Target{OutputChecks: []OutputCheck{{Command: "true"}}}
	if !tt.HasOutputChecksOnly() {
		t.Fatal("expected true")
	}
	tt.Inputs = []string{"a"}
	if tt.HasOutputChecksOnly() {
		t.Fatal("inputs disqualifies")
	}
}

func TestTarget_NodeInterface(t *testing.T) {
	l := mustLabel(t, "//x:y")
	dep := mustLabel(t, "//x:dep")
	tt := &Target{Label: l, Dependencies: []label.TargetLabel{dep}}
	if tt.GetType() != TargetNode {
		t.Fatal("type")
	}
	if tt.GetLabel() != l {
		t.Fatal("label")
	}
	if len(tt.GetDependencies()) != 1 {
		t.Fatal("deps")
	}
	if tt.GetIsSelected() {
		t.Fatal("not selected")
	}
	tt.Select()
	if !tt.GetIsSelected() {
		t.Fatal("selected")
	}
}

func TestTarget_IsTest(t *testing.T) {
	tt := &Target{Label: mustLabel(t, "//x:y_test")}
	_ = tt.IsTest()
}

func TestTarget_MarshalJSON(t *testing.T) {
	tt := &Target{Label: mustLabel(t, "//pkg:t"), Command: "echo"}
	b, err := tt.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "bin_output") {
		t.Fatalf("bin_output should be omitted: %s", b)
	}
	tt.BinOutput = NewOutput("file", "bin")
	b, err = tt.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "bin_output") {
		t.Fatalf("expected bin_output: %s", b)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
}

func TestAlias_NodeInterface(t *testing.T) {
	a := &Alias{Label: mustLabel(t, "//x:a"), Actual: mustLabel(t, "//x:t")}
	if a.GetType() != AliasNode {
		t.Fatal("type")
	}
	if a.GetLabel() != a.Label {
		t.Fatal("label")
	}
	if len(a.GetDependencies()) != 1 || a.GetDependencies()[0] != a.Actual {
		t.Fatal("deps")
	}
	if a.GetIsSelected() {
		t.Fatal("not selected")
	}
	a.Select()
	if !a.GetIsSelected() {
		t.Fatal("selected")
	}
}

func TestIsTestTargetNode(t *testing.T) {
	a := &Alias{Label: mustLabel(t, "//x:a")}
	if IsTestTargetNode(a) {
		t.Fatal("alias not test")
	}
}

func TestBuildNodeMap_FromPackages(t *testing.T) {
	t1 := &Target{Label: mustLabel(t, "//pkg:a")}
	a1 := &Alias{Label: mustLabel(t, "//pkg:b"), Actual: mustLabel(t, "//pkg:a")}
	pkg := &Package{
		Path: "pkg",
		Targets: map[label.TargetLabel]*Target{
			t1.Label: t1,
		},
		Aliases: map[label.TargetLabel]*Alias{
			a1.Label: a1,
		},
	}
	m, err := BuildNodeMapFromPackages([]*Package{pkg})
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 2 {
		t.Fatalf("got %d", len(m))
	}
	if len(m.GetTargets()) != 1 {
		t.Fatal("targets")
	}
	if len(m.NodesAlphabetically()) != 2 {
		t.Fatal("alpha")
	}
	t1.Select()
	sel := m.SelectedNodesAlphabetically()
	if len(sel) != 1 {
		t.Fatalf("got %d", len(sel))
	}

	dup := &Target{Label: t1.Label}
	pkgDup := &Package{
		Path:    "dup",
		Targets: map[label.TargetLabel]*Target{dup.Label: dup},
	}
	if _, err := BuildNodeMapFromPackages([]*Package{pkg, pkgDup}); err == nil {
		t.Fatal("expected duplicate target err")
	}
	dupA := &Alias{Label: a1.Label, Actual: t1.Label}
	pkgDupA := &Package{
		Path:    "dupA",
		Aliases: map[label.TargetLabel]*Alias{dupA.Label: dupA},
	}
	if _, err := BuildNodeMapFromPackages([]*Package{pkg, pkgDupA}); err == nil {
		t.Fatal("expected duplicate alias err")
	}
}

func TestBuildNodeMap_FromNodes(t *testing.T) {
	t1 := &Target{Label: mustLabel(t, "//x:a")}
	a1 := &Alias{Label: mustLabel(t, "//x:b")}
	m := BuildNodeMapFromNodes(t1, a1)
	if len(m) != 2 {
		t.Fatal("from nodes")
	}
}

func TestPackage_Getters(t *testing.T) {
	t1 := &Target{Label: mustLabel(t, "//x:a")}
	a1 := &Alias{Label: mustLabel(t, "//x:b")}
	pkg := &Package{
		Targets: map[label.TargetLabel]*Target{t1.Label: t1},
		Aliases: map[label.TargetLabel]*Alias{a1.Label: a1},
	}
	if len(pkg.GetTargets()) != 1 {
		t.Fatal("targets")
	}
	if len(pkg.GetAliases()) != 1 {
		t.Fatal("aliases")
	}
}

func TestPrintSortedLabels(t *testing.T) {
	t1 := &Target{Label: mustLabel(t, "//z:a")}
	t2 := &Target{Label: mustLabel(t, "//a:b")}
	PrintSortedLabels([]BuildNode{t1, t2})
}
