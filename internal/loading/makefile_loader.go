package loading

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// MakefileLoader implements the Loader interface for Makefiles.
type MakefileLoader struct{}

func (m MakefileLoader) Matches(fileName string) bool {
	return fileName == "Makefile"
}

// Load reads the Makefile at filePath parses it to PackageDTO
func (m MakefileLoader) Load(_ context.Context, filePath string) (PackageDTO, bool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return PackageDTO{}, false, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	parser := newMakefileParser(scanner)
	packageDto, targetsFound, err := parser.parse()
	if err != nil {
		return packageDto, targetsFound, fmt.Errorf(
			"failed to parse Makefile %s: %w",
			filePath,
			err)
	}

	return packageDto, targetsFound, nil
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
// returns the parsed PackageDTO and a bool indicating if targets were found at all
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
					content := strings.TrimSpace(trimmedNext[1:])
					annotationLines = append(annotationLines, content)
					annotationLineNumbers = append(annotationLineNumbers, lineCount)
				} else {
					// End of annotation: this should be the target definition.
					if err := p.handleTarget(annotationLines, annotationLineNumbers, nextLine); err != nil {
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
) error {
	// Combine annotation lines into a YAML snippet.
	annotationContent := strings.Join(annotationLines, "\n")
	lastLineNum := annotationLineNumbers[len(annotationLineNumbers)-1]

	var annotation grogAnnotation
	if len(annotationContent) > 0 {
		if err := yaml.Unmarshal([]byte(annotationContent), &annotation); err != nil {
			return fmt.Errorf("failed to parse annotation block L%d-%d: %w", annotationLineNumbers[0], lastLineNum, err)
		}
	}

	// Process the target definition.
	trimmedTarget := strings.TrimSpace(targetLine)
	if !strings.Contains(trimmedTarget, ":") {
		return fmt.Errorf("expected a make target definition in L%d ending with ':', got: %s", lastLineNum+1, trimmedTarget)
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
		Tags:         annotation.Tags,
	}

	// Use the annotation's name as key if provided, otherwise use the target name.
	if annotation.Name != "" {
		target.Name = annotation.Name
	}

	p.pkg.Targets = append(p.pkg.Targets, target)
	return nil
}
