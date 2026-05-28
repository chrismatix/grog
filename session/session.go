// Package session provides a headless, embeddable API over grog's build engine.
//
// It is the public boundary that external programs (notably the Terraform
// provider) use to drive grog without the CLI: no cobra, no Bubble Tea TUI, no
// os.Exit — every operation returns an error. A Session loads the workspace
// graph once and serves repeated Build calls against it.
//
// Concurrency model: grog configuration lives in process-global state
// (config.Global) and the build graph carries mutable per-node selection and
// hashes, so a Session serializes builds behind a mutex and memoizes results
// per target label. Intra-build parallelism (grog's worker pool) is unaffected;
// the serialization only bounds cross-call concurrency, which is what an
// embedder with many concurrent callers (e.g. Terraform resource operations)
// needs. One Session manages exactly one workspace per process.
package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"grog/internal/analysis"
	"grog/internal/caching"
	"grog/internal/caching/backends"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/dag"
	"grog/internal/execution"
	"grog/internal/label"
	"grog/internal/loading"
	"grog/internal/locking"
	"grog/internal/model"
	"grog/internal/output"
	"grog/internal/selection"
)

// Options configures a new Session.
type Options struct {
	// WorkspaceRoot is the directory containing grog.toml. Required.
	WorkspaceRoot string
	// Profile optionally selects a grog.<profile>.toml overlay.
	Profile string
	// LogWriter receives grog's build log lines. Embedders should pass a writer
	// that forwards somewhere other than stdout (e.g. an adapter onto Terraform's
	// log sink), since stdout may be owned by a plugin protocol. If nil, logs go
	// to stdout.
	LogWriter io.Writer
	// SkipWorkspaceLock disables the cross-process workspace lock. Leave false
	// unless you can guarantee no other grog process touches this workspace.
	SkipWorkspaceLock bool
}

// Session is a headless handle to a loaded grog workspace.
type Session struct {
	workspaceRoot string
	logger        *console.Logger

	cacheBackend backends.CacheBackend
	targetCache  *caching.TargetResultCache
	cas          *caching.Cas
	taintStore   *caching.TaintStore
	registry     *output.Registry

	locker *locking.WorkspaceLocker

	// mu serializes builds against the shared, mutable graph + global config.
	mu    sync.Mutex
	graph *dag.DirectedTargetGraph
	// memo caches build results per label for the session lifetime so repeated
	// requests for the same target (e.g. a shared dependency referenced by
	// several resources) do not rebuild.
	memo map[label.TargetLabel]*BuildResult

	closed bool
}

// New initializes configuration from the workspace's grog.toml, loads the build
// graph, wires the cache/CAS/output registry, and acquires the workspace lock.
// Call Close when done to release the lock and tear down the loopback docker
// registry.
func New(ctx context.Context, opts Options) (*Session, error) {
	if opts.WorkspaceRoot == "" {
		return nil, fmt.Errorf("session: WorkspaceRoot is required")
	}

	if err := config.InitForEmbedding(config.EmbeddingOptions{
		WorkspaceRoot: opts.WorkspaceRoot,
		Profile:       opts.Profile,
	}); err != nil {
		return nil, fmt.Errorf("session: loading config: %w", err)
	}
	if opts.SkipWorkspaceLock {
		config.Global.SkipWorkspaceLock = true
	}

	logger := newLogger(opts.LogWriter)
	// Ensure downstream code that reads the logger from context finds ours.
	ctx = console.WithLogger(ctx, logger)

	graph, err := loadGraph(ctx)
	if err != nil {
		return nil, fmt.Errorf("session: loading graph: %w", err)
	}

	cacheBackend, err := backends.GetCacheBackend(ctx, config.Global.Cache)
	if err != nil {
		return nil, fmt.Errorf("session: instantiating cache: %w", err)
	}

	cas := caching.NewCas(cacheBackend)

	s := &Session{
		workspaceRoot: config.Global.WorkspaceRoot,
		logger:        logger,
		cacheBackend:  cacheBackend,
		targetCache:   caching.NewTargetResultCache(cacheBackend),
		cas:           cas,
		taintStore:    caching.NewTaintStore(),
		registry:      output.NewRegistry(ctx, cas),
		graph:         graph,
		memo:          make(map[label.TargetLabel]*BuildResult),
	}

	if !config.Global.SkipWorkspaceLock {
		locker := locking.NewWorkspaceLocker()
		if err := locker.Lock(ctx); err != nil {
			_ = s.registry.Close()
			return nil, fmt.Errorf("session: acquiring workspace lock: %w", err)
		}
		s.locker = locker
	}

	return s, nil
}

// loadGraph is the error-returning analogue of loading.MustLoadGraphForBuild.
func loadGraph(ctx context.Context) (*dag.DirectedTargetGraph, error) {
	packages, err := loading.LoadAllPackages(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not load packages: %w", err)
	}
	nodes, err := model.BuildNodeMapFromPackages(packages)
	if err != nil {
		return nil, fmt.Errorf("could not create target map: %w", err)
	}
	graph, err := analysis.BuildGraph(nodes)
	if err != nil {
		return nil, fmt.Errorf("could not build graph: %w", err)
	}
	return graph, nil
}

