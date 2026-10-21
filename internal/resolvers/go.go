package resolvers

import (
	"context"
	"crypto/sha256"
	"errors"
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
func goDependencies(resolverContext context.Context, workspaceDirectory string) (Document, error) {
	document := Document{Version: 1, Packages: make(map[string]Package)}
	modules, pending, operationError := goWorkspaceLayout(resolverContext, workspaceDirectory)
	if operationError != nil {
		return document, operationError
	}
	if len(modules) == 0 {
		return document, fmt.Errorf("no go.mod under %s", workspaceDirectory)
	}

	// A directory the walk skips, such as _foo, is still a package once
	// something imports it, so parsing repeats for newly imported directories.
	localImports := make(map[string][]string)
	for len(pending) > 0 {
		var resultLock sync.Mutex
		parseGroup, parseContext := errgroup.WithContext(resolverContext)
		parseGroup.SetLimit(runtime.GOMAXPROCS(0))
		for _, packageDirectory := range pending {
			parseGroup.Go(func() error {
				if operationError := parseContext.Err(); operationError != nil {
					return operationError
				}
				imports, embedPatterns, operationError := goPackageSources(filepath.Join(workspaceDirectory, packageDirectory))
				if operationError != nil {
					return operationError
				}
				var dependencyDirectories []string
				for _, importPath := range imports {
					for _, dependencyDirectory := range goImportDirectories(importPath, modules) {
						if dependencyDirectory != packageDirectory {
							dependencyDirectories = append(dependencyDirectories, dependencyDirectory)
						}
					}
				}
				resultLock.Lock()
				defer resultLock.Unlock()
				localImports[packageDirectory] = dependencyDirectories
				reportedPackage := Package{Inputs: goInputs(filepath.Join(workspaceDirectory, packageDirectory), embedPatterns)}
				if fingerprint := goModuleFingerprint(packageDirectory, modules); fingerprint != "" {
					reportedPackage.Fingerprint = map[string]string{"module": fingerprint}
				}
				document.Packages[packageDirectory] = reportedPackage
				return nil
			})
		}
		if operationError := parseGroup.Wait(); operationError != nil {
			return document, operationError
		}
		pending = nil
		for _, dependencyDirectories := range localImports {
			for _, dependencyDirectory := range dependencyDirectories {
				if _, isParsed := document.Packages[dependencyDirectory]; !isParsed && !slices.Contains(pending, dependencyDirectory) && goHasSources(filepath.Join(workspaceDirectory, dependencyDirectory)) {
					pending = append(pending, dependencyDirectory)
				}
			}
		}
	}

	for packageDirectory, dependencyDirectories := range localImports {
		reportedPackage := document.Packages[packageDirectory]
		reportedPackage.Dependencies = []string{}
		for _, dependencyDirectory := range dependencyDirectories {
			if _, isPackage := document.Packages[dependencyDirectory]; isPackage {
				reportedPackage.Dependencies = append(reportedPackage.Dependencies, dependencyDirectory)
			}
		}
		slices.Sort(reportedPackage.Dependencies)
		reportedPackage.Dependencies = slices.Compact(reportedPackage.Dependencies)
		document.Packages[packageDirectory] = reportedPackage
	}
	return document, nil
}

// goHasSources reports whether a directory holds a Go file the go tool reads.
func goHasSources(directory string) bool {
	entries, operationError := os.ReadDir(directory)
	if operationError != nil {
		return false
	}
	return slices.ContainsFunc(entries, func(entry fs.DirEntry) bool { return !entry.IsDir() && isGoSourceFile(entry.Name()) })
}

type goModule struct {
	Path        string
	Directory   string
	Fingerprint string
}

// goWorkspaceLayout walks the workspace the way the go tool does, skipping
// vendor, testdata and hidden or underscore directories, and returns every
// module and every directory holding Go files, both workspace-relative.
func goWorkspaceLayout(walkContext context.Context, workspaceDirectory string) ([]goModule, []string, error) {
	modules, replacements, operationError := goReadWorkspaceModules(walkContext, workspaceDirectory)
	if operationError != nil {
		return nil, nil, operationError
	}
	packageDirectories := make(map[string]bool)
	operationError = filepath.WalkDir(workspaceDirectory, func(currentPath string, entry fs.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		if operationError := walkContext.Err(); operationError != nil {
			return operationError
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
			module, localReplacements, operationError := goReadModuleFile(workspaceDirectory, currentPath, directory)
			if operationError != nil {
				return operationError
			}
			modules = append(modules, module)
			replacements = append(replacements, localReplacements...)
		case isGoSourceFile(entry.Name()):
			packageDirectories[directory] = true
		}
		return nil
	})
	if operationError != nil {
		return nil, nil, operationError
	}
	modules = append(modules, replacements...)
	if operationError := goFingerprintModules(workspaceDirectory, modules); operationError != nil {
		return nil, nil, operationError
	}
	// Longest module path first, so a nested module wins over its parent.
	slices.SortFunc(modules, func(first, second goModule) int {
		if difference := len(second.Path) - len(first.Path); difference != 0 {
			return difference
		}
		return strings.Compare(first.Directory, second.Directory)
	})
	return modules, slices.Sorted(maps.Keys(packageDirectories)), nil
}

func goFingerprintModules(workspaceDirectory string, modules []goModule) error {
	for index := range modules {
		fingerprint, operationError := goModuleMetadataFingerprint(workspaceDirectory, modules[index].Directory)
		if operationError != nil {
			return operationError
		}
		modules[index].Fingerprint = fingerprint
	}
	return nil
}

func goReadWorkspaceModules(resolverContext context.Context, workspaceDirectory string) ([]goModule, []goModule, error) {
	if operationError := resolverContext.Err(); operationError != nil {
		return nil, nil, operationError
	}
	workspaceFilePath := filepath.Join(workspaceDirectory, "go.work")
	contents, operationError := os.ReadFile(workspaceFilePath)
	if errors.Is(operationError, os.ErrNotExist) {
		return nil, nil, nil
	}
	if operationError != nil {
		return nil, nil, fmt.Errorf("read %s: %w", workspaceFilePath, operationError)
	}
	workspaceFile, operationError := modfile.ParseWork(workspaceFilePath, contents, nil)
	if operationError != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", workspaceFilePath, operationError)
	}
	var modules []goModule
	var replacements []goModule
	for _, use := range workspaceFile.Use {
		if operationError := resolverContext.Err(); operationError != nil {
			return nil, nil, operationError
		}
		modulePath := filepath.FromSlash(use.Path)
		if !filepath.IsAbs(modulePath) {
			modulePath = filepath.Join(workspaceDirectory, modulePath)
		}
		moduleDirectory, operationError := filepath.Rel(workspaceDirectory, modulePath)
		if operationError != nil {
			return nil, nil, operationError
		}
		moduleDirectory = filepath.ToSlash(moduleDirectory)
		if moduleDirectory == "." {
			moduleDirectory = ""
		}
		if moduleDirectory == ".." || strings.HasPrefix(moduleDirectory, "../") {
			continue
		}
		module, localReplacements, operationError := goReadModuleFile(workspaceDirectory, filepath.Join(workspaceDirectory, filepath.FromSlash(moduleDirectory), "go.mod"), moduleDirectory)
		if operationError != nil {
			return nil, nil, operationError
		}
		modules = append(modules, module)
		replacements = append(replacements, localReplacements...)
	}
	workspaceReplacements, operationError := goLocalReplacements(workspaceDirectory, "", workspaceFile.Replace)
	return modules, append(replacements, workspaceReplacements...), operationError
}

