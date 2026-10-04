package loading

import (
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/mod/modfile"
	"golang.org/x/sync/errgroup"
)

// goDependencies maps every Go package under the workspace to the packages
// it imports from any module found there. Imports are parsed from the source
// files directly, so no toolchain is needed and every build constraint
// counts. Test files are left out: Go allows import cycles through them.
func goDependencies(resolverContext context.Context, workspaceDirectory string) (resolverDocument, error) {
	document := resolverDocument{Version: 1, Packages: make(map[string]resolverPackage)}
	modules, packageDirectories, operationError := goWorkspaceLayout(workspaceDirectory)
	if operationError != nil {
		return document, operationError
	}
	if len(modules) == 0 {
		return document, fmt.Errorf("no go.mod under %s", workspaceDirectory)
	}

	isPackage := make(map[string]bool, len(packageDirectories))
	for _, packageDirectory := range packageDirectories {
		isPackage[packageDirectory] = true
	}
	var documentLock sync.Mutex
	parseGroup, parseContext := errgroup.WithContext(resolverContext)
	parseGroup.SetLimit(runtime.GOMAXPROCS(0))
	for _, packageDirectory := range packageDirectories {
		parseGroup.Go(func() error {
			if operationError := parseContext.Err(); operationError != nil {
				return operationError
			}
			imports, embedPatterns, operationError := goPackageSources(filepath.Join(workspaceDirectory, packageDirectory))
			if operationError != nil {
				return operationError
			}
			reportedPackage := resolverPackage{Dependencies: []string{}, Inputs: goInputs(embedPatterns), ExcludeInputs: goExcludeInputs}
			for _, importPath := range imports {
				dependencyDirectory, isLocal := goImportDirectory(importPath, modules)
				if isLocal && dependencyDirectory != packageDirectory && isPackage[dependencyDirectory] {
					reportedPackage.Dependencies = append(reportedPackage.Dependencies, dependencyDirectory)
				}
			}
			slices.Sort(reportedPackage.Dependencies)
			reportedPackage.Dependencies = slices.Compact(reportedPackage.Dependencies)
			documentLock.Lock()
			document.Packages[packageDirectory] = reportedPackage
			documentLock.Unlock()
			return nil
		})
	}
	return document, parseGroup.Wait()
}

type goModule struct {
	Path      string
	Directory string
}

// goWorkspaceLayout walks the workspace the way the go tool does, skipping
// vendor, testdata and hidden or underscore directories, and returns every
// module and every directory holding Go files, both workspace-relative.
func goWorkspaceLayout(workspaceDirectory string) ([]goModule, []string, error) {
	var modules []goModule
	packageDirectories := make(map[string]bool)
	operationError := filepath.WalkDir(workspaceDirectory, func(currentPath string, entry fs.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		relativePath, operationError := filepath.Rel(workspaceDirectory, currentPath)
		if operationError != nil {
			return operationError
		}
		relativePath = filepath.ToSlash(relativePath)
		if relativePath == "." {
			relativePath = ""
		}
		if entry.IsDir() {
			name := entry.Name()
			if relativePath != "" && (name == "vendor" || name == "testdata" || name == "node_modules" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return fs.SkipDir
			}
			return nil
		}
		directory := path.Dir(relativePath)
		if directory == "." {
			directory = ""
		}
		switch {
		case entry.Name() == "go.mod":
			contents, operationError := os.ReadFile(currentPath)
			if operationError != nil {
				return fmt.Errorf("read %s: %w", currentPath, operationError)
			}
			modulePath := modfile.ModulePath(contents)
			if modulePath == "" {
				return fmt.Errorf("parse %s: no module directive", currentPath)
			}
			modules = append(modules, goModule{Path: modulePath, Directory: directory})
		case isGoSourceFile(entry.Name()):
			packageDirectories[directory] = true
		}
		return nil
	})
	if operationError != nil {
		return nil, nil, operationError
	}
	// Longest module path first, so a nested module wins over its parent.
	slices.SortFunc(modules, func(first, second goModule) int { return len(second.Path) - len(first.Path) })
	return modules, slices.Sorted(maps.Keys(packageDirectories)), nil
}

