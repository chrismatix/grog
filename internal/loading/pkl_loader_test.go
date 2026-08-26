package loading

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"grog/internal/config"

	"github.com/apple/pkl-go/pkl"
)

type pklEvaluatorStub struct {
	pkl.Evaluator
	evaluateModule func(context.Context, *pkl.ModuleSource, any) error
}

func (e *pklEvaluatorStub) EvaluateModule(ctx context.Context, source *pkl.ModuleSource, output any) error {
	return e.evaluateModule(ctx, source, output)
}

func TestPklLoaderTimeoutReturnsError(t *testing.T) {
	previousConfig := config.Global
	config.Global.PklEvaluationTimeout = 10 * time.Millisecond
	t.Cleanup(func() {
		config.Global = previousConfig
	})

	loader := &PklLoader{
		evaluator: &pklEvaluatorStub{
			evaluateModule: func(ctx context.Context, _ *pkl.ModuleSource, _ any) error {
				<-ctx.Done()
				// This matches pkl-go v0.12.1, which returns nil on cancellation.
				return nil
			},
		},
	}
	loader.evaluatorOnce.Do(func() {})

	packageDTO, matched, err := loader.Load(context.Background(), "BUILD.pkl")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded error, got %v", err)
	}
	if matched {
		t.Fatal("expected a timed-out BUILD.pkl not to be reported as successfully loaded")
	}
	if len(packageDTO.Targets) != 0 {
		t.Fatalf("expected no targets from timed-out BUILD.pkl, got %d", len(packageDTO.Targets))
	}
}

func TestPklEvaluatorCreationContextIgnoresCallerCancellation(t *testing.T) {
	type contextKey string
	const key contextKey = "key"
	callerContext, cancelCaller := context.WithCancel(context.WithValue(context.Background(), key, "value"))
	cancelCaller()

	creationContext, cancelCreation := newPklEvaluatorCreationContext(callerContext)
	t.Cleanup(cancelCreation)

	if err := creationContext.Err(); err != nil {
		t.Fatalf("expected evaluator creation to ignore caller cancellation, got %v", err)
	}
	if _, hasDeadline := creationContext.Deadline(); !hasDeadline {
		t.Fatal("expected evaluator creation context to have its own deadline")
	}
	if value := creationContext.Value(key); value != "value" {
		t.Fatalf("expected evaluator creation context to retain caller values, got %v", value)
	}
}

func TestPklLoaderRejectsNilEvaluator(t *testing.T) {
	loader := &PklLoader{}
	loader.evaluatorOnce.Do(func() {})

	_, matched, err := loader.Load(context.Background(), "BUILD.pkl")
	if err == nil {
		t.Fatal("expected nil evaluator to return an error")
	}
	if !strings.Contains(err.Error(), "no evaluator and no error") {
		t.Fatalf("expected explicit nil evaluator error, got %v", err)
	}
	if matched {
		t.Fatal("expected BUILD.pkl not to be reported as loaded with a nil evaluator")
	}
}
