package resolvers

import (
	"context"
	"crypto/sha256"
	"encoding/xml"
	"errors"
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
	Version    string `xml:"version"`
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
		if strings.TrimSpace(plugin.GroupId) == "" {
			plugin.GroupId = "org.apache.maven.plugins"
		}
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

type mavenResolvedModel struct {
	model      mavenModel
	properties map[string]string
	profile    bool
}

type mavenMember struct {
	project         mavenProject
	models          []mavenResolvedModel
	pomFile         string
	parentDirectory string
	hasParent       bool
}

func mavenInterpolate(value string, properties map[string]string) string {
	value = strings.TrimSpace(value)
	names := slices.Sorted(maps.Keys(properties))
	for range len(names) + 1 {
		previous := value
		for _, name := range names {
			value = strings.ReplaceAll(value, "${"+name+"}", properties[name])
		}
		if value == previous {
			break
		}
	}
	return value
}

func mavenModelProperties(project mavenProject, model mavenModel, inherited map[string]string) map[string]string {
	properties := make(map[string]string, len(inherited)+len(model.Properties.Entries)+9)
	maps.Copy(properties, inherited)
	for _, entry := range model.Properties.Entries {
		properties[entry.XMLName.Local] = strings.TrimSpace(entry.Value)
	}
	groupId := mavenInterpolate(mavenEffectiveGroupId(project), properties)
	artifactId := mavenInterpolate(project.ArtifactId, properties)
	version := mavenInterpolate(mavenEffectiveVersion(project), properties)
	for _, name := range []string{"project.groupId", "pom.groupId", "groupId"} {
		properties[name] = groupId
	}
	for _, name := range []string{"project.artifactId", "pom.artifactId", "artifactId"} {
		properties[name] = artifactId
	}
	for _, name := range []string{"project.version", "pom.version", "version"} {
		properties[name] = version
	}
	properties["project.parent.groupId"] = mavenInterpolate(project.Parent.GroupId, properties)
	properties["project.parent.artifactId"] = mavenInterpolate(project.Parent.ArtifactId, properties)
	properties["project.parent.version"] = mavenInterpolate(project.Parent.Version, properties)
	return properties
}

func mavenCoordinateKey(groupId, artifactId, version string) string {
	key := groupId + ":" + artifactId
	if version != "" {
		key += ":" + version
	}
	return key
}

func mavenReferenceKey(groupId, artifactId, version string) string {
	version = strings.TrimSpace(version)
	if strings.HasPrefix(version, "[") || strings.HasPrefix(version, "(") {
		version = ""
	}
	return mavenCoordinateKey(groupId, artifactId, version)
}

func mavenParentMatches(project, parent mavenProject) bool {
	projectProperties := mavenModelProperties(project, project.mavenModel, nil)
	parentProperties := mavenModelProperties(parent, parent.mavenModel, nil)
	return mavenInterpolate(project.Parent.GroupId, projectProperties) == mavenInterpolate(mavenEffectiveGroupId(parent), parentProperties) &&
		mavenInterpolate(project.Parent.ArtifactId, projectProperties) == mavenInterpolate(parent.ArtifactId, parentProperties) &&
		mavenInterpolate(project.Parent.Version, projectProperties) == mavenInterpolate(mavenEffectiveVersion(parent), parentProperties)
}

// mavenDependencies maps every module reachable from the root pom.xml to the
// modules it references, in any profile, and to its parent.
func mavenDependencies(resolverContext context.Context, workspaceDirectory string) (Document, error) {
	document := Document{Version: 1, Packages: make(map[string]Package)}
	members, operationError := mavenMembers(resolverContext, workspaceDirectory)
	if operationError != nil {
		return document, operationError
	}
	directoriesByCoordinates := make(map[string][]string, len(members))
	for directory, member := range members {
		for _, resolvedModel := range member.models {
			groupId := mavenInterpolate(mavenEffectiveGroupId(member.project), resolvedModel.properties)
			artifactId := mavenInterpolate(member.project.ArtifactId, resolvedModel.properties)
			version := mavenInterpolate(mavenEffectiveVersion(member.project), resolvedModel.properties)
			keys := []string{mavenCoordinateKey(groupId, artifactId, "")}
			if version != "" {
				keys = append(keys, mavenCoordinateKey(groupId, artifactId, version))
			}
			for _, key := range keys {
				directoriesByCoordinates[key] = append(directoriesByCoordinates[key], directory)
			}
		}
	}
	for key, directories := range directoriesByCoordinates {
		slices.Sort(directories)
		directoriesByCoordinates[key] = slices.Compact(directories)
	}
	profileDependencies := make(map[string][]string)
	for directory, member := range members {
		project := member.project
		reportedPackage := Package{Dependencies: []string{}, Inputs: []string{member.pomFile, "src/**/*"}, ExcludeInputs: mavenExcludeInputs}
		if directory == "" {
			reportedPackage.Inputs = []string{member.pomFile}
		}
		for _, resolvedModel := range member.models {
			references := append([]mavenCoordinates{project.Parent.mavenCoordinates}, resolvedModel.model.references()...)
			for _, reference := range references {
				groupId := mavenInterpolate(reference.GroupId, resolvedModel.properties)
				artifactId := mavenInterpolate(reference.ArtifactId, resolvedModel.properties)
				version := mavenInterpolate(reference.Version, resolvedModel.properties)
				if groupId == "" || artifactId == "" {
					continue
				}
				for _, dependencyDirectory := range directoriesByCoordinates[mavenReferenceKey(groupId, artifactId, version)] {
					if dependencyDirectory != directory {
						if resolvedModel.profile {
							profileDependencies[directory] = append(profileDependencies[directory], dependencyDirectory)
						} else {
							reportedPackage.Dependencies = append(reportedPackage.Dependencies, dependencyDirectory)
						}
					}
				}
			}
		}
		if member.hasParent && member.parentDirectory != directory {
			if _, isMember := members[member.parentDirectory]; isMember {
				reportedPackage.Dependencies = append(reportedPackage.Dependencies, member.parentDirectory)
			}
		}
		slices.Sort(reportedPackage.Dependencies)
		reportedPackage.Dependencies = slices.Compact(reportedPackage.Dependencies)
		document.Packages[directory] = reportedPackage
	}
	if operationError := mavenApplyProfileDependencies(resolverContext, workspaceDirectory, members, profileDependencies, &document); operationError != nil {
		return document, operationError
	}
	return document, nil
}

func mavenApplyProfileDependencies(resolverContext context.Context, workspaceDirectory string, members map[string]mavenMember, profileDependencies map[string][]string, document *Document) error {
	profileCycleFingerprint := ""
	for _, directory := range slices.Sorted(maps.Keys(profileDependencies)) {
		dependencies := profileDependencies[directory]
		slices.Sort(dependencies)
		for _, dependencyDirectory := range slices.Compact(dependencies) {
			if mavenAddsCycle(*document, directory, dependencyDirectory) {
				if profileCycleFingerprint == "" {
					fingerprint, operationError := mavenInputsFingerprint(resolverContext, workspaceDirectory, members)
					if operationError != nil {
						return operationError
					}
					profileCycleFingerprint = fingerprint
				}
				reportedPackage := document.Packages[directory]
				if reportedPackage.Fingerprint == nil {
					reportedPackage.Fingerprint = make(map[string]string)
				}
				reportedPackage.Fingerprint["profile-cycle"] = profileCycleFingerprint
				document.Packages[directory] = reportedPackage
				continue
			}
			reportedPackage := document.Packages[directory]
			reportedPackage.Dependencies = append(reportedPackage.Dependencies, dependencyDirectory)
			slices.Sort(reportedPackage.Dependencies)
			reportedPackage.Dependencies = slices.Compact(reportedPackage.Dependencies)
			document.Packages[directory] = reportedPackage
		}
	}
	return nil
}

func mavenAddsCycle(document Document, directory, dependencyDirectory string) bool {
	visited := map[string]bool{dependencyDirectory: true}
	for queue := []string{dependencyDirectory}; len(queue) > 0; queue = queue[1:] {
		if queue[0] == directory {
			return true
		}
		for _, dependency := range document.Packages[queue[0]].Dependencies {
			if !visited[dependency] {
				visited[dependency] = true
				queue = append(queue, dependency)
			}
		}
	}
	return false
}

func mavenInputsFingerprint(resolverContext context.Context, workspaceDirectory string, members map[string]mavenMember) (string, error) {
	var files []string
	for directory, member := range members {
		files = append(files, path.Join(directory, member.pomFile))
		sourceDirectory := filepath.Join(workspaceDirectory, filepath.FromSlash(directory), "src")
		operationError := filepath.WalkDir(sourceDirectory, func(filePath string, entry os.DirEntry, walkError error) error {
			if errors.Is(walkError, os.ErrNotExist) {
				return nil
			}
			if walkError != nil {
				return walkError
			}
			if operationError := resolverContext.Err(); operationError != nil {
				return operationError
			}
			if entry.IsDir() {
				return nil
			}
			relativePath, operationError := filepath.Rel(workspaceDirectory, filePath)
			if operationError != nil {
				return operationError
			}
			files = append(files, filepath.ToSlash(relativePath))
			return nil
		})
		if operationError != nil {
			return "", operationError
		}
	}
	slices.Sort(files)
	hasher := sha256.New()
	for _, file := range files {
		contents, operationError := os.ReadFile(filepath.Join(workspaceDirectory, filepath.FromSlash(file)))
		if operationError != nil {
			return "", operationError
		}
		_, _ = fmt.Fprintf(hasher, "%s\x00%d\x00", file, len(contents))
		_, _ = hasher.Write(contents)
	}
	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}

// mavenMembers reads the root pom.xml and every module it lists, directly or
// through a profile, recursively. Modules outside the workspace are skipped.
type mavenWorkspace struct {
	context         context.Context
	directory       string
	projects        map[string]mavenProject
	projectFiles    map[string]string
	models          map[string][]mavenResolvedModel
	parentDirectory map[string]string
	resolving       map[string]bool
}

func (workspace *mavenWorkspace) readProject(directory string) (mavenProject, error) {
	projectFile := workspace.projectFiles[directory]
	if projectFile == "" {
		projectFile = "pom.xml"
	}
	return workspace.readProjectFile(directory, projectFile)
}

func (workspace *mavenWorkspace) readProjectFile(directory, projectFile string) (mavenProject, error) {
	if operationError := workspace.context.Err(); operationError != nil {
		return mavenProject{}, operationError
	}
	if project, exists := workspace.projects[directory]; exists {
		return project, nil
	}
	project, operationError := readMavenProject(filepath.Join(workspace.directory, filepath.FromSlash(directory), filepath.FromSlash(projectFile)))
	if operationError == nil {
		workspace.projects[directory] = project
		workspace.projectFiles[directory] = projectFile
	}
	return project, operationError
}

func (workspace *mavenWorkspace) modelsFor(directory string) ([]mavenResolvedModel, error) {
	if models, exists := workspace.models[directory]; exists {
		return models, nil
	}
	if workspace.resolving[directory] {
		return nil, fmt.Errorf("maven parent cycle at %q", directory)
	}
	workspace.resolving[directory] = true
	defer delete(workspace.resolving, directory)
	project, operationError := workspace.readProject(directory)
	if operationError != nil {
		return nil, operationError
	}
	inheritedModels := []mavenResolvedModel{{properties: map[string]string{}}}
	parentDirectory := ""
	hasParent := false
	if relativeParentDirectory, relativeParentFile, inside := mavenParentLocation(directory, project); inside && relativeParentDirectory != directory {
		parentProject, operationError := workspace.readProjectFile(relativeParentDirectory, relativeParentFile)
		if operationError == nil && mavenParentMatches(project, parentProject) {
			parentDirectory = relativeParentDirectory
			hasParent = true
		} else if operationError != nil && !errors.Is(operationError, os.ErrNotExist) {
			return nil, operationError
		}
	}
	if !hasParent {
		parentDirectory, hasParent = workspace.coordinateParentDirectory(directory, project)
	}
	if hasParent {
		inheritedModels, operationError = workspace.modelsFor(parentDirectory)
		if operationError != nil {
			return nil, operationError
		}
		workspace.parentDirectory[directory] = parentDirectory
	}
	var models []mavenResolvedModel
	for _, inheritedModel := range inheritedModels {
		baseProperties := mavenModelProperties(project, project.mavenModel, inheritedModel.properties)
		models = append(models, mavenResolvedModel{model: project.mavenModel, properties: baseProperties, profile: inheritedModel.profile})
		for _, profile := range project.Profiles {
			profileProperties := mavenModelProperties(project, profile, baseProperties)
			models = append(models,
				mavenResolvedModel{model: project.mavenModel, properties: profileProperties, profile: true},
				mavenResolvedModel{model: profile, properties: profileProperties, profile: true},
			)
		}
	}
	workspace.models[directory] = models
	return models, nil
}

func (workspace *mavenWorkspace) coordinateParentDirectory(directory string, project mavenProject) (string, bool) {
	if project.Parent.ArtifactId == "" {
		return "", false
	}
	for _, candidateDirectory := range slices.Sorted(maps.Keys(workspace.projects)) {
		if candidateDirectory != directory && mavenParentMatches(project, workspace.projects[candidateDirectory]) {
			return candidateDirectory, true
		}
	}
	return "", false
}

func mavenMembers(resolverContext context.Context, workspaceDirectory string) (map[string]mavenMember, error) {
	members := make(map[string]mavenMember)
	workspace := mavenWorkspace{
		context:         resolverContext,
		directory:       workspaceDirectory,
		projects:        make(map[string]mavenProject),
		projectFiles:    make(map[string]string),
		models:          make(map[string][]mavenResolvedModel),
		parentDirectory: make(map[string]string),
		resolving:       make(map[string]bool),
	}
	var addParent func(directory string) error
	addParent = func(directory string) error {
		if _, exists := members[directory]; exists {
			return nil
		}
		project, operationError := workspace.readProject(directory)
		if operationError != nil {
			return operationError
		}
		models, operationError := workspace.modelsFor(directory)
		if operationError != nil {
			return operationError
		}
		parentDirectory, hasParent := workspace.parentDirectory[directory]
		members[directory] = mavenMember{project: project, models: models, pomFile: workspace.projectFiles[directory], parentDirectory: parentDirectory, hasParent: hasParent}
		if hasParent {
			return addParent(parentDirectory)
		}
		return nil
	}
	reactorVisited := make(map[string]bool)
	var visit func(directory string) error
	visit = func(directory string) error {
		if operationError := resolverContext.Err(); operationError != nil {
			return operationError
		}
		if reactorVisited[directory] {
			return nil
		}
		project, operationError := workspace.readProject(directory)
		if operationError != nil {
			return operationError
		}
		models, operationError := workspace.modelsFor(directory)
		if operationError != nil {
			return operationError
		}
		parentDirectory, hasParent := workspace.parentDirectory[directory]
		member := mavenMember{project: project, models: models, pomFile: workspace.projectFiles[directory], parentDirectory: parentDirectory, hasParent: hasParent}
		members[directory] = member
		reactorVisited[directory] = true
		if hasParent {
			if operationError := addParent(parentDirectory); operationError != nil {
				return operationError
			}
		}
		var moduleDirectories []string
		for _, resolvedModel := range member.models {
			for _, module := range resolvedModel.model.Modules {
				moduleDirectory, inside := mavenRelativeDirectory(directory, mavenInterpolate(module, resolvedModel.properties))
				if !inside {
					continue
				}
				moduleDirectories = append(moduleDirectories, moduleDirectory)
			}
		}
		slices.Sort(moduleDirectories)
		moduleDirectories = slices.Compact(moduleDirectories)
		for _, moduleDirectory := range moduleDirectories {
			if _, operationError := workspace.readProject(moduleDirectory); operationError != nil {
				return operationError
			}
		}
		for _, moduleDirectory := range moduleDirectories {
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

func mavenParentLocation(directory string, project mavenProject) (string, string, bool) {
	if project.Parent.ArtifactId == "" {
		return "", "", false
	}
	relativePath := "../pom.xml"
	if project.Parent.RelativePath != nil {
		relativePath = *project.Parent.RelativePath
	}
	if strings.TrimSpace(relativePath) == "" {
		return "", "", false
	}
	projectPath := path.Join(directory, filepath.ToSlash(strings.TrimSpace(relativePath)))
	if projectPath == ".." || strings.HasPrefix(projectPath, "../") {
		return "", "", false
	}
	parentDirectory := path.Dir(projectPath)
	if parentDirectory == "." {
		parentDirectory = ""
	}
	return parentDirectory, path.Base(projectPath), true
}

func mavenEffectiveGroupId(project mavenProject) string {
	if project.GroupId != "" {
		return project.GroupId
	}
	return project.Parent.GroupId
}

func mavenEffectiveVersion(project mavenProject) string {
	if project.Version != "" {
		return project.Version
	}
	return project.Parent.Version
}

// mavenExcludeInputs keeps the globs out of the module's build output.
var mavenExcludeInputs = []string{"target/**"}

// mavenDefaultInputs are the files the built-in reads: the root pom and the
// pom of every module reachable from it.
func mavenDefaultInputs(workspaceDirectory string) (inputs []string, excludeInputs []string) {
	inputs = []string{"pom.xml"}
	members, operationError := mavenMembers(context.Background(), workspaceDirectory)
	if operationError != nil {
		return inputs, mavenExcludeInputs
	}
	for directory, member := range members {
		input := path.Join(directory, member.pomFile)
		if input != "pom.xml" {
			inputs = append(inputs, input)
		}
	}
	if document, operationError := mavenDependencies(context.Background(), workspaceDirectory); operationError == nil {
		for _, reportedPackage := range document.Packages {
			if reportedPackage.Fingerprint["profile-cycle"] != "" {
				inputs = append(inputs, "**/src/**/*")
				break
			}
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
