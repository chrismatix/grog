package resolvers

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/xml"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

type dotnetProjectFile struct {
	Imports           []dotnetImport
	ProjectReferences []string
}

type dotnetImport struct {
	Project string `xml:"Project,attr"`
}

type dotnetSolutionFolder struct {
	Projects []struct {
		Path string `xml:"Path,attr"`
	} `xml:"Project"`
	Folders []dotnetSolutionFolder `xml:"Folder"`
}

var dotnetSolutionProjectLine = regexp.MustCompile(`^Project\("\{[^"]*\}"\)\s*=\s*"[^"]*",\s*"([^"]+)"`)

var dotnetProjectExtensions = []string{".csproj", ".fsproj", ".vbproj"}

// dotnetConfigurationFiles are imported by every project below their
// directory, which a member's own inputs cannot reach.
var dotnetConfigurationFiles = []string{"directory.build.props", "directory.build.targets", "directory.packages.props", "global.json", "nuget.config"}

var dotnetConfigurationFileNames = []string{"Directory.Build.props", "Directory.Build.targets", "Directory.Packages.props", "global.json", "nuget.config", "NuGet.Config"}

// dotnetExcludeInputs keeps every glob out of build outputs.
var dotnetExcludeInputs = []string{"**/bin/**", "**/obj/**"}

// dotnetDependencies maps every project to the projects its ProjectReference
// items name, following references to projects the solution does not list.
// Every project also depends on each ancestor directory holding configuration
// files MSBuild imports from above the project, the root included.
func dotnetDependencies(resolverContext context.Context, workspaceDirectory string) (Document, error) {
	document := Document{Version: 1, Packages: make(map[string]Package)}
	projectFiles, operationError := dotnetProjectFiles(resolverContext, workspaceDirectory)
	if operationError != nil {
		return document, operationError
	}
	configurationDirectories, operationError := dotnetConfigurationDirectories(resolverContext, workspaceDirectory)
	if operationError != nil {
		return document, operationError
	}
	for directory, files := range configurationDirectories {
		document.Packages[directory] = Package{Dependencies: []string{}, Inputs: files, ExcludeInputs: dotnetExcludeInputs}
	}
	visited := make(map[string]bool)
	projectDirectories := make(map[string]bool)
	configurationDependenciesByProject := make(map[string][]string)
	for queue := projectFiles; len(queue) > 0; queue = queue[1:] {
		projectFile := queue[0]
		if visited[projectFile] {
			continue
		}
		visited[projectFile] = true
		if operationError := resolverContext.Err(); operationError != nil {
			return document, operationError
		}
		directory := dotnetPackagePath(path.Dir(projectFile))
		reportedPackage, exists := document.Packages[directory]
		if _, isConfigurationDirectory := configurationDirectories[directory]; !exists || isConfigurationDirectory {
			reportedPackage = Package{Dependencies: reportedPackage.Dependencies, Inputs: []string{"**/*"}, ExcludeInputs: dotnetExcludeInputs, Fingerprint: reportedPackage.Fingerprint}
		}
		projectDirectories[directory] = true
		configurationDependencies, configurationFiles := dotnetConfigurationReferences(directory, configurationDirectories)
		configurationDependenciesByProject[directory] = configurationDependencies
		reportedPackage.Dependencies = append(reportedPackage.Dependencies, configurationDependencies...)
		importedFiles, references, _, operationError := dotnetImportedFiles(resolverContext, workspaceDirectory, append([]string{projectFile}, configurationFiles...))
		if operationError != nil {
			return document, operationError
		}
		fingerprintFiles := append([]string{projectFile}, configurationFiles...)
		fingerprintFiles = append(fingerprintFiles, importedFiles...)
		slices.Sort(fingerprintFiles)
		fingerprintFiles = slices.Compact(fingerprintFiles)
		if len(fingerprintFiles) > 0 {
			fingerprint, operationError := dotnetFilesFingerprint(workspaceDirectory, fingerprintFiles)
			if operationError != nil {
				return document, operationError
			}
			if reportedPackage.Fingerprint == nil {
				reportedPackage.Fingerprint = make(map[string]string)
			}
			reportedPackage.Fingerprint["imports:"+path.Base(projectFile)] = fingerprint
		}
		for _, reference := range references {
			for _, referencedFile := range dotnetReferencedFiles(workspaceDirectory, path.Join(path.Dir(projectFile), reference)) {
				queue = append(queue, referencedFile)
				if referencedDirectory := dotnetPackagePath(path.Dir(referencedFile)); referencedDirectory != directory {
					reportedPackage.Dependencies = append(reportedPackage.Dependencies, referencedDirectory)
				}
			}
		}
		slices.Sort(reportedPackage.Dependencies)
		reportedPackage.Dependencies = slices.Compact(reportedPackage.Dependencies)
		document.Packages[directory] = reportedPackage
	}
	if projectDirectory, descendantDirectory, found := dotnetConfigurationCycle(document, projectDirectories, configurationDependenciesByProject); found {
		return document, fmt.Errorf("dotnet project package %q references descendant package %q, creating a cycle through shared configuration", projectDirectory, descendantDirectory)
	}
	return document, nil
}

