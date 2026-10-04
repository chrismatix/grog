package loading

import (
	"bufio"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

type dotnetProjectFile struct {
	ItemGroups []struct {
		ProjectReferences []struct {
			Include string `xml:"Include,attr"`
		} `xml:"ProjectReference"`
	} `xml:"ItemGroup"`
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

// dotnetExcludeInputs keeps every glob out of build outputs.
var dotnetExcludeInputs = []string{"**/bin/**", "**/obj/**"}

// dotnetDependencies maps every project to the projects its ProjectReference
// items name, following references to projects the solution does not list.
// Every project also depends on each ancestor directory holding configuration
// files MSBuild imports from above the project, the root included.
func dotnetDependencies(resolverContext context.Context, workspaceDirectory string) (resolverDocument, error) {
	document := resolverDocument{Version: 1, Packages: make(map[string]resolverPackage)}
	projectFiles, operationError := dotnetProjectFiles(workspaceDirectory)
	if operationError != nil {
		return document, operationError
	}
	configurationDirectories, operationError := dotnetConfigurationDirectories(workspaceDirectory)
	if operationError != nil {
		return document, operationError
	}
	for directory, files := range configurationDirectories {
		document.Packages[directory] = resolverPackage{Dependencies: []string{}, Inputs: files, ExcludeInputs: dotnetExcludeInputs}
	}
	visited := make(map[string]bool)
	for queue := projectFiles; len(queue) > 0; queue = queue[1:] {
		projectFile := queue[0]
		if visited[projectFile] {
			continue
		}
		visited[projectFile] = true
		if operationError := resolverContext.Err(); operationError != nil {
			return document, operationError
		}
		references, operationError := dotnetProjectReferences(filepath.Join(workspaceDirectory, filepath.FromSlash(projectFile)))
		if operationError != nil {
			return document, operationError
		}
		directory := dotnetPackagePath(path.Dir(projectFile))
		reportedPackage, exists := document.Packages[directory]
		if _, isConfigurationDirectory := configurationDirectories[directory]; !exists || isConfigurationDirectory {
			reportedPackage = resolverPackage{Dependencies: reportedPackage.Dependencies, Inputs: []string{"**/*"}, ExcludeInputs: dotnetExcludeInputs}
		}
		for configurationDirectory := range configurationDirectories {
			if (configurationDirectory == "" && directory != "") || strings.HasPrefix(directory, configurationDirectory+"/") {
				reportedPackage.Dependencies = append(reportedPackage.Dependencies, configurationDirectory)
			}
		}
		for _, reference := range references {
			referencedFile := path.Clean(path.Join(path.Dir(projectFile), reference))
			if referencedFile == ".." || strings.HasPrefix(referencedFile, "../") {
				continue
			}
			queue = append(queue, referencedFile)
			if referencedDirectory := dotnetPackagePath(path.Dir(referencedFile)); referencedDirectory != directory {
				reportedPackage.Dependencies = append(reportedPackage.Dependencies, referencedDirectory)
			}
		}
		slices.Sort(reportedPackage.Dependencies)
		reportedPackage.Dependencies = slices.Compact(reportedPackage.Dependencies)
		document.Packages[directory] = reportedPackage
	}
	return document, nil
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
func dotnetConfigurationDirectories(workspaceDirectory string) (map[string][]string, error) {
	directories := map[string][]string{"": {"*.sln", "*.slnx"}}
	operationError := filepath.WalkDir(workspaceDirectory, func(filePath string, entry os.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		if entry.IsDir() && (entry.Name() == "bin" || entry.Name() == "obj") {
			return filepath.SkipDir
		}
		if entry.IsDir() || !slices.Contains(dotnetConfigurationFiles, strings.ToLower(entry.Name())) {
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

// dotnetProjectReferences returns the slash-separated, project-relative paths
// a project file references. MSBuild property references cannot be resolved
// without MSBuild and are skipped.
func dotnetProjectReferences(projectPath string) ([]string, error) {
	contents, operationError := os.ReadFile(projectPath)
	if operationError != nil {
		return nil, fmt.Errorf("read %s: %w", projectPath, operationError)
	}
	var project dotnetProjectFile
	if operationError := xml.Unmarshal(contents, &project); operationError != nil {
		return nil, fmt.Errorf("parse %s: %w", projectPath, operationError)
	}
	var references []string
	for _, itemGroup := range project.ItemGroups {
		for _, projectReference := range itemGroup.ProjectReferences {
			for include := range strings.SplitSeq(projectReference.Include, ";") {
				include = strings.TrimSpace(strings.ReplaceAll(include, "\\", "/"))
				if include == "" || strings.Contains(include, "$(") {
					continue
				}
				references = append(references, include)
			}
		}
	}
	return references, nil
}

// dotnetProjectFiles lists the projects every solution file in the workspace
// root names, or, without one, every project file in the tree.
func dotnetProjectFiles(workspaceDirectory string) ([]string, error) {
	fileSystem := os.DirFS(workspaceDirectory)
	solutions, operationError := doublestar.Glob(fileSystem, "*.{sln,slnx}")
	if operationError != nil {
		return nil, operationError
	}
	var projectFiles []string
	for _, solution := range solutions {
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
		matches, operationError := doublestar.Glob(fileSystem, "**/*.{csproj,fsproj,vbproj}")
		if operationError != nil {
			return nil, operationError
		}
		for _, match := range matches {
			segments := strings.Split(path.Dir(match), "/")
			if !slices.Contains(segments, "bin") && !slices.Contains(segments, "obj") {
				projectFiles = append(projectFiles, match)
			}
		}
	}
	slices.Sort(projectFiles)
	return slices.Compact(projectFiles), nil
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
func dotnetDefaultInputs(string) (inputs []string, excludeInputs []string) {
	inputs = []string{"*.sln", "*.slnx", "**/*.csproj", "**/*.fsproj", "**/*.vbproj", "**/Directory.Build.props", "**/Directory.Build.targets", "**/Directory.Packages.props", "**/global.json", "**/nuget.config", "**/NuGet.Config"}
	return inputs, dotnetExcludeInputs
}
