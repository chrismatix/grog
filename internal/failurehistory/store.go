package failurehistory

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"grog/internal/caching/backends"
	"grog/internal/config"
	"grog/internal/hashing"
	"grog/internal/model"
)

// InputChange describes one file difference from the last green execution.
type InputChange struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}

type inputManifest struct {
	Hashes map[string]string `json:"hashes"`
}

// Store persists per-target last-green input manifests.
type Store struct {
	backend backends.CacheBackend

	identityOnce sync.Once
	identity     string
}

// NewStore creates a last-green input history store.
func NewStore(backend backends.CacheBackend) *Store {
	return &Store{backend: backend}
}

// Save records the target inputs from a green execution.
func (store *Store) Save(commandContext context.Context, target *model.Target) error {
	manifest, captureError := captureInputManifest(target)
	if captureError != nil {
		return captureError
	}

	content, marshalError := json.Marshal(manifest)
	if marshalError != nil {
		return marshalError
	}
	return store.backend.Set(commandContext, "last-green", store.key(commandContext, target), bytes.NewReader(content))
}

// Changes compares current target inputs with the last green execution.
func (store *Store) Changes(commandContext context.Context, target *model.Target) ([]InputChange, error) {
	key := store.key(commandContext, target)
	exists, existsError := store.backend.Exists(commandContext, "last-green", key)
	if existsError != nil || !exists {
		return nil, existsError
	}

	reader, getError := store.backend.Get(commandContext, "last-green", key)
	if getError != nil {
		return nil, getError
	}
	defer reader.Close()

	var previous inputManifest
	if decodeError := json.NewDecoder(reader).Decode(&previous); decodeError != nil {
		return nil, decodeError
	}
	current, captureError := captureInputManifest(target)
	if captureError != nil {
		return nil, captureError
	}

	return compareInputManifests(previous, current), nil
}

func (store *Store) key(commandContext context.Context, target *model.Target) string {
	store.identityOnce.Do(func() {
		store.identity = repositoryIdentity(commandContext)
	})
	digest := sha256.Sum256([]byte(store.identity + "\x00" + target.Label.String()))
	return hex.EncodeToString(digest[:])
}

func repositoryIdentity(commandContext context.Context) string {
	remoteCommand := exec.CommandContext(commandContext, "git", "config", "--get", "remote.origin.url")
	remoteCommand.Dir = config.Global.WorkspaceRoot
	if output, outputError := remoteCommand.Output(); outputError == nil && strings.TrimSpace(string(output)) != "" {
		return strings.TrimSpace(string(output))
	}

	commonDirectoryCommand := exec.CommandContext(commandContext, "git", "rev-parse", "--path-format=absolute", "--git-common-dir")
	commonDirectoryCommand.Dir = config.Global.WorkspaceRoot
	if output, outputError := commonDirectoryCommand.Output(); outputError == nil && strings.TrimSpace(string(output)) != "" {
		return strings.TrimSpace(string(output))
	}
	return config.Global.WorkspaceRoot
}

func captureInputManifest(target *model.Target) (inputManifest, error) {
	hashes := make(map[string]string)
	packagePath := config.GetPathAbsoluteToWorkspaceRoot(target.Label.Package)
	for _, inputPath := range target.Inputs {
		hash, hashError := hashing.HashFile(filepath.Join(packagePath, inputPath))
		if hashError != nil {
			if errors.Is(hashError, os.ErrNotExist) {
				continue
			}
			return inputManifest{}, hashError
		}
		hashes[filepath.ToSlash(filepath.Join(target.Label.Package, inputPath))] = hash
	}

	if target.SourceFilePath != "" {
		hash, hashError := hashing.HashFile(target.SourceFilePath)
		if hashError != nil {
			return inputManifest{}, hashError
		}
		relativePath, relativePathError := filepath.Rel(config.Global.WorkspaceRoot, target.SourceFilePath)
		if relativePathError != nil {
			return inputManifest{}, relativePathError
		}
		hashes[filepath.ToSlash(relativePath)] = hash
	}
	return inputManifest{Hashes: hashes}, nil
}

func compareInputManifests(previous inputManifest, current inputManifest) []InputChange {
	paths := make(map[string]bool)
	for path := range previous.Hashes {
		paths[path] = true
	}
	for path := range current.Hashes {
		paths[path] = true
	}

	var changes []InputChange
	for path := range paths {
		previousHash, existedBefore := previous.Hashes[path]
		currentHash, existsNow := current.Hashes[path]
		switch {
		case !existedBefore:
			changes = append(changes, InputChange{Path: path, Kind: "added"})
		case !existsNow:
			changes = append(changes, InputChange{Path: path, Kind: "removed"})
		case previousHash != currentHash:
			changes = append(changes, InputChange{Path: path, Kind: "modified"})
		}
	}
	sort.Slice(changes, func(firstIndex int, secondIndex int) bool {
		return changes[firstIndex].Path < changes[secondIndex].Path
	})
	return changes
}
