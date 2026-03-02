package output

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/alitto/pond/v2"

	"grog/internal/label"
	"grog/internal/maps"
	"grog/internal/model"
	"grog/internal/output/handlers"
	"grog/internal/proto/gen"
	"grog/internal/worker"
)

type mockRegistryHandler struct {
	handlerType             handlers.HandlerType
	loadCallCount           int
	loadedOutputDefinitions []string
	mutex                   sync.Mutex
}

func (handler *mockRegistryHandler) Type() handlers.HandlerType {
	return handler.handlerType
}

func (handler *mockRegistryHandler) Write(
	ctx context.Context,
	target model.Target,
	output model.Output,
	tracker *worker.ProgressTracker,
) (*gen.Output, error) {
	return nil, nil
}

func (handler *mockRegistryHandler) Hash(
	ctx context.Context,
	target model.Target,
	output model.Output,
) (string, error) {
	return "", nil
}

func (handler *mockRegistryHandler) Load(
	ctx context.Context,
	target model.Target,
	output *gen.Output,
	tracker *worker.ProgressTracker,
) error {
	handler.mutex.Lock()
	defer handler.mutex.Unlock()
	handler.loadCallCount++
	outputDefinition, err := getOutputDefinitionFromProto(output)
	if err != nil {
		return err
	}
	handler.loadedOutputDefinitions = append(handler.loadedOutputDefinitions, outputDefinition)
	return nil
}

func newTestRegistryWithHandlers(registryHandlers ...handlers.Handler) *Registry {
	handlersByType := make(map[string]handlers.Handler)
	for _, handler := range registryHandlers {
		handlersByType[string(handler.Type())] = handler
	}
	return &Registry{
		handlers:       handlersByType,
		targetMutexMap: maps.NewMutexMap(),
		pool:           pond.NewPool(4),
	}
}

func TestLoadOutputsRejectsMismatchedOutputCount(t *testing.T) {
	registry := newTestRegistryWithHandlers()
	target := &model.Target{
		Label:   label.TL("pkg", "target"),
		Outputs: []model.Output{model.NewOutput(string(handlers.FileHandler), "dist/output.txt")},
	}

	targetResult := &gen.TargetResult{
		Outputs: []*gen.Output{},
	}

	err := registry.LoadOutputs(context.Background(), target, targetResult, nil)
	if err == nil {
		t.Fatalf("expected mismatch error but got nil")
	}

	if !strings.Contains(err.Error(), "cached outputs mismatch") {
		t.Fatalf("expected mismatch error but got %v", err)
	}
}

func TestLoadOutputsRejectsMismatchedOutputDefinitions(t *testing.T) {
	registry := newTestRegistryWithHandlers()
	target := &model.Target{
		Label:   label.TL("pkg", "target"),
		Outputs: []model.Output{model.NewOutput(string(handlers.FileHandler), "dist/output.txt")},
	}

	targetResult := &gen.TargetResult{
		Outputs: []*gen.Output{
			{
				Kind: &gen.Output_File{
					File: &gen.FileOutput{Path: "dist/other.txt"},
				},
			},
		},
	}

	err := registry.LoadOutputs(context.Background(), target, targetResult, nil)
	if err == nil {
		t.Fatalf("expected mismatch error but got nil")
	}

	if !strings.Contains(err.Error(), "cached outputs mismatch") {
		t.Fatalf("expected mismatch error but got %v", err)
	}
}

func TestLoadOutputsLoadsWhenDefinitionsMatch(t *testing.T) {
	fileHandler := &mockRegistryHandler{handlerType: handlers.FileHandler}
	registry := newTestRegistryWithHandlers(fileHandler)
	target := &model.Target{
		Label:   label.TL("pkg", "target"),
		Outputs: []model.Output{model.NewOutput(string(handlers.FileHandler), "dist/output.txt")},
	}

	targetResult := &gen.TargetResult{
		OutputHash: "output-hash",
		Outputs: []*gen.Output{
			{
				Kind: &gen.Output_File{
					File: &gen.FileOutput{Path: "dist/output.txt"},
				},
			},
		},
	}

	err := registry.LoadOutputs(context.Background(), target, targetResult, nil)
	if err != nil {
		t.Fatalf("expected no error but got %v", err)
	}

	if !target.OutputsLoaded {
		t.Fatalf("expected target outputs to be marked as loaded")
	}

	if target.OutputHash != "output-hash" {
		t.Fatalf("expected output hash %q but got %q", "output-hash", target.OutputHash)
	}

	if fileHandler.loadCallCount != 1 {
		t.Fatalf("expected exactly one load call but got %d", fileHandler.loadCallCount)
	}
}

func TestLoadOutputsRejectsNilOutputEntries(t *testing.T) {
	registry := newTestRegistryWithHandlers()
	target := &model.Target{
		Label:   label.TL("pkg", "target"),
		Outputs: []model.Output{model.NewOutput(string(handlers.FileHandler), "dist/output.txt")},
	}

	targetResult := &gen.TargetResult{
		Outputs: []*gen.Output{nil},
	}

	err := registry.LoadOutputs(context.Background(), target, targetResult, nil)
	if err == nil {
		t.Fatalf("expected error for nil output entry but got nil")
	}

	if !strings.Contains(err.Error(), "invalid output entry in cached target result") {
		t.Fatalf("expected invalid output entry error but got %v", err)
	}
}