// dotnetConfigurationReferences returns effective configuration directories
// and source files above a project directory.
func dotnetConfigurationReferences(directory string, configurationDirectories map[string][]string) (dependencies []string, sources []string) {
	directories := slices.Collect(maps.Keys(configurationDirectories))
	slices.SortFunc(directories, func(first, second string) int { return len(second) - len(first) })
	selected := make(map[string]bool)
	for _, configurationDirectory := range directories {
		if configurationDirectory != directory && configurationDirectory != "" && !strings.HasPrefix(directory, configurationDirectory+"/") {
			continue
		}
		usesDirectory := false
		for _, file := range configurationDirectories[configurationDirectory] {
			lowerName := strings.ToLower(file)
			if !slices.Contains(dotnetConfigurationFiles, lowerName) || (lowerName != "nuget.config" && selected[lowerName]) {
				continue
			}
			selected[lowerName] = true
			usesDirectory = true
			if lowerName == "directory.build.props" || lowerName == "directory.build.targets" || lowerName == "directory.packages.props" {
				sources = append(sources, path.Join(configurationDirectory, file))
			}
		}
		if usesDirectory && configurationDirectory != directory {
			dependencies = append(dependencies, configurationDirectory)
		}
	}
	slices.Sort(dependencies)
	return slices.Compact(dependencies), sources
}

func dotnetConfigurationCycle(document Document, projectDirectories map[string]bool, configurationDependenciesByProject map[string][]string) (string, string, bool) {
	for _, directory := range slices.Sorted(maps.Keys(document.Packages)) {
		for _, dependency := range configurationDependenciesByProject[directory] {
			if !projectDirectories[dependency] {
				continue
			}
			visited := map[string]bool{dependency: true}
			for queue := []string{dependency}; len(queue) > 0; queue = queue[1:] {
				for _, reachable := range document.Packages[queue[0]].Dependencies {
					if reachable == directory {
						return dependency, directory, true
					}
					if !visited[reachable] {
						visited[reachable] = true
						queue = append(queue, reachable)
					}
				}
			}
		}
	}
	return "", "", false
}

// dotnetReferencedFiles resolves one reference, which may be a glob, to the
// project files it names inside the workspace. A reference to a project that
// does not exist is dropped, as MSBuild's Condition="Exists(...)" would.
func dotnetReferencedFiles(workspaceDirectory string, reference string) []string {
	reference = path.Clean(reference)
	if reference == ".." || strings.HasPrefix(reference, "../") {
		return nil
	}
	if strings.ContainsAny(reference, "*?") {
		matches, _ := doublestar.Glob(os.DirFS(workspaceDirectory), reference, doublestar.WithFilesOnly())
		return matches
	}
	if _, operationError := os.Stat(filepath.Join(workspaceDirectory, filepath.FromSlash(reference))); operationError != nil {
		return nil
	}
	return []string{reference}
}

func dotnetPackagePath(directory string) string {
	if directory == "." {
		return ""
	}
	return directory
}

// dotnetConfigurationDirectories maps every directory holding a file MSBuild
// imports into the projects below it to those files. The root is always
// present, since it also holds the solution files.
func dotnetConfigurationDirectories(resolverContext context.Context, workspaceDirectory string) (map[string][]string, error) {
	directories := map[string][]string{"": {"*.sln", "*.slnx"}}
	operationError := filepath.WalkDir(workspaceDirectory, func(filePath string, entry os.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		if operationError := resolverContext.Err(); operationError != nil {
			return operationError
		}
		if entry.IsDir() && (entry.Name() == "bin" || entry.Name() == "obj") {
			return filepath.SkipDir
		}
		if entry.IsDir() || !slices.Contains(dotnetConfigurationFileNames, entry.Name()) {
			return nil
		}
		relativePath, operationError := filepath.Rel(workspaceDirectory, filePath)
		if operationError != nil {
			return operationError
		}
		directory := dotnetPackagePath(path.Dir(filepath.ToSlash(relativePath)))
		directories[directory] = append(directories[directory], entry.Name())
		return nil
	})
	for _, files := range directories {
		slices.Sort(files)
	}
	return directories, operationError
}

