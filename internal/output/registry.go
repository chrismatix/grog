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
func (r *Registry) GetHandler(outputType string) (handlers.Handler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	handler, exists := r.handlers[outputType]
	return handler, exists
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

	for _, outputRef := range target.Outputs {
		handler := r.handlers[outputRef.Type]
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

	for _, outputRef := range target.Outputs {
		if handlerErr := r.handlers[outputRef.Type].Write(ctx, target, outputRef); handlerErr != nil {
			return handlerErr
		}
	}

	return nil
}

func (r *Registry) LoadOutputs(ctx context.Context, target model.Target) error {
	for _, outputRef := range target.Outputs {
		if err := r.handlers[outputRef.Type].Load(ctx, target, outputRef); err != nil {
			return err
		}
	}
	return nil
}

//func (tc *TargetCache) HasCacheExistsFile(ctx context.Context, target model.Target) bool {
//	// check all specified outputs exist in the backend
//	for _, output := range target.Outputs {
//		if !tc.backend.Exists(ctx, tc.cachePath(target), hashing.HashString(output)) {
//			return false
//		}
//	}
//
//	// check that the existsFileKey is present in the backend
//	return tc.backend.Exists(ctx, tc.cachePath(target), existsFileKey)
//}
//
//// LoadOutputs loads all outputs from the backend and fails if they do not exist
//// (existence should be checked before calling this function)
//func (tc *TargetCache) LoadOutputs(ctx context.Context, target model.Target) error {
//	for _, output := range target.Outputs {
//		// read output from file
//		absOutputPath := config.GetPathAbsoluteToWorkspaceRoot(filepath.Join(target.Label.Package, output))
//		contentReader, err := tc.backend.Get(ctx, tc.cachePath(target), hashing.HashString(output))
//		if err != nil {
//			return fmt.Errorf("output %s for target %s does not exist in %s backend",
//				output,
//				target.Label,
//				tc.backend.TypeName())
//		}
//
//		// TODO should we store the file permissions as-well somehow?
//		outputFile, err := os.Create(absOutputPath)
//		if err != nil {
//			return err
//		}
//
//		if _, err := io.Copy(outputFile, contentReader); err != nil {
//			return err
//		}
//
//		err = outputFile.Close()
//		if err != nil {
//			return err
//		}
//	}
//
//	return nil
//}
//
//// WriteOutputs Writes a target's outputs to the backend.
//func (tc *TargetCache) WriteOutputs(ctx context.Context, target model.Target) error {
//	for _, output := range target.Outputs {
//		// read output from file
//		absOutputPath := config.GetPathAbsoluteToWorkspaceRoot(filepath.Join(target.Label.Package, output))
//		outputReader, err := os.Open(absOutputPath)
//		if err != nil {
//			return fmt.Errorf("declared output %s for target %s was not created", output, target.Label)
//		}
//		if err = tc.backend.Set(ctx, tc.cachePath(target), hashing.HashString(output), outputReader); err != nil {
//			return err
//		}
//	}
//
//	// write existsFileKey to backend
//	return tc.backend.Set(ctx, tc.cachePath(target), existsFileKey, bytes.NewReader([]byte{}))
//}
