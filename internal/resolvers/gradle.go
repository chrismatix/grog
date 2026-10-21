package resolvers

import (
	"context"
	"errors"
	"fmt"
	"maps"
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
	gradleProjectDirPattern       = regexp.MustCompile(`\bproject\(\s*["'](:[^"']*)["']\s*\)\.projectDir\s*=\s*(?:new\s+)?[fF]ile\(\s*(?:(?:rootDir|settingsDir|rootProject\.projectDir)\s*,\s*)?["']([^"']+)["']`)
	gradleIncludeBuildPattern     = regexp.MustCompile(`\bincludeBuild\s*\(?\s*["']([^"']+)["']`)
	gradleProjectReferencePattern = regexp.MustCompile(`\bproject\(\s*(?:path\s*[:=]\s*)?["'](:[^"']*)["']`)
	gradleAccessorPattern         = regexp.MustCompile(`\bprojects((?:\.[A-Za-z_][A-Za-z0-9_]*)+)`)
	gradleBuildFileNamePattern    = regexp.MustCompile(`\b(?:project\(\s*["'](:[^"']*)["']\s*\)|(rootProject))\.buildFileName\s*=\s*["']([^"']+)["']`)
	gradleProjectNamePattern      = regexp.MustCompile(`\bproject\(\s*["'](:[^"']*)["']\s*\)\.name\s*=\s*["']([^:"'/]+)["']`)
)

var gradleExcludeInputs = []string{"build/**", ".gradle/**"}

// gradleDependencies reads the settings file for the project tree and every
// project's build file for project(":x") and projects.x references. Every
// project depends on the root, which owns the version catalogs and buildSrc.
func gradleDependencies(resolverContext context.Context, workspaceDirectory string) (Document, error) {
	document := Document{Version: 1, Packages: make(map[string]Package)}
	settings, operationError := readGradleFile(workspaceDirectory, "settings.gradle", "settings.gradle.kts")
	if operationError != nil {
		return document, operationError
	}
	if settings == "" {
		// Without a settings file Gradle runs a single-project build.
		rootBuildFile, operationError := readGradleFile(workspaceDirectory, gradleBuildFiles(settings, ":")...)
		if operationError != nil {
			return document, operationError
		}
		if rootBuildFile == "" {
			return document, fmt.Errorf("no settings.gradle, settings.gradle.kts or build file in %s", workspaceDirectory)
		}
	}
	settings = gradleWithoutComments(settings)
	directoryByProject := gradleProjectDirectories(settings)
	accessorByProject := make(map[string]string, len(directoryByProject))
	for projectPath := range directoryByProject {
		accessorByProject[gradleAccessor(projectPath)] = projectPath
	}

	root := Package{
		Dependencies:  []string{},
		Inputs:        slices.Concat([]string{"settings.gradle", "settings.gradle.kts", "gradle.properties", "gradle/**", "buildSrc/**", "src/**/*"}, gradleBuildFiles(settings, ":")),
		ExcludeInputs: []string{"build/**", ".gradle/**", "buildSrc/build/**", "buildSrc/.gradle/**"},
	}
	for _, match := range gradleIncludeBuildPattern.FindAllStringSubmatch(settings, -1) {
		includedBuild := path.Clean(filepath.ToSlash(match[1]))
		if ValidatePath(includedBuild) != nil || includedBuild == "." {
			continue
		}
		root.Dependencies = append(root.Dependencies, includedBuild)
		document.Packages[includedBuild] = Package{Dependencies: []string{}, Inputs: []string{"**/*"}, ExcludeInputs: []string{"**/build/**", "**/.gradle/**"}}
	}
	slices.Sort(root.Dependencies)
	root.Dependencies = slices.Compact(root.Dependencies)
	slices.Sort(root.Inputs)
	document.Packages[""] = root

	for projectPath, directory := range directoryByProject {
		if operationError := resolverContext.Err(); operationError != nil {
			return document, operationError
		}
		buildFile, operationError := readGradleFile(filepath.Join(workspaceDirectory, directory), gradleBuildFiles(settings, projectPath)...)
		if operationError != nil {
			return document, operationError
		}
		reportedPackage := Package{
			Dependencies:  []string{""},
			Inputs:        slices.Concat(gradleBuildFiles(settings, projectPath), []string{"gradle.properties", "src/**/*"}),
			ExcludeInputs: gradleExcludeInputs,
		}
		for _, referenced := range gradleProjectReferences(buildFile, directoryByProject, accessorByProject) {
			if referenced != projectPath {
				reportedPackage.Dependencies = append(reportedPackage.Dependencies, directoryByProject[referenced])
			}
		}
		slices.Sort(reportedPackage.Dependencies)
		reportedPackage.Dependencies = slices.Compact(reportedPackage.Dependencies)
		slices.Sort(reportedPackage.Inputs)
		document.Packages[directory] = reportedPackage
	}
	return document, nil
}

