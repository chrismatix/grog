package analysis

import (
	"strings"
	"testing"

	"grog/internal/label"
	"grog/internal/model"
)

func TestDetectOutputConflicts_FileFileSamePath(t *testing.T) {
	a := &model.Target{
		Label:   label.TL("pkg", "a"),
		Outputs: []model.Output{model.NewOutput("file", "out.txt")},
	}
	b := &model.Target{
		Label:   label.TL("pkg", "b"),
		Outputs: []model.Output{model.NewOutput("file", "out.txt")},
	}
	nodes := model.BuildNodeMapFromNodes(a, b)
	_, err := BuildGraph(nodes)
	if err == nil {
		t.Fatal("expected conflict err")
	}
	if !strings.Contains(err.Error(), "file output") {
		t.Fatalf("got %v", err)
	}
}

func TestDetectOutputConflicts_DirDirOverlap(t *testing.T) {
	a := &model.Target{
		Label:   label.TL("pkg", "a"),
		Outputs: []model.Output{model.NewOutput("dir", "out")},
	}
	b := &model.Target{
		Label:   label.TL("pkg", "b"),
		Outputs: []model.Output{model.NewOutput("dir", "out/sub")},
	}
	nodes := model.BuildNodeMapFromNodes(a, b)
	if _, err := BuildGraph(nodes); err == nil {
		t.Fatal("expected conflict err")
	}
}

func TestDetectOutputConflicts_DirFileOverlap(t *testing.T) {
	a := &model.Target{
		Label:   label.TL("pkg", "a"),
		Outputs: []model.Output{model.NewOutput("dir", "out")},
	}
	b := &model.Target{
		Label:   label.TL("pkg", "b"),
		Outputs: []model.Output{model.NewOutput("file", "out/file.txt")},
	}
	nodes := model.BuildNodeMapFromNodes(a, b)
	if _, err := BuildGraph(nodes); err == nil {
		t.Fatal("expected conflict err")
	}
}

func TestDetectOutputConflicts_DockerSameImage(t *testing.T) {
	a := &model.Target{
		Label:   label.TL("pkg", "a"),
		Outputs: []model.Output{model.NewOutput("oci", "img")},
	}
	b := &model.Target{
		Label:   label.TL("pkg", "b"),
		Outputs: []model.Output{model.NewOutput("oci", "img")},
	}
	nodes := model.BuildNodeMapFromNodes(a, b)
	if _, err := BuildGraph(nodes); err == nil {
		t.Fatal("expected conflict err")
	}
}

func TestDetectOutputConflicts_OrderedNoConflict(t *testing.T) {
	a := &model.Target{
		Label:   label.TL("pkg", "a"),
		Outputs: []model.Output{model.NewOutput("file", "out.txt")},
	}
	b := &model.Target{
		Label:        label.TL("pkg", "b"),
		Outputs:      []model.Output{model.NewOutput("file", "out.txt")},
		Dependencies: []label.TargetLabel{a.Label},
	}
	nodes := model.BuildNodeMapFromNodes(a, b)
	if _, err := BuildGraph(nodes); err != nil {
		t.Fatalf("ordered should not conflict: %v", err)
	}
}
