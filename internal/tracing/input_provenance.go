package tracing

import (
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

	"grog/internal/config"
	"grog/internal/hashing"
	"grog/internal/model"
)

// InputChange describes one input difference from a successful trace.
type InputChange struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}

// InputBaseline identifies the successful trace used for comparison.
type InputBaseline struct {
	TraceID   string `json:"trace_id"`
	GitCommit string `json:"git_commit"`
}

// InputChangeSet contains a trace baseline and its current input differences.
type InputChangeSet struct {
	Baseline InputBaseline `json:"baseline"`
	Changes  []InputChange `json:"changes"`
}

type inputManifest struct {
	Hashes map[string]string `json:"hashes"`
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

func encodeInputManifest(target *model.Target) string {
	manifest, captureError := captureInputManifest(target)
	if captureError != nil {
		return ""
	}
	content, marshalError := json.Marshal(manifest)
	if marshalError != nil {
		return ""
	}
	return string(content)
}

func decodeInputManifest(content string) (inputManifest, error) {
	var manifest inputManifest
	if decodeError := json.Unmarshal([]byte(content), &manifest); decodeError != nil {
		return inputManifest{}, decodeError
	}
	return manifest, nil
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

func repositoryIdentity(commandContext context.Context) string {
	remoteCommand := exec.CommandContext(commandContext, "git", "config", "--get", "remote.origin.url")
	remoteCommand.Dir = config.Global.WorkspaceRoot
	if output, outputError := remoteCommand.Output(); outputError == nil && strings.TrimSpace(string(output)) != "" {
		return hashIdentity(strings.TrimSpace(string(output)))
	}

	commonDirectoryCommand := exec.CommandContext(commandContext, "git", "rev-parse", "--path-format=absolute", "--git-common-dir")
	commonDirectoryCommand.Dir = config.Global.WorkspaceRoot
	if output, outputError := commonDirectoryCommand.Output(); outputError == nil && strings.TrimSpace(string(output)) != "" {
		return hashIdentity(strings.TrimSpace(string(output)))
	}
	return hashIdentity(config.Global.WorkspaceRoot)
}

func hashIdentity(identity string) string {
	digest := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(digest[:])
}

func workspaceIdentity() string {
	return config.GetWorkspaceCachePrefix(config.Global.WorkspaceRoot)
}
