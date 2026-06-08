package cmds

import (
	"testing"

	"grog/internal/dag"
	"grog/internal/label"
	"grog/internal/model"
)

func TestResolveRunTarget_Target(t *testing.T) {
	logger := newTestLogger()
	tgt := &model.Target{
		Label:     label.TL("pkg", "t"),
		BinOutput: model.NewOutput("file", "out"),
	}
	g := dag.NewDirectedGraphFromTargets(tgt)
	if got := resolveRunTarget(logger, g, tgt.Label); got != tgt {
		t.Fatal("expected target back")
	}
}

func TestResolveRunTarget_Alias(t *testing.T) {
	logger := newTestLogger()
	tgt := &model.Target{
		Label:     label.TL("pkg", "t"),
		BinOutput: model.NewOutput("file", "out"),
	}
	alias := &model.Alias{
		Label:  label.TL("pkg", "a"),
		Actual: tgt.Label,
	}
	g := dag.NewDirectedGraphFromTargets(tgt, alias)
	if got := resolveRunTarget(logger, g, alias.Label); got != tgt {
		t.Fatal("alias resolution")
	}
}