func goReadModuleFile(workspaceDirectory, moduleFilePath, moduleDirectory string) (goModule, []goModule, error) {
	contents, operationError := os.ReadFile(moduleFilePath)
	if operationError != nil {
		return goModule{}, nil, fmt.Errorf("read %s: %w", moduleFilePath, operationError)
	}
	moduleFile, operationError := modfile.Parse(moduleFilePath, contents, nil)
	if operationError != nil {
		return goModule{}, nil, fmt.Errorf("parse %s: %w", moduleFilePath, operationError)
	}
	if moduleFile.Module == nil || moduleFile.Module.Mod.Path == "" {
		return goModule{}, nil, fmt.Errorf("parse %s: no module directive", moduleFilePath)
	}
	replacements, operationError := goLocalReplacements(workspaceDirectory, moduleDirectory, moduleFile.Replace)
	if operationError != nil {
		return goModule{}, nil, operationError
	}
	return goModule{Path: moduleFile.Module.Mod.Path, Directory: moduleDirectory}, replacements, nil
}

func goLocalReplacements(workspaceDirectory, baseDirectory string, replacements []*modfile.Replace) ([]goModule, error) {
	var localReplacements []goModule
	for _, replacement := range replacements {
		if replacement.New.Version != "" {
			continue
		}
		replacementPath := filepath.FromSlash(replacement.New.Path)
		if !filepath.IsAbs(replacementPath) {
			replacementPath = filepath.Join(workspaceDirectory, filepath.FromSlash(baseDirectory), replacementPath)
		}
		replacementDirectory, operationError := filepath.Rel(workspaceDirectory, replacementPath)
		if operationError != nil {
			return nil, operationError
		}
		replacementDirectory = filepath.ToSlash(replacementDirectory)
		if replacementDirectory == "." {
			replacementDirectory = ""
		}
		if replacementDirectory != ".." && !strings.HasPrefix(replacementDirectory, "../") {
			localReplacements = append(localReplacements, goModule{Path: replacement.Old.Path, Directory: replacementDirectory})
		}
	}
	return localReplacements, nil
}

