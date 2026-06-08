package handlers_test

import (
	"context"
	"testing"

	"grog/internal/output/handlers"
)

func TestNopWritePlan(t *testing.T) {
	plan := handlers.NewNopWritePlan()
	if err := plan.Execute(context.Background(), nil); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if err := plan.Cleanup(context.Background()); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
}
