package analysis

import (
	"strings"
	"testing"

	"grog/internal/label"
	"grog/internal/model"
)

func TestCheckOciPushReferences_Valid(t *testing.T) {
	target := &model.Target{
		Label:   label.TL("pkg", "tgt"),
		Outputs: []model.Output{model.NewOutput("oci", "api"), model.NewOutput("oci", "worker")},
		OciPush: map[string][]string{
			"api":    {"repo/api:1"},
			"worker": {"repo/worker:1"},
		},
	}
	if errs := checkOciPushReferences(target); len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestCheckOciPushReferences_UnknownKey(t *testing.T) {
	target := &model.Target{
		Label:   label.TL("pkg", "tgt"),
		Outputs: []model.Output{model.NewOutput("oci", "api")},
		OciPush: map[string][]string{"missing": {"repo/x:1"}},
	}
	errs := checkOciPushReferences(target)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
	if !strings.Contains(errs[0].Error(), `"missing"`) {
		t.Errorf("error %q should name the offending key", errs[0].Error())
	}
}

func TestCheckOciPushReferences_Empty(t *testing.T) {
	target := &model.Target{
		Label:   label.TL("pkg", "tgt"),
		Outputs: []model.Output{model.NewOutput("oci", "api")},
	}
	if errs := checkOciPushReferences(target); len(errs) != 0 {
		t.Errorf("expected no errors when oci_push is unset, got %v", errs)
	}
}
