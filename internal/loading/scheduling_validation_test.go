package loading

import (
	"strings"
	"testing"

	"grog/internal/config"
	"grog/internal/label"
	"grog/internal/model"
)

func pkgWithTarget(name, group string, weight int) *model.Package {
	return &model.Package{
		Path: "pkg",
		Targets: map[label.TargetLabel]*model.Target{
			{Package: "pkg", Name: name}: {
				Label:            label.TargetLabel{Package: "pkg", Name: name},
				ConcurrencyGroup: group,
				Weight:           weight,
			},
		},
	}
}

func TestValidateScheduling_WeightExceedsNumWorkers(t *testing.T) {
	prev := config.Global.NumWorkers
	config.Global.NumWorkers = 4
	t.Cleanup(func() { config.Global.NumWorkers = prev })

	err := validateScheduling([]*model.Package{pkgWithTarget("heavy", "", 8)})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "num_workers=4") {
		t.Fatalf("error should mention num_workers=4: %v", err)
	}
}

func TestValidateScheduling_WeightExceedsGroupCapacity(t *testing.T) {
	prevWorkers := config.Global.NumWorkers
	prevGroups := config.Global.ConcurrencyGroups
	config.Global.NumWorkers = 8
	config.Global.ConcurrencyGroups = map[string]int{"docker": 1}
	t.Cleanup(func() {
		config.Global.NumWorkers = prevWorkers
		config.Global.ConcurrencyGroups = prevGroups
	})

	err := validateScheduling([]*model.Package{pkgWithTarget("img", "docker", 2)})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "group capacity=1") {
		t.Fatalf("error should mention group capacity=1: %v", err)
	}
}

func TestValidateScheduling_WeightFitsDefaultGroupCapacity(t *testing.T) {
	prevWorkers := config.Global.NumWorkers
	prevGroups := config.Global.ConcurrencyGroups
	config.Global.NumWorkers = 8
	config.Global.ConcurrencyGroups = nil
	t.Cleanup(func() {
		config.Global.NumWorkers = prevWorkers
		config.Global.ConcurrencyGroups = prevGroups
	})

	// weight=1 in an unconfigured group is fine (default capacity=1).
	if err := validateScheduling([]*model.Package{pkgWithTarget("t", "unconfigured", 1)}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// weight=2 in an unconfigured group must fail (exceeds default=1).
	err := validateScheduling([]*model.Package{pkgWithTarget("t", "unconfigured", 2)})
	if err == nil {
		t.Fatal("expected error for weight > default group capacity")
	}
}
