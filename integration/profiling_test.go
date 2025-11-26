package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

type ProfilingRepositoryDefinition struct {
	Name           string             `yaml:"name"`
	Files          []ProfilingFile    `yaml:"files"`
	Packages       []ProfilingPackage `yaml:"packages"`
	TargetsToBuild []string           `yaml:"targetsToBuild"`
}

type ProfilingFile struct {
	Path      string `yaml:"path"`
	SizeBytes int    `yaml:"sizeBytes"`
	Content   string `yaml:"content"`
}

type ProfilingPackage struct {
	Path    string            `yaml:"path"`
	Targets []ProfilingTarget `json:"targets" yaml:"targets"`
}

type ProfilingTarget struct {
	Name         string   `json:"name" yaml:"name"`
	Inputs       []string `json:"inputs,omitempty" yaml:"inputs,omitempty"`
	Dependencies []string `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
	Command      string   `json:"command" yaml:"command"`
	Outputs      []string `json:"outputs,omitempty" yaml:"outputs,omitempty"`
}

func TestProfilingBuildIntegration(t *testing.T) {
	definitions := loadProfilingDefinitions(t, filepath.Join("integration", "profiling", "profiling_definitions.yaml"))
	if len(definitions) == 0 {
		t.Fatalf("no profiling definitions found")
	}

	for _, definition := range definitions {
		definition := definition
		t.Run(definition.Name, func(t *testing.T) {
			repoPath := materializeProfilingRepo(t, definition)

			runProfilingCommand(t, repoPath, []string{"clean"})

			buildArgs := append([]string{"build"}, definition.TargetsToBuild...)

			_, uncachedDuration := runProfilingCommand(t, repoPath, append([]string{"--enable-cache=false"}, buildArgs...))
			_, cacheMissDuration := runProfilingCommand(t, repoPath, buildArgs)
			_, cacheHitDuration := runProfilingCommand(t, repoPath, buildArgs)

			t.Logf(
				"Profiling complete for %s (uncached/cache-miss/cache-hit): %s / %s / %s",
				definition.Name,
				uncachedDuration,
				cacheMissDuration,
				cacheHitDuration,
			)
		})
	}
}

func loadProfilingDefinitions(t *testing.T, path string) []ProfilingRepositoryDefinition {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read profiling definitions: %v", err)
	}

	var wrapper struct {
		Profiles []ProfilingRepositoryDefinition `yaml:"profiles"`
	}

	if err := yaml.Unmarshal(content, &wrapper); err != nil {
		t.Fatalf("failed to parse profiling definitions: %v", err)
	}

	return wrapper.Profiles
}

func materializeProfilingRepo(t *testing.T, definition ProfilingRepositoryDefinition) string {
	t.Helper()

	if len(definition.TargetsToBuild) == 0 {
		t.Fatalf("no targets provided to build in profiling definition")
	}

	repoPath := t.TempDir()

	if err := os.WriteFile(filepath.Join(repoPath, "grog.toml"), nil, 0644); err != nil {
		t.Fatalf("failed to create grog.toml: %v", err)
	}

	for _, file := range definition.Files {
		materializeProfilingFile(t, repoPath, file)
	}

	for _, pkg := range definition.Packages {
		pkgPath := filepath.Join(repoPath, pkg.Path)
		if err := os.MkdirAll(pkgPath, 0755); err != nil {
			t.Fatalf("failed to create package directory %s: %v", pkg.Path, err)
		}

		buildFilePath := filepath.Join(pkgPath, "BUILD.json")
		content, err := json.MarshalIndent(pkg, "", "  ")
		if err != nil {
			t.Fatalf("failed to marshal build file for %s: %v", pkg.Path, err)
		}

		if err := os.WriteFile(buildFilePath, content, 0644); err != nil {
			t.Fatalf("failed to write build file %s: %v", buildFilePath, err)
		}
	}

	return repoPath
}

func materializeProfilingFile(t *testing.T, repoPath string, file ProfilingFile) {
	t.Helper()

	filePath := filepath.Join(repoPath, file.Path)
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		t.Fatalf("failed to create directory for %s: %v", file.Path, err)
	}

	content := []byte(file.Content)
	if file.SizeBytes > 0 {
		content = padContent(content, file.SizeBytes)
	} else if len(content) == 0 {
		content = []byte("profiling file")
	}

	if err := os.WriteFile(filePath, content, 0644); err != nil {
		t.Fatalf("failed to write file %s: %v", file.Path, err)
	}
}

func padContent(content []byte, size int) []byte {
	if len(content) >= size {
		return content[:size]
	}

	padding := bytes.Repeat([]byte{'x'}, size-len(content))
	return append(content, padding...)
}

func runProfilingCommand(t *testing.T, repoPath string, args []string) ([]byte, time.Duration) {
	t.Helper()

	cmd := exec.Command(binaryPath, args...)

	coverDir, err := getCoverDir()
	if err != nil {
		t.Fatalf("could not get coverage directory: %v", err)
	}

	cmd.Env = append(os.Environ(), "GOCOVERDIR="+coverDir)
	cmd.Env = append(cmd.Env, "GROG_DISABLE_NON_DETERMINISTIC_LOGGING=true")
	cmd.Dir = repoPath

	start := time.Now()
	output, err := cmd.CombinedOutput()
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("command failed: grog %s: %v\nOutput:\n%s", strings.Join(args, " "), err, output)
	}

	t.Logf("grog %s took %s", strings.Join(args, " "), duration)

	return output, duration
}
