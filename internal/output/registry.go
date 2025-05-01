package output

import (
	"context"
	"fmt"
	"grog/internal/caching"
	"grog/internal/model"
	"grog/internal/output/handlers"
	"sync"
)

// Registry manages the available output handlers
type Registry struct {
	handlers    map[string]handlers.Handler
	targetCache *caching.TargetCache
	mu          sync.RWMutex
}

// NewRegistry creates a new registry with default handlers
func NewRegistry(
	targetCache *caching.TargetCache,
) *Registry {
	r := &Registry{
		handlers:    make(map[string]handlers.Handler),
		targetCache: targetCache,
	}

	// Register built-in handlers
	r.Register(handlers.NewFileOutputHandler(targetCache))
	r.Register(handlers.NewDirectoryOutputHandler(targetCache))
	r.Register(handlers.NewDockerOutputHandler(targetCache))

	return r
}

// Register adds a new output handler to the registry
func (r *Registry) Register(handler handlers.Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.handlers[string(handler.Type())]; ok {
		panic(fmt.Sprintf("handler for type %s already registered", handler.Type()))
	}
	r.handlers[string(handler.Type())] = handler
}

// GetHandler retrieves a handler by type
func (r *Registry) mustGetHandler(outputType string) handlers.Handler {
	r.mu.RLock()
	defer r.mu.RUnlock()
	handler, exists := r.handlers[outputType]
	if !exists {
		panic(fmt.Sprintf("handler for type %s not registered", outputType))
	}
	return handler
}

func (r *Registry) HasCacheHit(ctx context.Context, target model.Target) (bool, error) {
	// check for the default file system key (for empty outputs)
	cacheHit, err := r.targetCache.HasCacheExistsFile(ctx, target)
	if err != nil {
		return false, err
	}
	// short-circuit
	if !cacheHit {
		return false, nil
	}

	for _, outputRef := range target.AllOutputs() {
		handler := r.mustGetHandler(outputRef.Type)
		handlerCacheHit, handlerErr := handler.Has(ctx, target, outputRef)
		if handlerErr != nil {
			return false, fmt.Errorf("error checking for output %s with %s handler: %w", outputRef, handler.Type(), handlerErr)
		}
		if !handlerCacheHit {
			return false, nil
		}
	}

	return true, nil
}

func (r *Registry) WriteOutputs(ctx context.Context, target model.Target) error {
	err := r.targetCache.WriteCacheExistsFile(ctx, target)
	if err != nil {
		return err
	}

	for _, outputRef := range target.AllOutputs() {
		if handlerErr := r.mustGetHandler(outputRef.Type).Write(ctx, target, outputRef); handlerErr != nil {
			return handlerErr
		}
	}

	return nil
}

func (r *Registry) LoadOutputs(ctx context.Context, target model.Target) error {
	for _, outputRef := range target.AllOutputs() {
		if err := r.mustGetHandler(outputRef.Type).Load(ctx, target, outputRef); err != nil {
			return err
		}
	}

	// Why this is needed:
	// - When restoring from the remote cache we copy every file to the local cache
	// - However, we don't explicitly restore the cache exists file
	// TODO here we should only write the local cache exists file but it feels like the wrong place to do this
	// since the output registry should not have to know about the file cache
	return r.targetCache.WriteLocalCacheExistsFile(ctx, target)
}
