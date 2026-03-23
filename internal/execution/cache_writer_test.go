package execution

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"

	"grog/internal/caching"
	"grog/internal/console"
	"grog/internal/output"
	"grog/internal/output/handlers"
	"grog/internal/proto/gen"
	"grog/internal/worker"
)

type recordingCacheBackend struct {
	mu       sync.Mutex
	setCalls []string
}

func (r *recordingCacheBackend) TypeName() string { return "recording" }

func (r *recordingCacheBackend) Get(context.Context, string, string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}

func (r *recordingCacheBackend) Set(_ context.Context, path, _ string, content io.Reader) error {
	_, _ = io.Copy(io.Discard, content)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.setCalls = append(r.setCalls, path)
	return nil
}

func (r *recordingCacheBackend) Delete(context.Context, string, string) error { return nil }

func (r *recordingCacheBackend) Exists(context.Context, string, string) (bool, error) {
	return false, nil
}

func (r *recordingCacheBackend) Calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	calls := make([]string, len(r.setCalls))
	copy(calls, r.setCalls)
	return calls
}

type mockWritePlan struct {
	mu         sync.Mutex
	calls      []string
	uploadErr  error
	cleanupErr error
}

func (m *mockWritePlan) Execute(context.Context, *worker.ProgressTracker) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, "upload")
	return m.uploadErr
}

func (m *mockWritePlan) Cleanup(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, "cleanup")
	return m.cleanupErr
}

func (m *mockWritePlan) Calls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	calls := make([]string, len(m.calls))
	copy(calls, m.calls)
	return calls
}

func newTestCacheWriter(t *testing.T, targetCache *caching.TargetResultCache, asyncWrites bool, ioContext context.Context) (*CacheWriter, *worker.TaskWorkerPool[struct{}]) {
	t.Helper()

	testLogger := console.NewFromSugared(zaptest.NewLogger(t).Sugar(), zapcore.DebugLevel)
	ioPool := worker.NewTaskWorkerPool[struct{}](testLogger, 1, func(_ tea.Msg) {}, 0)
	ioPool.StartWorkers(ioContext)

	return NewCacheWriter(targetCache, ioPool, asyncWrites, ioContext), ioPool
}

func TestCacheWriterSkipsTargetPublicationWhenAsyncUploadFails(t *testing.T) {
	backend := &recordingCacheBackend{}
	targetCache := caching.NewTargetResultCache(backend)
	cacheWriter, ioPool := newTestCacheWriter(t, targetCache, true, context.Background())
	defer ioPool.Shutdown()

	writePlan := &mockWritePlan{uploadErr: io.EOF}
	preparedTarget := &output.PreparedTargetResult{
		TargetResult: &gen.TargetResult{ChangeHash: "change-hash"},
		WritePlans:   []handlers.OutputWritePlan{writePlan},
	}

	if err := cacheWriter.PersistPreparedTarget(context.Background(), "//pkg:target", preparedTarget, func(worker.StatusUpdate) {}); err != nil {
		t.Fatalf("PersistPreparedTarget returned error: %v", err)
	}

	cacheWriter.Wait()

	if calls := backend.Calls(); len(calls) != 0 {
		t.Fatalf("expected no target-cache publication after failed upload, got %v", calls)
	}

	if calls := writePlan.Calls(); len(calls) != 2 || calls[0] != "upload" || calls[1] != "cleanup" {
		t.Fatalf("expected upload followed by cleanup, got %v", calls)
	}
}

func TestCacheWriterAsyncUsesIndependentIOContext(t *testing.T) {
	backend := &recordingCacheBackend{}
	targetCache := caching.NewTargetResultCache(backend)
	cacheWriter, ioPool := newTestCacheWriter(t, targetCache, true, context.Background())
	defer ioPool.Shutdown()

	writePlan := &mockWritePlan{}
	preparedTarget := &output.PreparedTargetResult{
		TargetResult: &gen.TargetResult{ChangeHash: "change-hash"},
		WritePlans:   []handlers.OutputWritePlan{writePlan},
	}

	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()

	if err := cacheWriter.PersistPreparedTarget(canceledContext, "//pkg:target", preparedTarget, func(worker.StatusUpdate) {}); err != nil {
		t.Fatalf("PersistPreparedTarget returned error: %v", err)
	}

	cacheWriter.Wait()

	if calls := backend.Calls(); len(calls) != 1 || calls[0] != "target" {
		t.Fatalf("expected one target-cache publication, got %v", calls)
	}

	if calls := writePlan.Calls(); len(calls) != 2 || calls[0] != "upload" || calls[1] != "cleanup" {
		t.Fatalf("expected upload followed by cleanup, got %v", calls)
	}
}
