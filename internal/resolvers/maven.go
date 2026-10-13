package resolvers

import (
	"context"
	"encoding/xml"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

type mavenCoordinates struct {
	GroupId    string `xml:"groupId"`
	ArtifactId string `xml:"artifactId"`
	Scope      string `xml:"scope"`
}

// mavenModel is the part of a pom a profile can override as well.
type mavenModel struct {
	Modules              []string           `xml:"modules>module"`
	Dependencies         []mavenCoordinates `xml:"dependencies>dependency"`
	DependencyManagement []mavenCoordinates `xml:"dependencyManagement>dependencies>dependency"`
	Build                struct {
		Plugins []struct {
			mavenCoordinates

			Dependencies []mavenCoordinates `xml:"dependencies>dependency"`
		} `xml:"plugins>plugin"`
		Extensions []mavenCoordinates `xml:"extensions>extension"`
	} `xml:"build"`
	Properties struct {
		Entries []struct {
			XMLName xml.Name
			Value   string `xml:",chardata"`
		} `xml:",any"`
	} `xml:"properties"`
}

// references lists everything a module needs from other modules: its
// dependencies, imported BOMs, build plugins with their dependencies, and
// build extensions.
func (model mavenModel) references() []mavenCoordinates {
	references := slices.Clone(model.Dependencies)
	for _, managed := range model.DependencyManagement {
		if strings.TrimSpace(managed.Scope) == "import" {
			references = append(references, managed)
		}
	}
	for _, plugin := range model.Build.Plugins {
		references = append(references, plugin.mavenCoordinates)
		references = append(references, plugin.Dependencies...)
	}
	return append(references, model.Build.Extensions...)
}

type mavenProject struct {
	mavenCoordinates
	mavenModel

	Parent struct {
		mavenCoordinates

		RelativePath *string `xml:"relativePath"`
	} `xml:"parent"`
	Profiles []mavenModel `xml:"profiles>profile"`
}

// mavenMember is a module with the properties visible to it: its own,
// its profiles', and those of the modules that aggregate it.
type mavenMember struct {
	project    mavenProject
	properties map[string]string
}

func (member mavenMember) interpolate(value string) string {
	value = strings.TrimSpace(value)
	for name, propertyValue := range member.properties {
		value = strings.ReplaceAll(value, "${"+name+"}", propertyValue)
	}
	return value
}

// mavenDependencies maps every module reachable from the root pom.xml to the
// modules it references, in any profile, and to its parent.
func mavenDependencies(resolverContext context.Context, workspaceDirectory string) (Document, error) {
	document := Document{Version: 1, Packages: make(map[string]Package)}
	if operationError := resolverContext.Err(); operationError != nil {
		return document, operationError
	}
	members, operationError := mavenMembers(workspaceDirectory)
	if operationError != nil {
		return document, operationError
	}
	directoryByCoordinates := make(map[string]string, len(members))
	for directory, member := range members {
		directoryByCoordinates[member.interpolate(mavenEffectiveGroupId(member.project))+":"+member.interpolate(member.project.ArtifactId)] = directory
	}
	for directory, member := range members {
		project := member.project
		reportedPackage := Package{Dependencies: []string{}, Inputs: []string{"pom.xml", "src/**/*"}, ExcludeInputs: mavenExcludeInputs}
		if directory == "" {
			reportedPackage.Inputs = []string{"pom.xml"}
		}
		references := project.references()
		for _, profile := range project.Profiles {
			references = append(references, profile.references()...)
		}
		candidates := []string{member.interpolate(project.Parent.GroupId) + ":" + member.interpolate(project.Parent.ArtifactId)}
		for _, reference := range references {
			candidates = append(candidates, member.interpolate(reference.GroupId)+":"+member.interpolate(reference.ArtifactId))
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
func mavenMembers(workspaceDirectory string) (map[string]mavenMember, error) {
	members := make(map[string]mavenMember)
	var visit func(directory string, inherited map[string]string) error
	visit = func(directory string, inherited map[string]string) error {
		if _, visited := members[directory]; visited {
			return nil
		}
		project, operationError := readMavenProject(filepath.Join(workspaceDirectory, directory, "pom.xml"))
		if operationError != nil {
			return operationError
		}
		member := mavenMember{project: project, properties: maps.Clone(inherited)}
		for _, model := range append([]mavenModel{project.mavenModel}, project.Profiles...) {
			for _, entry := range model.Properties.Entries {
				member.properties[entry.XMLName.Local] = strings.TrimSpace(entry.Value)
			}
		}
		effectiveGroupId := mavenEffectiveGroupId(project)
		for _, name := range []string{"project.groupId", "pom.groupId", "groupId"} {
			member.properties[name] = effectiveGroupId
		}
		member.properties["project.parent.groupId"] = project.Parent.GroupId
		members[directory] = member
		modules := slices.Clone(project.Modules)
		for _, profile := range project.Profiles {
			modules = append(modules, profile.Modules...)
		}
		for _, module := range modules {
			moduleDirectory, inside := mavenRelativeDirectory(directory, member.interpolate(module))
			if !inside {
				continue
			}
			if operationError := visit(moduleDirectory, member.properties); operationError != nil {
				return operationError
			}
		}
		return nil
	}
	return members, visit("", map[string]string{})
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