// goImportDirectory maps an import path to a workspace directory through the
// module whose path prefixes it, or reports false for an external import.
func goImportDirectory(importPath string, modules []goModule) (string, bool) {
	for _, module := range modules {
		if importPath == module.Path {
			return module.Directory, true
		}
		if suffix, isPrefixed := strings.CutPrefix(importPath, module.Path+"/"); isPrefixed {
			return path.Join(module.Directory, suffix), true
		}
	}
	return "", false
}

// goPackageSources returns the imports of the non-test Go files in a directory
// and the patterns of every //go:embed directive, test files included.
func goPackageSources(packageDirectory string) (imports []string, embedPatterns []string, operationError error) {
	entries, operationError := os.ReadDir(packageDirectory)
	if operationError != nil {
		return nil, nil, operationError
	}
	fileSet := token.NewFileSet()
	for _, entry := range entries {
		if entry.IsDir() || !isGoSourceFile(entry.Name()) {
			continue
		}
		filePath := filepath.Join(packageDirectory, entry.Name())
		file, operationError := parser.ParseFile(fileSet, filePath, nil, parser.ParseComments)
		if operationError != nil {
			return nil, nil, fmt.Errorf("parse %s: %w", filePath, operationError)
		}
		for _, commentGroup := range file.Comments {
			for _, comment := range commentGroup.List {
				embedPatterns = append(embedPatterns, goEmbedPatterns(comment.Text)...)
			}
		}
		if strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		for _, importSpec := range file.Imports {
			importPath, operationError := strconv.Unquote(importSpec.Path.Value)
			if operationError != nil {
				return nil, nil, fmt.Errorf("parse %s: invalid import %s", filePath, importSpec.Path.Value)
			}
			imports = append(imports, importPath)
		}
	}
	return imports, embedPatterns, nil
}

// isGoSourceFile applies the go tool's rule of ignoring files whose names
// start with a dot or an underscore.
func isGoSourceFile(name string) bool {
	return strings.HasSuffix(name, ".go") && !strings.HasPrefix(name, ".") && !strings.HasPrefix(name, "_")
}

// goEmbedPatterns reads the bare, quoted or backquoted patterns of one
// //go:embed comment. An all: prefix changes traversal, not the path.
func goEmbedPatterns(comment string) []string {
	line, isEmbed := strings.CutPrefix(comment, "//go:embed")
	if !isEmbed || (line != "" && line[0] != ' ' && line[0] != '\t') {
		return nil
	}
	var patterns []string
	for line = strings.TrimSpace(line); line != ""; line = strings.TrimSpace(line) {
		var pattern string
		switch line[0] {
		case '"':
			quoted, operationError := strconv.QuotedPrefix(line)
			if operationError != nil {
				return patterns
			}
			pattern, _ = strconv.Unquote(quoted)
			line = line[len(quoted):]
		case '`':
			end := strings.IndexByte(line[1:], '`')
			if end < 0 {
				return patterns
			}
			pattern = line[1 : end+1]
			line = line[end+2:]
		default:
			end := strings.IndexAny(line, " \t")
			if end < 0 {
				end = len(line)
			}
			pattern = line[:end]
			line = line[end:]
		}
		patterns = append(patterns, strings.TrimPrefix(pattern, "all:"))
	}
	return patterns
}

// goInputs covers the package directory itself, its testdata, and every
// embedded file. An embed pattern may name a directory, which embeds its
// whole tree, so each pattern is listed with and without a trailing /**.
func goInputs(embedPatterns []string) []string {
	inputs := []string{"*", "testdata/**"}
	for _, pattern := range embedPatterns {
		inputs = append(inputs, pattern, path.Join(pattern, "**"))
	}
	slices.Sort(inputs)
	return slices.Compact(inputs)
}

// goExcludeInputs keeps vendored modules out of an embed pattern's tree.
var goExcludeInputs = []string{"vendor/**"}

// goDefaultInputs are the files the built-in reads: every module file and
// every Go source file outside vendor and testdata directories.
func goDefaultInputs(_ string) (inputs []string, excludeInputs []string) {
	return []string{"go.work", "go.work.sum", "**/go.mod", "**/go.sum", "**/*.go"}, []string{"**/vendor/**", "**/testdata/**", "**/node_modules/**", "**/.*/**", "**/_*/**"}
}
