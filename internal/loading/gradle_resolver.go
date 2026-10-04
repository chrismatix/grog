package loading

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

var (
	gradleIncludePattern          = regexp.MustCompile(`\binclude\s*(?:\(([^)]*)\)|((?:["'][^"']*["']\s*,?\s*)+))`)
	gradleQuotedPattern           = regexp.MustCompile(`["']([^"']*)["']`)
	gradleProjectDirPattern       = regexp.MustCompile(`\bproject\(\s*["'](:[^"']*)["']\s*\)\.projectDir\s*=\s*(?:new\s+)?[fF]ile\(\s*(?:(?:rootDir|settingsDir)\s*,\s*)?["']([^"']+)["']`)
	gradleIncludeBuildPattern     = regexp.MustCompile(`\bincludeBuild\s*\(?\s*["']([^"']+)["']`)
	gradleProjectReferencePattern = regexp.MustCompile(`\bproject\(\s*(?:path\s*[:=]\s*)?["'](:[^"']*)["']`)
	gradleAccessorPattern         = regexp.MustCompile(`\bprojects((?:\.[A-Za-z_][A-Za-z0-9_]*)+)`)
)

var gradleExcludeInputs = []string{"build/**", ".gradle/**"}

// gradleDependencies reads the settings file for the project tree and every
// project's build file for project(":x") and projects.x references. Every
// project depends on the root, which owns the version catalogs and buildSrc.
func gradleDependencies(resolverContext context.Context, workspaceDirectory string) (resolverDocument, error) {
	document := resolverDocument{Version: 1, Packages: make(map[string]resolverPackage)}
	settings, operationError := readGradleFiles(workspaceDirectory, "settings.gradle.kts", "settings.gradle")
	if operationError != nil {
		return document, operationError
	}
	if settings == "" {
		return document, fmt.Errorf("no settings.gradle or settings.gradle.kts in %s", workspaceDirectory)
	}
	directoryByProject := gradleProjectDirectories(workspaceDirectory, settings)
	accessorByProject := make(map[string]string, len(directoryByProject))
	for projectPath := range directoryByProject {
		accessorByProject[gradleAccessor(projectPath)] = projectPath
	}

	root := resolverPackage{
		Dependencies:  []string{},
		Inputs:        []string{"settings.gradle", "settings.gradle.kts", "build.gradle", "build.gradle.kts", "gradle.properties", "gradle/**", "buildSrc/**"},
		ExcludeInputs: []string{"build/**", ".gradle/**", "buildSrc/build/**", "buildSrc/.gradle/**"},
	}
	for _, match := range gradleIncludeBuildPattern.FindAllStringSubmatch(settings, -1) {
		includedBuild := path.Clean(filepath.ToSlash(match[1]))
		if validateResolverPath(includedBuild) != nil || includedBuild == "." {
			continue
		}
		root.Dependencies = append(root.Dependencies, includedBuild)
		document.Packages[includedBuild] = resolverPackage{Dependencies: []string{}, Inputs: []string{"**/*"}, ExcludeInputs: []string{"**/build/**", "**/.gradle/**"}}
	}
	slices.Sort(root.Dependencies)
	root.Dependencies = slices.Compact(root.Dependencies)
	document.Packages[""] = root

	for projectPath, directory := range directoryByProject {
		if operationError := resolverContext.Err(); operationError != nil {
			return document, operationError
		}
		buildFile, operationError := readGradleFiles(filepath.Join(workspaceDirectory, directory), "build.gradle.kts", "build.gradle")
		if operationError != nil {
			return document, operationError
		}
		reportedPackage := resolverPackage{
			Dependencies:  []string{""},
			Inputs:        []string{"build.gradle", "build.gradle.kts", "gradle.properties", "src/**/*"},
			ExcludeInputs: gradleExcludeInputs,
		}
		for _, referenced := range gradleProjectReferences(buildFile, directoryByProject, accessorByProject) {
			if referenced != projectPath {
				reportedPackage.Dependencies = append(reportedPackage.Dependencies, directoryByProject[referenced])
			}
		}
		slices.Sort(reportedPackage.Dependencies)
		reportedPackage.Dependencies = slices.Compact(reportedPackage.Dependencies)
		document.Packages[directory] = reportedPackage
	}
	return document, nil
}

