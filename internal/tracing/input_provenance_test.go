package tracing

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"grog/internal/caching/backends"
	"grog/internal/config"
	"grog/internal/label"
	"grog/internal/model"
)

func TestCompareInputManifests(t *testing.T) {
	previous := inputManifest{Hashes: map[string]string{
		"first.txt":   "old",
		"removed.txt": "removed",
	}}
	current := inputManifest{Hashes: map[string]string{
		"added.txt": "added",
		"first.txt": "new",
	}}

	expected := []InputChange{
		{Path: "added.txt", Kind: "added"},
		{Path: "first.txt", Kind: "modified"},
		{Path: "removed.txt", Kind: "removed"},
	}
	if changes := compareInputManifests(previous, current); !slices.Equal(changes, expected) {
		t.Errorf("expected %v, got %v", expected, changes)
	}
}

func TestTraceStoreInputChangesPreferSameWorkspace(t *testing.T) {
	commandContext := context.Background()
	workspaceRoot := t.TempDir()
	traceRoot := t.TempDir()
	previousConfig := config.Global
	config.Global = config.WorkspaceConfig{
		Root:          t.TempDir(),
		WorkspaceRoot: workspaceRoot,
	}
	t.Cleanup(func() {
		config.Global = previousConfig
	})

	writeInputTestFile(t, filepath.Join(workspaceRoot, "BUILD.json"), "{}")
	writeInputTestFile(t, filepath.Join(workspaceRoot, "input.txt"), "before")
	target := &model.Target{
		Label:          label.TargetLabel{Name: "test"},
		SourceFilePath: filepath.Join(workspaceRoot, "BUILD.json"),
		Inputs:         []string{"input.txt"},
	}
	previousManifest := encodeInputManifest(target)

	writeInputTestFile(t, filepath.Join(workspaceRoot, "input.txt"), "after")
	currentManifest := encodeInputManifest(target)

	backend := backends.NewFileSystemCacheForTest(traceRoot, t.TempDir())
	writer := NewTraceWriter(backend)
	now := time.Now()
	traces := []*BuildTrace{
		inputProvenanceTrace(
			"same-workspace",
			now.Add(-time.Minute).UnixMilli(),
			repositoryIdentity(commandContext),
			workspaceIdentity(),
			previousManifest,
		),
		inputProvenanceTrace(
			"other-workspace",
			now.UnixMilli(),
			repositoryIdentity(commandContext),
			"other-workspace",
			currentManifest,
		),
	}
	for _, trace := range traces {
		if writeError := writer.Write(commandContext, trace); writeError != nil {
			t.Fatalf("could not write trace: %v", writeError)
		}
	}

	store, storeError := NewTraceStore(backend, &PathResolver{
		buildsBase: filepath.Join(traceRoot, tracesBuildsPath),
		spansBase:  filepath.Join(traceRoot, tracesSpansPath),
	})
	if storeError != nil {
		t.Fatalf("could not create trace store: %v", storeError)
	}
	defer store.Close()

	changeSet, changeError := store.InputChangesSinceLastSuccess(commandContext, target)
	if changeError != nil {
		t.Fatalf("could not compare trace inputs: %v", changeError)
	}
	if changeSet == nil {
		t.Fatal("expected input change set")
	}
	if changeSet.Baseline.TraceID != "same-workspace" {
		t.Errorf("expected same-workspace baseline, got %s", changeSet.Baseline.TraceID)
	}
	expected := []InputChange{{Path: "input.txt", Kind: "modified"}}
	if !slices.Equal(changeSet.Changes, expected) {
		t.Errorf("expected %v, got %v", expected, changeSet.Changes)
	}
}

func TestTraceStoreInputChangesWithoutTraceFiles(t *testing.T) {
	commandContext := context.Background()
	workspaceRoot := t.TempDir()
	previousConfig := config.Global
	config.Global = config.WorkspaceConfig{
		Root:          t.TempDir(),
		WorkspaceRoot: workspaceRoot,
	}
	t.Cleanup(func() {
		config.Global = previousConfig
	})

	writeInputTestFile(t, filepath.Join(workspaceRoot, "input.txt"), "content")
	target := &model.Target{
		Label:  label.TargetLabel{Name: "test"},
		Inputs: []string{"input.txt"},
	}
	traceRoot := t.TempDir()
	backend := backends.NewFileSystemCacheForTest(traceRoot, t.TempDir())
	store, storeError := NewTraceStore(backend, &PathResolver{
		buildsBase: filepath.Join(traceRoot, tracesBuildsPath),
		spansBase:  filepath.Join(traceRoot, tracesSpansPath),
	})
	if storeError != nil {
		t.Fatalf("could not create trace store: %v", storeError)
	}
	defer store.Close()

	var waitGroup sync.WaitGroup
	errorsByWorker := make(chan error, 8)
	for workerIndex := 0; workerIndex < 8; workerIndex++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			changeSet, changeError := store.InputChangesSinceLastSuccess(commandContext, target)
			if changeError != nil {
				errorsByWorker <- changeError
			} else if changeSet != nil {
				errorsByWorker <- errors.New("expected no change set")
			}
		}()
	}
	waitGroup.Wait()
	close(errorsByWorker)
	for workerError := range errorsByWorker {
		t.Error(workerError)
	}
}

func inputProvenanceTrace(
	traceID string,
	startTimeUnixMillis int64,
	repositoryID string,
	workspaceID string,
	manifest string,
) *BuildTrace {
	return &BuildTrace{
		Build: BuildRow{
			TraceID:             traceID,
			GitCommit:           "commit-" + traceID,
			StartTimeUnixMillis: startTimeUnixMillis,
			RepositoryID:        repositoryID,
			WorkspaceID:         workspaceID,
		},
		Spans: []SpanRow{{
			TraceID:       traceID,
			Label:         "//:test",
			Status:        "SUCCESS",
			InputManifest: manifest,
		}},
	}
}

func writeInputTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if writeError := os.WriteFile(path, []byte(content), 0o644); writeError != nil {
		t.Fatalf("could not write %s: %v", path, writeError)
	}
}