func readDotnetProjectFile(projectPath string) (dotnetProjectFile, error) {
	var project dotnetProjectFile
	contents, operationError := os.ReadFile(projectPath)
	if operationError != nil {
		return project, fmt.Errorf("read %s: %w", projectPath, operationError)
	}
	if operationError := xml.Unmarshal(contents, &project); operationError != nil {
		return project, fmt.Errorf("parse %s: %w", projectPath, operationError)
	}
	return project, nil
}

func (project *dotnetProjectFile) UnmarshalXML(decoder *xml.Decoder, start xml.StartElement) error {
	for {
		token, operationError := decoder.Token()
		if operationError != nil {
			return operationError
		}
		switch element := token.(type) {
		case xml.StartElement:
			for _, attribute := range element.Attr {
				switch {
				case element.Name.Local == "Import" && attribute.Name.Local == "Project":
					project.Imports = append(project.Imports, dotnetImport{Project: attribute.Value})
				case element.Name.Local == "ProjectReference" && attribute.Name.Local == "Include":
					project.ProjectReferences = append(project.ProjectReferences, attribute.Value)
				}
			}
		case xml.EndElement:
			if element.Name == start.Name {
				return nil
			}
		}
	}
}

func dotnetImportedFiles(resolverContext context.Context, workspaceDirectory string, sourceFiles []string) ([]string, []string, []string, error) {
	visited := make(map[string]bool, len(sourceFiles))
	queue := slices.Clone(sourceFiles)
	for _, sourceFile := range sourceFiles {
		visited[sourceFile] = true
	}
	var importedFiles []string
	var references []string
	var importPatterns []string
	for len(queue) > 0 {
		if operationError := resolverContext.Err(); operationError != nil {
			return nil, nil, nil, operationError
		}
		sourceFile := queue[0]
		queue = queue[1:]
		project, operationError := readDotnetProjectFile(filepath.Join(workspaceDirectory, filepath.FromSlash(sourceFile)))
		if operationError != nil {
			return nil, nil, nil, operationError
		}
		for _, projectReference := range project.ProjectReferences {
			for include := range strings.SplitSeq(projectReference, ";") {
				include = strings.TrimSpace(strings.ReplaceAll(include, "\\", "/"))
				if include != "" && !strings.Contains(include, "$(") {
					references = append(references, include)
				}
			}
		}
		for _, imported := range project.Imports {
			matches, importPattern := dotnetImportFiles(workspaceDirectory, sourceFile, imported.Project)
			if importPattern != "" {
				importPatterns = append(importPatterns, importPattern)
			}
			for _, match := range matches {
				if operationError := resolverContext.Err(); operationError != nil {
					return nil, nil, nil, operationError
				}
				if visited[match] {
					continue
				}
				visited[match] = true
				importedFiles = append(importedFiles, match)
				queue = append(queue, match)
			}
		}
	}
	slices.Sort(importedFiles)
	slices.Sort(importPatterns)
	return importedFiles, references, slices.Compact(importPatterns), nil
}

func dotnetImportFiles(workspaceDirectory, sourceFile, projectValue string) ([]string, string) {
	importPath := strings.TrimSpace(strings.ReplaceAll(projectValue, "\\", "/"))
	if importPath == "" || strings.Contains(importPath, "$(") {
		return nil, ""
	}
	importPath = path.Clean(path.Join(path.Dir(sourceFile), importPath))
	if importPath == ".." || strings.HasPrefix(importPath, "../") {
		return nil, ""
	}
	matches := []string{importPath}
	importPattern := ""
	if strings.ContainsAny(importPath, "*?[") {
		importPattern = importPath
		matches, _ = doublestar.Glob(os.DirFS(workspaceDirectory), importPath, doublestar.WithFilesOnly())
	}
	return slices.DeleteFunc(matches, func(match string) bool {
		if ValidatePath(match) != nil {
			return true
		}
		fileInfo, operationError := os.Stat(filepath.Join(workspaceDirectory, filepath.FromSlash(match)))
		return operationError != nil || fileInfo.IsDir()
	}), importPattern
}

func dotnetFilesFingerprint(workspaceDirectory string, files []string) (string, error) {
	hasher := sha256.New()
	for _, file := range files {
		contents, operationError := os.ReadFile(filepath.Join(workspaceDirectory, filepath.FromSlash(file)))
		if operationError != nil {
			return "", fmt.Errorf("read %s: %w", file, operationError)
		}
		_, _ = fmt.Fprintf(hasher, "%s\x00%d\x00", file, len(contents))
		_, _ = hasher.Write(contents)
	}
	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}