// gradleProjectDirectories maps every included project path (":a:b") to its
// directory, honouring projectDir overrides. The root project is left out.
// Including ":a:b" also creates ":a"; it is kept when its directory exists.
func gradleProjectDirectories(workspaceDirectory string, settings string) map[string]string {
	directories := make(map[string]string)
	for _, match := range gradleIncludePattern.FindAllStringSubmatch(settings, -1) {
		for _, quoted := range gradleQuotedPattern.FindAllStringSubmatch(match[1]+match[2], -1) {
			projectPath := ":" + strings.Trim(quoted[1], ":")
			if projectPath == ":" {
				continue
			}
			directories[projectPath] = strings.ReplaceAll(strings.TrimPrefix(projectPath, ":"), ":", "/")
		}
	}
	for projectPath := range directories {
		for parent := projectPath[:strings.LastIndex(projectPath, ":")]; parent != ""; parent = parent[:strings.LastIndex(parent, ":")] {
			if _, included := directories[parent]; included {
				continue
			}
			directory := strings.ReplaceAll(strings.TrimPrefix(parent, ":"), ":", "/")
			if info, statError := os.Stat(filepath.Join(workspaceDirectory, filepath.FromSlash(directory))); statError == nil && info.IsDir() {
				directories[parent] = directory
			}
		}
	}
	for _, match := range gradleProjectDirPattern.FindAllStringSubmatch(settings, -1) {
		if _, included := directories[match[1]]; included {
			directories[match[1]] = path.Clean(filepath.ToSlash(match[2]))
		}
	}
	for projectPath, directory := range directories {
		if validateResolverPath(directory) != nil || directory == "." {
			delete(directories, projectPath)
		}
	}
	return directories
}

// gradleProjectReferences lists the project paths a build file mentions,
// through project(":x") or the type-safe accessor projects.x. An accessor
// chain may continue into a property, so its longest known prefix wins.
func gradleProjectReferences(buildFile string, directoryByProject map[string]string, accessorByProject map[string]string) []string {
	var references []string
	for _, match := range gradleProjectReferencePattern.FindAllStringSubmatch(buildFile, -1) {
		if _, included := directoryByProject[match[1]]; included {
			references = append(references, match[1])
		}
	}
	for _, match := range gradleAccessorPattern.FindAllStringSubmatch(buildFile, -1) {
		segments := strings.Split(strings.TrimPrefix(match[1], "."), ".")
		for length := len(segments); length > 0; length-- {
			if projectPath, included := accessorByProject[strings.Join(segments[:length], ".")]; included {
				references = append(references, projectPath)
				break
			}
		}
	}
	return references
}

// gradleAccessor is the type-safe accessor for a project path: each segment
// has its "-", "_" and "." separators folded into camelCase.
func gradleAccessor(projectPath string) string {
	segments := strings.Split(strings.TrimPrefix(projectPath, ":"), ":")
	for index, segment := range segments {
		var accessor strings.Builder
		capitalizeNext := false
		for _, character := range segment {
			if character == '-' || character == '_' || character == '.' {
				capitalizeNext = true
				continue
			}
			if capitalizeNext {
				character = unicode.ToUpper(character)
				capitalizeNext = false
			}
			accessor.WriteRune(character)
		}
		segments[index] = accessor.String()
	}
	return strings.Join(segments, ".")
}

// readGradleFiles concatenates the named files that exist in the directory.
func readGradleFiles(directory string, names ...string) (string, error) {
	var contents strings.Builder
	for _, name := range names {
		fileContents, operationError := os.ReadFile(filepath.Join(directory, name))
		if errors.Is(operationError, os.ErrNotExist) {
			continue
		}
		if operationError != nil {
			return "", fmt.Errorf("read %s: %w", filepath.Join(directory, name), operationError)
		}
		contents.Write(fileContents)
		contents.WriteString("\n")
	}
	return contents.String(), nil
}

// gradleDefaultInputs are the files the built-in reads: the settings file
// and every included project's build file.
func gradleDefaultInputs(workspaceDirectory string) (inputs []string, excludeInputs []string) {
	inputs = []string{"settings.gradle", "settings.gradle.kts"}
	settings, operationError := readGradleFiles(workspaceDirectory, "settings.gradle.kts", "settings.gradle")
	if operationError != nil {
		return inputs, gradleExcludeInputs
	}
	for _, directory := range gradleProjectDirectories(workspaceDirectory, settings) {
		inputs = append(inputs, path.Join(directory, "build.gradle"), path.Join(directory, "build.gradle.kts"))
	}
	slices.Sort(inputs[2:])
	return inputs, gradleExcludeInputs
}