// Build builds a single target (and its dependency closure) and returns its
// structured result. targetStr is a grog target label resolved relative to the
// workspace root, e.g. "//services/api:image".
//
// Builds are serialized and memoized: repeated calls for the same label return
// the cached result without rebuilding. Errors from the build (including a
// target's command failing) are returned, never fatal to the process.
func (s *Session) Build(ctx context.Context, targetStr string) (*BuildResult, error) {
	targetLabel, err := label.ParseTargetLabel(".", targetStr)
	if err != nil {
		return nil, fmt.Errorf("session: invalid target %q: %w", targetStr, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil, fmt.Errorf("session: closed")
	}
	if result, ok := s.memo[targetLabel]; ok {
		return result, nil
	}

	ctx = console.WithLogger(ctx, s.logger)

	if _, ok := s.graph.GetNodes()[targetLabel]; !ok {
		return nil, fmt.Errorf("session: target %q not found in workspace", targetLabel)
	}

	// Reset selection from any prior build before selecting this target.
	for _, node := range s.graph.GetNodes() {
		node.Deselect()
	}

	pattern, err := label.ParseTargetPattern(".", targetStr)
	if err != nil {
		return nil, fmt.Errorf("session: invalid target pattern %q: %w", targetStr, err)
	}
	selector := selection.New(
		[]label.TargetPattern{pattern},
		config.Global.Tags,
		config.Global.ExcludeTags,
		selection.NonTestOnly,
	)
	selectedCount, _, err := selector.SelectTargetsForBuild(s.graph)
	if err != nil {
		return nil, fmt.Errorf("session: target selection failed: %w", err)
	}
	if selectedCount == 0 {
		return nil, fmt.Errorf("session: no targets matched %q", targetStr)
	}

	// disable_tea is set in InitForEmbedding, so console.UseTea() inside
	// Executor.Execute returns false and the TUI is skipped — no separate
	// headless toggle needed.
	executor := execution.NewExecutor(
		s.targetCache,
		s.taintStore,
		s.registry,
		s.graph,
		config.Global.FailFast,
		config.Global.StreamLogs,
		config.Global.EnableCache,
		config.Global.GetLoadOutputsMode(),
	)

	completionMap, execErr := executor.Execute(ctx)
	// Always wait for async cache writes so results are durably persisted before
	// we return; the loopback docker registry stays open until Close.
	executor.WaitForAsyncWrites(ctx)

	if execErr != nil {
		return nil, fmt.Errorf("session: build of %q failed: %w", targetStr, execErr)
	}
	if buildErrs := completionMap.GetErrors(); len(buildErrs) > 0 {
		return nil, fmt.Errorf("session: build of %q failed: %w", targetStr, errors.Join(buildErrs...))
	}

	// Memoize results for every target built in this invocation so shared
	// dependencies referenced by later calls are free.
	var requested *BuildResult
	for builtLabel, completion := range completionMap {
		if completion.NodeType != model.TargetNode {
			continue
		}
		tr := executor.ResultFor(builtLabel)
		result := newBuildResult(builtLabel, completion.CacheResult == dag.CacheHit, tr, s.workspaceRoot)
		s.memo[builtLabel] = result
		if builtLabel == targetLabel {
			requested = result
		}
	}

	// If the caller asked to build an alias label, the alias node itself has no
	// TargetResult — only the underlying target does. Follow the alias chain to
	// find the real result and memoize the alias to it too.
	if requested == nil {
		if r := s.resolveAliasResult(targetLabel); r != nil {
			s.memo[targetLabel] = r
			requested = r
		}
	}

	if requested == nil {
		return nil, fmt.Errorf("session: build of %q produced no result", targetStr)
	}
	return requested, nil
}

// resolveAliasResult walks the alias chain starting at lbl and returns the
// memoized BuildResult of the underlying target, or nil if lbl is not an alias
// (or points nowhere). The chain length is bounded to avoid pathological loops
// even though grog's graph builder rejects cycles.
func (s *Session) resolveAliasResult(lbl label.TargetLabel) *BuildResult {
	const maxAliasDepth = 32
	cur := lbl
	for i := 0; i < maxAliasDepth; i++ {
		node, ok := s.graph.GetNodes()[cur]
		if !ok {
			return nil
		}
		alias, isAlias := node.(*model.Alias)
		if !isAlias {
			return s.memo[cur]
		}
		cur = alias.Actual
		if r, ok := s.memo[cur]; ok {
			return r
		}
	}
	return nil
}

// Close releases the workspace lock and tears down output handlers (notably the
// loopback docker registry). Safe to call multiple times.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true

	var firstErr error
	if s.registry != nil {
		if err := s.registry.Close(); err != nil {
			firstErr = fmt.Errorf("session: closing output registry: %w", err)
		}
	}
	if s.locker != nil {
		if err := s.locker.Unlock(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("session: releasing workspace lock: %w", err)
		}
	}
	return firstErr
}