// dotnetProjectFiles lists the projects every solution file in the workspace
// root names, or, without one, every project file in the tree.
func dotnetProjectFiles(resolverContext context.Context, workspaceDirectory string) ([]string, error) {
	if operationError := resolverContext.Err(); operationError != nil {
		return nil, operationError
	}
	fileSystem := os.DirFS(workspaceDirectory)
	solutions, operationError := doublestar.Glob(fileSystem, "*.{sln,slnx}")
	if operationError != nil {
		return nil, operationError
	}
	var projectFiles []string
	for _, solution := range solutions {
		if operationError := resolverContext.Err(); operationError != nil {
			return nil, operationError
		}
		contents, operationError := os.ReadFile(filepath.Join(workspaceDirectory, solution))
		if operationError != nil {
			return nil, fmt.Errorf("read %s: %w", solution, operationError)
		}
		var entries []string
		if strings.HasSuffix(solution, ".slnx") {
			entries, operationError = dotnetSlnxProjects(contents)
		} else {
			entries, operationError = dotnetSlnProjects(contents)
		}
		if operationError != nil {
			return nil, fmt.Errorf("parse %s: %w", filepath.Join(workspaceDirectory, solution), operationError)
		}
		for _, entry := range entries {
			entry = path.Clean(strings.ReplaceAll(entry, "\\", "/"))
			if !slices.Contains(dotnetProjectExtensions, path.Ext(entry)) || entry == ".." || strings.HasPrefix(entry, "../") {
				continue
			}
			projectFiles = append(projectFiles, entry)
		}
	}
	if len(solutions) == 0 {
		projectFiles, operationError = dotnetProjectFilesInTree(resolverContext, workspaceDirectory)
		if operationError != nil {
			return nil, operationError
		}
	}
	slices.Sort(projectFiles)
	return slices.Compact(projectFiles), nil
}

func dotnetProjectFilesInTree(resolverContext context.Context, workspaceDirectory string) ([]string, error) {
	var projectFiles []string
	operationError := filepath.WalkDir(workspaceDirectory, func(filePath string, entry os.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		if operationError := resolverContext.Err(); operationError != nil {
			return operationError
		}
		if entry.IsDir() && (entry.Name() == "bin" || entry.Name() == "obj") {
			return filepath.SkipDir
		}
		if entry.IsDir() || !slices.Contains(dotnetProjectExtensions, filepath.Ext(entry.Name())) {
			return nil
		}
		relativePath, operationError := filepath.Rel(workspaceDirectory, filePath)
		if operationError != nil {
			return operationError
		}
		projectFiles = append(projectFiles, filepath.ToSlash(relativePath))
		return nil
	})
	return projectFiles, operationError
}

func dotnetSlnProjects(contents []byte) ([]string, error) {
	var projects []string
	scanner := bufio.NewScanner(bytes.NewReader(contents))
	for scanner.Scan() {
		if match := dotnetSolutionProjectLine.FindStringSubmatch(strings.TrimSpace(scanner.Text())); match != nil {
			projects = append(projects, match[1])
		}
	}
	return projects, scanner.Err()
}

func dotnetSlnxProjects(contents []byte) ([]string, error) {
	var solution dotnetSolutionFolder
	if operationError := xml.Unmarshal(contents, &solution); operationError != nil {
		return nil, operationError
	}
	var projects []string
	var collect func(folder dotnetSolutionFolder)
	collect = func(folder dotnetSolutionFolder) {
		for _, project := range folder.Projects {
			projects = append(projects, project.Path)
		}
		for _, nested := range folder.Folders {
			collect(nested)
		}
	}
	collect(solution)
	return projects, nil
}

// dotnetDefaultInputs are the files the built-in reads: the solution files,
// every project file, since references lead to projects outside the
// solution, and every configuration file, since each creates a package.
func dotnetDefaultInputs(workspaceDirectory string) (inputs []string, excludeInputs []string) {
	inputs = []string{"*.sln", "*.slnx", "**/*.csproj", "**/*.fsproj", "**/*.vbproj", "**/Directory.Build.props", "**/Directory.Build.targets", "**/Directory.Packages.props", "**/global.json", "**/nuget.config", "**/NuGet.Config"}
	sources, operationError := doublestar.Glob(os.DirFS(workspaceDirectory), "**/*.{csproj,fsproj,vbproj,props,targets}", doublestar.WithFilesOnly())
	if operationError != nil {
		return inputs, dotnetExcludeInputs
	}
	var importedFiles []string
	for _, source := range sources {
		segments := strings.Split(path.Dir(source), "/")
		if slices.Contains(segments, "bin") || slices.Contains(segments, "obj") {
			continue
		}
		imports, _, importPatterns, operationError := dotnetImportedFiles(context.Background(), workspaceDirectory, []string{source})
		if operationError == nil {
			importedFiles = append(importedFiles, imports...)
			importedFiles = append(importedFiles, importPatterns...)
		}
	}
	slices.Sort(importedFiles)
	inputs = append(inputs, slices.Compact(importedFiles)...)
	return inputs, dotnetExcludeInputs
}
