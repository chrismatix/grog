package loading

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

type mavenCoordinates struct {
	GroupId    string `xml:"groupId"`
	ArtifactId string `xml:"artifactId"`
}

type mavenProject struct {
	mavenCoordinates

	Parent struct {
		mavenCoordinates

		RelativePath *string `xml:"relativePath"`
	} `xml:"parent"`
	Modules  []string `xml:"modules>module"`
	Profiles []struct {
		Modules []string `xml:"modules>module"`
	} `xml:"profiles>profile"`
	Dependencies []mavenCoordinates `xml:"dependencies>dependency"`
}

// mavenDependencies maps every module reachable from the root pom.xml to the
// modules its dependencies and parent name. Every module also depends on the
// root, since managed versions and plugin configuration are inherited from it.
func mavenDependencies(resolverContext context.Context, workspaceDirectory string) (resolverDocument, error) {
	document := resolverDocument{Version: 1, Packages: make(map[string]resolverPackage)}
	if operationError := resolverContext.Err(); operationError != nil {
		return document, operationError
	}
	members, operationError := mavenMembers(workspaceDirectory)
	if operationError != nil {
		return document, operationError
	}
	directoryByCoordinates := make(map[string]string, len(members))
	for directory, project := range members {
		directoryByCoordinates[mavenEffectiveGroupId(project)+":"+project.ArtifactId] = directory
	}
	for directory, project := range members {
		reportedPackage := resolverPackage{Dependencies: []string{}, Inputs: []string{"pom.xml", "src/**/*"}, ExcludeInputs: mavenExcludeInputs}
		if directory == "" {
			reportedPackage.Inputs = []string{"pom.xml"}
		} else {
			reportedPackage.Dependencies = append(reportedPackage.Dependencies, "")
		}
		candidates := []string{project.Parent.GroupId + ":" + project.Parent.ArtifactId}
		for _, dependency := range project.Dependencies {
			candidates = append(candidates, interpolateMavenGroupId(dependency.GroupId, project)+":"+dependency.ArtifactId)
		}
		for _, candidate := range candidates {
			if dependencyDirectory, isMember := directoryByCoordinates[candidate]; isMember && dependencyDirectory != directory {
				reportedPackage.Dependencies = append(reportedPackage.Dependencies, dependencyDirectory)
			}
		}
		if parentDirectory, hasParent := mavenParentDirectory(directory, project); hasParent && parentDirectory != directory {
			if _, isMember := members[parentDirectory]; isMember {
				reportedPackage.Dependencies = append(reportedPackage.Dependencies, parentDirectory)
			}
		}
		slices.Sort(reportedPackage.Dependencies)
		reportedPackage.Dependencies = slices.Compact(reportedPackage.Dependencies)
		document.Packages[directory] = reportedPackage
	}
	return document, nil
}

// mavenMembers reads the root pom.xml and every module it lists, directly or
// through a profile, recursively. Modules outside the workspace are skipped.
func mavenMembers(workspaceDirectory string) (map[string]mavenProject, error) {
	members := make(map[string]mavenProject)
	var visit func(directory string) error
	visit = func(directory string) error {
		if _, visited := members[directory]; visited {
			return nil
		}
		project, operationError := readMavenProject(filepath.Join(workspaceDirectory, directory, "pom.xml"))
		if operationError != nil {
			return operationError
		}
		members[directory] = project
		modules := slices.Clone(project.Modules)
		for _, profile := range project.Profiles {
			modules = append(modules, profile.Modules...)
		}
		for _, module := range modules {
			moduleDirectory, inside := mavenRelativeDirectory(directory, module)
			if !inside {
				continue
			}
			if operationError := visit(moduleDirectory); operationError != nil {
				return operationError
			}
		}
		return nil
	}
	return members, visit("")
}

// mavenRelativeDirectory resolves a module or parent reference, which may
// name a directory or a pom file, against the referencing module's directory.
func mavenRelativeDirectory(fromDirectory, reference string) (string, bool) {
	reference = filepath.ToSlash(strings.TrimSpace(reference))
	if strings.HasSuffix(reference, ".xml") {
		reference = path.Dir(reference)
	}
	directory := path.Join(fromDirectory, reference)
	if directory == "." {
		directory = ""
	}
	if directory == ".." || strings.HasPrefix(directory, "../") {
		return "", false
	}
	return directory, true
}

// mavenParentDirectory follows <relativePath>, which defaults to ../pom.xml.
// An empty element tells maven to skip the lookup, so it yields nothing.
func mavenParentDirectory(directory string, project mavenProject) (string, bool) {
	if project.Parent.ArtifactId == "" {
		return "", false
	}
	relativePath := "../pom.xml"
	if project.Parent.RelativePath != nil {
		relativePath = *project.Parent.RelativePath
	}
	if strings.TrimSpace(relativePath) == "" {
		return "", false
	}
	return mavenRelativeDirectory(directory, relativePath)
}

func mavenEffectiveGroupId(project mavenProject) string {
	if project.GroupId != "" {
		return project.GroupId
	}
	return project.Parent.GroupId
}

func interpolateMavenGroupId(value string, project mavenProject) string {
	effectiveGroupId := mavenEffectiveGroupId(project)
	return strings.NewReplacer(
		"${project.groupId}", effectiveGroupId,
		"${pom.groupId}", effectiveGroupId,
		"${groupId}", effectiveGroupId,
		"${project.parent.groupId}", project.Parent.GroupId,
	).Replace(value)
}

// mavenExcludeInputs keeps the globs out of the module's build output.
var mavenExcludeInputs = []string{"target/**"}

// mavenDefaultInputs are the files the built-in reads: the root pom and the
// pom of every module reachable from it.
func mavenDefaultInputs(workspaceDirectory string) (inputs []string, excludeInputs []string) {
	inputs = []string{"pom.xml"}
	members, operationError := mavenMembers(workspaceDirectory)
	if operationError != nil {
		return inputs, mavenExcludeInputs
	}
	for directory := range members {
		if directory != "" {
			inputs = append(inputs, path.Join(directory, "pom.xml"))
		}
	}
	slices.Sort(inputs)
	return inputs, mavenExcludeInputs
}

func readMavenProject(projectPath string) (mavenProject, error) {
	var project mavenProject
	contents, operationError := os.ReadFile(projectPath)
	if operationError != nil {
		return project, fmt.Errorf("read %s: %w", projectPath, operationError)
	}
	if operationError := xml.Unmarshal(contents, &project); operationError != nil {
		return project, fmt.Errorf("parse %s: %w", projectPath, operationError)
	}
	return project, nil
}
