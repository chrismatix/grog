package loading

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// MakefileLoader implements the Loader interface for Makefiles.
type MakefileLoader struct{}

func (MakefileLoader) Matches(fileName string) bool {
	return isPackageFileFormat(fileName, makePackageFile)
}

// Load reads the Makefile at filePath parses it to PackageDTO.
func (loader MakefileLoader) Load(ctx context.Context, filePath string) (PackageDTO, bool, error) {
	source, operationError := os.ReadFile(filePath)
	if operationError != nil {
		return PackageDTO{}, false, operationError
	}
	return loader.loadSource(ctx, filePath, source)
}

func (MakefileLoader) loadSource(_ context.Context, filePath string, source []byte) (PackageDTO, bool, error) {
	scanner := bufio.NewScanner(bytes.NewReader(source))
	parser := newMakefileParser(scanner)
	packageDTO, targetsFound, operationError := parser.parse()
	if operationError != nil {
		return packageDTO, targetsFound, fmt.Errorf(
			"failed to parse Makefile %s: %w",
			filePath,
			operationError)
	}

	return packageDTO, targetsFound, nil
}

// makefileParser encapsulates the scanning logic and state.
type makefileParser struct {
	scanner *bufio.Scanner
	pkg     PackageDTO
}

// newMakefileParser creates a new parser for the given scanner and file path.
func newMakefileParser(scanner *bufio.Scanner) *makefileParser {
	return &makefileParser{
		scanner: scanner,
		pkg: PackageDTO{
			Targets: make([]*TargetDTO, 0),
		},
	}
}

// parse iterates through the file line by line, handling annotations and targets.
// returns the parsed PackageDTO and a bool indicating if targets were found at all.
func (p *makefileParser) parse() (PackageDTO, bool, error) {
	targetsFound := false
	lineCount := 0

	for p.scanner.Scan() {
		lineCount++
		line := p.scanner.Text()
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "# @grog") {
			targetsFound = true

			// Collect the subsequent comment lines.
			var annotationLines []string
			var annotationLineNumbers []int

			for p.scanner.Scan() {
				lineCount++
				nextLine := p.scanner.Text()
				trimmedNext := strings.TrimSpace(nextLine)
				if trimmedNext == "" {
					continue
				}
				if strings.HasPrefix(trimmedNext, "#") {
					// Remove '#' and any whitespace.
					content := trimmedNext[1:]
					annotationLines = append(annotationLines, content)
					annotationLineNumbers = append(annotationLineNumbers, lineCount)
				} else {
					// End of annotation: this should be the target definition.
					if err := p.handleTarget(annotationLines, annotationLineNumbers, nextLine, lineCount); err != nil {
						return p.pkg, targetsFound, err
					}
					// Break out of the loop.
					break
				}
			}
		}
	}

	return p.pkg, targetsFound, p.scanner.Err()
}

// handleTarget parses the collected annotation lines and the subsequent target definition.
func (p *makefileParser) handleTarget(
	annotationLines []string,
	annotationLineNumbers []int,
	targetLine string,
	targetLineNumber int,
) error {
	// Combine annotation lines into a YAML snippet.
	annotationContent := strings.Join(annotationLines, "\n")
	firstLineNum, lastLineNum := annotationLineRange(annotationLineNumbers)

	var annotation grogAnnotation
	if len(annotationContent) > 0 {
		if err := yaml.Unmarshal([]byte(annotationContent), &annotation); err != nil {
			return fmt.Errorf("failed to parse annotation block L%d-%d: %w", firstLineNum, lastLineNum, err)
		}
	}

	// Process the target definition.
	trimmedTarget := strings.TrimSpace(targetLine)
	if !strings.Contains(trimmedTarget, ":") {
		return fmt.Errorf("expected a make target definition in L%d ending with ':', got: %s", targetLineNumber, trimmedTarget)
	}
	// Extract the target name (remove the trailing colon).
	targetName := strings.Split(trimmedTarget, ":")[0]

	// Create the TargetDTO.
	target := &TargetDTO{
		Name:         targetName,
		Command:      "make " + targetName,
		Dependencies: annotation.Dependencies,
		Inputs:       annotation.Inputs,
		Outputs:      annotation.Outputs,
		OciPush:      annotation.OciPush,
		Tags:         annotation.Tags,
	}

	// Use the annotation's name as key if provided, otherwise use the target name.
	if annotation.Name != "" {
		target.Name = annotation.Name
	}

	p.pkg.Targets = append(p.pkg.Targets, target)
	return nil
}
