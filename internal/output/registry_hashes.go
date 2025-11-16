package output

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/daemon"

	"grog/internal/config"
	"grog/internal/hashing"
	"grog/internal/model"
	"grog/internal/output/handlers"
)

const outputHashMetaKey = "__grog_output_hash"

var targetHashMetaOutput = model.NewOutput("__grog__", "__target_output__")

// LoadTargetOutputHash returns the aggregated hash for all outputs of the target.
// It first checks the in-memory cache, then the cache metadata, and finally
// falls back to hashing the local outputs if necessary.
func (r *Registry) LoadTargetOutputHash(ctx context.Context, target *model.Target) (string, error) {
	if !r.enableCache || target.SkipsCache() {
		return "", nil
	}

	if target.OutputHash != "" {
		return target.OutputHash, nil
	}

	if hash, ok := r.getCachedTargetHash(target.Label.String()); ok {
		target.OutputHash = hash
		return hash, nil
	}

	if hash, err := r.loadTargetHashFromMeta(ctx, target); err == nil && hash != "" {
		r.cacheTargetHash(target, hash)
		return hash, nil
	}

	if !target.OutputsLoaded {
		if err := r.LoadOutputs(ctx, target); err != nil {
			return "", fmt.Errorf("load outputs for %s: %w", target.Label, err)
		}
	}

	if target.OutputHash != "" {
		return target.OutputHash, nil
	}

	return r.computeAndStoreTargetOutputHash(ctx, target)
}

func (r *Registry) computeAndStoreTargetOutputHash(ctx context.Context, target *model.Target) (string, error) {
	if !r.enableCache || target.SkipsCache() {
		return "", nil
	}

	hash, err := r.hashTargetOutputs(ctx, target)
	if err != nil {
		return "", err
	}

	if hash == "" {
		target.OutputHash = ""
		r.cacheEmptyTargetHash(target)
		return "", nil
	}

	r.cacheTargetHash(target, hash)
	if err := r.targetCache.WriteOutputMetaFile(ctx, *target, targetHashMetaOutput, outputHashMetaKey, hash); err != nil {
		return "", fmt.Errorf("write output hash meta for %s: %w", target.Label, err)
	}
	return hash, nil
}

func (r *Registry) hashTargetOutputs(ctx context.Context, target *model.Target) (string, error) {
	outputs := target.AllOutputs()
	if len(outputs) == 0 {
		return "", nil
	}

	hashed := make([]string, 0, len(outputs))
	for _, out := range outputs {
		outputHash, err := r.hashSingleOutput(ctx, target, out)
		if err != nil {
			return "", err
		}
		hashed = append(hashed, fmt.Sprintf("%s=%s", out.String(), outputHash))
	}
	sort.Strings(hashed)
	return hashing.HashString(strings.Join(hashed, "|")), nil
}

func (r *Registry) hashSingleOutput(ctx context.Context, target *model.Target, output model.Output) (string, error) {
	switch handlers.HandlerType(output.Type) {
	case handlers.FileHandler:
		absPath := config.GetPathAbsoluteToWorkspaceRoot(filepath.Join(target.Label.Package, output.Identifier))
		return hashing.HashFile(absPath)
	case handlers.DirHandler:
		absPath := config.GetPathAbsoluteToWorkspaceRoot(filepath.Join(target.Label.Package, output.Identifier))
		return hashing.HashDirectory(absPath)
	case handlers.DockerHandler:
		return hashDockerImage(ctx, output.Identifier)
	default:
		return "", fmt.Errorf("unsupported output type %s", output.Type)
	}
}

func hashDockerImage(ctx context.Context, imageRef string) (string, error) {
	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return "", fmt.Errorf("parse docker reference %s: %w", imageRef, err)
	}
	img, err := daemon.Image(ref, daemon.WithContext(ctx))
	if err != nil {
		return "", fmt.Errorf("inspect docker image %s: %w", imageRef, err)
	}
	digest, err := img.Digest()
	if err != nil {
		return "", fmt.Errorf("compute digest for docker image %s: %w", imageRef, err)
	}
	return digest.String(), nil
}

func (r *Registry) loadTargetHashFromMeta(ctx context.Context, target *model.Target) (string, error) {
	content, err := r.targetCache.LoadOutputMetaFile(ctx, *target, targetHashMetaOutput, outputHashMetaKey)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(content), nil
}

func (r *Registry) getCachedTargetHash(label string) (string, bool) {
	r.hashMutex.RLock()
	defer r.hashMutex.RUnlock()
	hash, ok := r.hashCache[label]
	return hash, ok
}

func (r *Registry) cacheTargetHash(target *model.Target, hash string) {
	r.hashMutex.Lock()
	r.hashCache[target.Label.String()] = hash
	r.hashMutex.Unlock()
	target.OutputHash = hash
}

func (r *Registry) cacheEmptyTargetHash(target *model.Target) {
	r.hashMutex.Lock()
	delete(r.hashCache, target.Label.String())
	r.hashMutex.Unlock()
	target.OutputHash = ""
}