// gradleProjectDirectories maps every included project path (":a:b") to its
// directory, honouring projectDir overrides. The root project is left out.
// Including ":a:b" also creates ":a"; it is kept when its directory exists.
func gradleProjectDirectories(settings string) map[string]string {
	settings = gradleWithoutComments(settings)
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
			directories[parent] = strings.ReplaceAll(strings.TrimPrefix(parent, ":"), ":", "/")
		}
	}
	for _, match := range gradleProjectDirPattern.FindAllStringSubmatch(settings, -1) {
		isAncestor := slices.ContainsFunc(slices.Collect(maps.Keys(directories)), func(projectPath string) bool { return strings.HasPrefix(projectPath, match[1]+":") })
		if _, included := directories[match[1]]; included || isAncestor {
			directories[match[1]] = path.Clean(filepath.ToSlash(match[2]))
		}
	}
	for _, match := range gradleProjectNamePattern.FindAllStringSubmatch(settings, -1) {
		if _, included := directories[match[1]]; !included {
			continue
		}
		parentPath := match[1][:strings.LastIndex(match[1], ":")]
		newPrefix := parentPath + ":" + match[2]
		for _, projectPath := range slices.Sorted(maps.Keys(directories)) {
			if projectPath != match[1] && !strings.HasPrefix(projectPath, match[1]+":") {
				continue
			}
			directories[newPrefix+strings.TrimPrefix(projectPath, match[1])] = directories[projectPath]
			delete(directories, projectPath)
		}
	}
	for projectPath, directory := range directories {
		if ValidatePath(directory) != nil || directory == "." {
			delete(directories, projectPath)
		}
	}
	return directories
}

func gradleWithoutComments(contents string) string {
	var result strings.Builder
	result.Grow(len(contents))
	var quote byte
	lineComment := false
	blockComment := false
	escaped := false
	for index := 0; index < len(contents); index++ {
		character := contents[index]
		if lineComment {
			if character == '\n' {
				lineComment = false
				result.WriteByte(character)
			} else {
				result.WriteByte(' ')
			}
			continue
		}
		if blockComment {
			if character == '*' && index+1 < len(contents) && contents[index+1] == '/' {
				result.WriteString("  ")
				index++
				blockComment = false
			} else if character == '\n' {
				result.WriteByte(character)
			} else {
				result.WriteByte(' ')
			}
			continue
		}
		if quote != 0 {
			result.WriteByte(character)
			if escaped {
				escaped = false
			} else if character == '\\' {
				escaped = true
			} else if character == quote {
				quote = 0
			}
			continue
		}
		if character == '\'' || character == '"' {
			quote = character
			result.WriteByte(character)
			continue
		}
		if character == '/' && index+1 < len(contents) {
			switch contents[index+1] {
			case '/':
				result.WriteString("  ")
				index++
				lineComment = true
				continue
			case '*':
				result.WriteString("  ")
				index++
				blockComment = true
				continue
			}
		}
		result.WriteByte(character)
	}
	return result.String()
}

// gradleBuildFiles is the build file to read for a project, ":" for the
// root: the last buildFileName settings assign to it, or the defaults.
func gradleBuildFiles(settings string, projectPath string) []string {
	buildFiles := []string{"build.gradle", "build.gradle.kts"}
	for _, match := range gradleBuildFileNamePattern.FindAllStringSubmatch(settings, -1) {
		if match[1] == projectPath || (match[2] != "" && projectPath == ":") {
			buildFiles = []string{match[3]}
		}
	}
	return buildFiles
}

