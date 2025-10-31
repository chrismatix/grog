package loading

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"grog/internal/config"
	"grog/internal/model"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

type ScriptLoader struct{}

func (ScriptLoader) Matches(fileName string) bool {
	return strings.HasSuffix(fileName, ".grog.sh") || strings.HasSuffix(fileName, ".grog.py")
}

func (ScriptLoader) Load(_ context.Context, filePath string) (PackageDTO, bool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return PackageDTO{}, false, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	parser := newScriptParser(scanner, filePath)
	return parser.parse()
}

type scriptParser struct {
	scanner *bufio.Scanner
	file    string
}

func newScriptParser(scanner *bufio.Scanner, file string) *scriptParser {
	return &scriptParser{scanner: scanner, file: file}
}

func (p *scriptParser) parse() (PackageDTO, bool, error) {
	lineCount := 0
	var annotation scriptAnnotation

	for p.scanner.Scan() {
		lineCount++
		line := p.scanner.Text()
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "# @grog") {

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
					// Remove '#'
					content := trimmedNext[1:]
					annotationLines = append(annotationLines, content)
					annotationLineNumbers = append(annotationLineNumbers, lineCount)
				} else {
					if len(annotationLines) == 0 {
						// This means that there is just an empty # @grog start block with no content
						// This is fine
						break
					}

					// End of annotation: this should be the target definition.
					foundAnnotation, err := p.handleTarget(annotationLines, annotationLineNumbers)
					if err != nil {
						return PackageDTO{}, true, err
					}
					annotation = foundAnnotation
					// Break out of the loop.
					break
				}
			}
		}
	}

	if err := p.scanner.Err(); err != nil {
		return PackageDTO{}, false, fmt.Errorf("failed to scan %s: %w", p.file, err)
	}

	scriptFileName := filepath.Base(p.file)
	targetName := annotation.Name
	if targetName == "" {
		// Default to the script file name as the target name
		targetName = scriptFileName
	}

	target := &TargetDTO{
		Name:         targetName,
		Dependencies: annotation.Dependencies,
		// Prepend the script file name to the inputs to ensure
		// that changing it always invalidates it as a target
		Inputs: prependUnique(annotation.Inputs, scriptFileName),
		// Script files should never be cached
		Tags:                 prependUnique(annotation.Tags, "no-cache"),
		Fingerprint:          annotation.Fingerprint,
		EnvironmentVariables: annotation.EnvironmentVariables,
		Timeout:              annotation.Timeout,
		Platform:             annotation.Platform,
	}

	// Script files are always their own bin output
	target.BinOutput = scriptFileName

	pkg := PackageDTO{
		SourceFilePath: p.file,
		Targets:        []*TargetDTO{target},
	}

	return pkg, true, nil
}

// handleTarget parses the collected annotation lines and the subsequent target definition.
func (p *scriptParser) handleTarget(
	annotationLines []string,
	annotationLineNumbers []int,
) (scriptAnnotation, error) {
	// Combine annotation lines into a YAML snippet.
	annotationContent := strings.Join(annotationLines, "\n")
	lastLineNum := annotationLineNumbers[len(annotationLineNumbers)-1]

	var annotation scriptAnnotation
	if len(annotationContent) > 0 {
		if err := yaml.Unmarshal([]byte(annotationContent), &annotation); err != nil {
			return scriptAnnotation{}, fmt.Errorf("failed to parse annotation block L%d-%d: %w", annotationLineNumbers[0], lastLineNum, err)
		}
	}

	return annotation, nil
}

func prependUnique(values []string, element string) []string {
	for _, existing := range values {
		if existing == element {
			return append([]string{}, values...)
		}
	}
	return append([]string{element}, values...)
}

func LoadScriptTarget(ctx context.Context, logger *zap.SugaredLogger, filePath string) (*model.Target, error) {
	packagePath, err := config.GetPackagePath(filePath)
	if err != nil {
		return nil, err
	}

	loader := ScriptLoader{}
	pkgDto, matched, err := loader.Load(ctx, filePath)
	if err != nil {
		return nil, err
	}
	if !matched {
		relativePath, relErr := config.GetPathRelativeToWorkspaceRoot(filePath)
		if relErr == nil {
			filePath = relativePath
		}
		return nil, fmt.Errorf("%s does not contain a # @grog annotation", filePath)
	}

	pkg, err := getEnrichedPackage(logger, packagePath, pkgDto)
	if err != nil {
		return nil, err
	}

	for _, target := range pkg.Targets {
		return target, nil
	}

	return nil, fmt.Errorf("no target could be derived from %s", filePath)
}