func goModuleMetadataFingerprint(workspaceDirectory, moduleDirectory string) (string, error) {
	hasher := sha256.New()
	for _, metadataPath := range []string{"go.work", "go.work.sum", path.Join(moduleDirectory, "go.mod"), path.Join(moduleDirectory, "go.sum")} {
		contents, operationError := os.ReadFile(filepath.Join(workspaceDirectory, filepath.FromSlash(metadataPath)))
		if errors.Is(operationError, os.ErrNotExist) {
			continue
		}
		if operationError != nil {
			return "", fmt.Errorf("read %s: %w", metadataPath, operationError)
		}
		_, _ = fmt.Fprintf(hasher, "%s\x00%d\x00", metadataPath, len(contents))
		_, _ = hasher.Write(contents)
	}
	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}

func goModuleFingerprint(packageDirectory string, modules []goModule) string {
	fingerprint := ""
	matchedLength := -1
	for _, module := range modules {
		isWithin := module.Directory == "" || packageDirectory == module.Directory || strings.HasPrefix(packageDirectory, module.Directory+"/")
		if module.Fingerprint != "" && isWithin && len(module.Directory) > matchedLength {
			fingerprint = module.Fingerprint
			matchedLength = len(module.Directory)
		}
	}
	return fingerprint
}

// goImportDirectories maps an import path through the longest matching module
// path. Multiple results conservatively cover version-specific replacements.
func goImportDirectories(importPath string, modules []goModule) []string {
	matchedLength := -1
	var directories []string
	for _, module := range modules {
		var directory string
		matches := false
		if importPath == module.Path {
			directory, matches = module.Directory, true
		}
		if suffix, isPrefixed := strings.CutPrefix(importPath, module.Path+"/"); isPrefixed {
			directory, matches = path.Join(module.Directory, suffix), true
		}
		if !matches || len(module.Path) < matchedLength {
			continue
		}
		if len(module.Path) > matchedLength {
			directories = nil
			matchedLength = len(module.Path)
		}
		directories = append(directories, directory)
	}
	slices.Sort(directories)
	return slices.Compact(directories)
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

func goInputs(_ string, embedPatterns []string) []string {
	inputs := []string{"*", "testdata/**"}
	for _, pattern := range embedPatterns {
		globPattern := strings.NewReplacer("{", `\{`, "}", `\}`).Replace(pattern)
		if strings.ContainsAny(pattern, "*?[") {
			inputs = append(inputs, globPattern, path.Join(globPattern, "**"))
			continue
		}
		inputs = append(inputs, globPattern+"{,/**}")
	}
	slices.Sort(inputs)
	return slices.Compact(inputs)
}

// goDefaultInputs are the files the built-in reads: every module file and
// every Go source file an import path can reach.
func goDefaultInputs(resolverContext context.Context, workspaceDirectory string) (inputs []string, excludeInputs []string) {
	inputs = []string{"go.work", "go.work.sum", "**/go.mod", "**/go.sum", "**/*.go"}
	excludeInputs = []string{"**/vendor/**", "**/node_modules/**"}
	modules, _, operationError := goWorkspaceLayout(resolverContext, workspaceDirectory)
	if operationError != nil {
		return inputs, excludeInputs
	}
	for _, module := range modules {
		segments := strings.Split(module.Directory, "/")
		if slices.Contains(segments, "vendor") {
			excludeInputs = slices.DeleteFunc(excludeInputs, func(pattern string) bool { return pattern == "**/vendor/**" })
		}
		if slices.Contains(segments, "node_modules") {
			excludeInputs = slices.DeleteFunc(excludeInputs, func(pattern string) bool { return pattern == "**/node_modules/**" })
		}
	}
	return inputs, excludeInputs
}