// gradleProjectReferences lists the project paths a build file mentions,
// through project(":x") or the type-safe accessor projects.x. An accessor
// chain may continue into a property, so its longest known prefix wins.
func gradleProjectReferences(buildFile string, directoryByProject map[string]string, accessorByProject map[string]string) []string {
	buildFile = gradleWithoutTestDependencies(gradleWithoutComments(buildFile))
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

func gradleWithoutTestDependencies(contents string) string {
	result := []byte(contents)
	for index := 0; index < len(contents); index++ {
		if !unicode.IsLetter(rune(contents[index])) && contents[index] != '_' {
			continue
		}
		start := index
		for index < len(contents) && (unicode.IsLetter(rune(contents[index])) || unicode.IsDigit(rune(contents[index])) || contents[index] == '_') {
			index++
		}
		name := contents[start:index]
		if !gradleTestConfiguration(name) {
			index--
			continue
		}
		for index < len(contents) && (contents[index] == ' ' || contents[index] == '\t') {
			index++
		}
		if index >= len(contents) || (contents[index] != '(' && !strings.HasPrefix(contents[index:], "project")) {
			index--
			continue
		}
		var end int
		if index < len(contents) && contents[index] == '(' {
			end = gradleCallEnd(contents, index)
		} else if newline := strings.IndexByte(contents[index:], '\n'); newline >= 0 {
			end = index + newline
		} else {
			end = len(contents)
		}
		for position := start; position < end; position++ {
			if result[position] != '\n' {
				result[position] = ' '
			}
		}
		index = end - 1
	}
	return string(result)
}

func gradleTestConfiguration(name string) bool {
	lowerName := strings.ToLower(name)
	if !strings.Contains(lowerName, "test") {
		return false
	}
	return slices.ContainsFunc([]string{"implementation", "api", "compileonly", "runtimeonly", "annotationprocessor"}, func(suffix string) bool {
		return strings.HasSuffix(lowerName, suffix)
	})
}

func gradleCallEnd(contents string, openParenthesis int) int {
	depth := 0
	var quote byte
	escaped := false
	for index := openParenthesis; index < len(contents); index++ {
		character := contents[index]
		if quote != 0 {
			if escaped {
				escaped = false
			} else if character == '\\' {
				escaped = true
			} else if character == quote {
				quote = 0
			}
			continue
		}
		if character == '\'' || character == '"' {
			quote = character
			continue
		}
		switch character {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return index + 1
			}
		}
	}
	return len(contents)
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

// readGradleFile reads the first named script that exists.
func readGradleFile(directory string, names ...string) (string, error) {
	for _, name := range names {
		fileContents, operationError := os.ReadFile(filepath.Join(directory, name))
		if errors.Is(operationError, os.ErrNotExist) {
			continue
		}
		if operationError != nil {
			return "", fmt.Errorf("read %s: %w", filepath.Join(directory, name), operationError)
		}
		return string(fileContents), nil
	}
	return "", nil
}

// gradleDefaultInputs are the files the built-in reads: the settings file
// and every included project's build file.
func gradleDefaultInputs(_ context.Context, workspaceDirectory string) (inputs []string, excludeInputs []string) {
	inputs = []string{"settings.gradle", "settings.gradle.kts"}
	settings, operationError := readGradleFile(workspaceDirectory, "settings.gradle", "settings.gradle.kts")
	if operationError != nil {
		return inputs, gradleExcludeInputs
	}
	settings = gradleWithoutComments(settings)
	inputs = append(inputs, gradleBuildFiles(settings, ":")...)
	for projectPath, directory := range gradleProjectDirectories(settings) {
		for _, buildFile := range gradleBuildFiles(settings, projectPath) {
			inputs = append(inputs, path.Join(directory, buildFile))
		}
	}
	slices.Sort(inputs[2:])
	inputs = slices.Compact(inputs)
	return inputs, gradleExcludeInputs
}
