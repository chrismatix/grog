package loading

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
	"grog/internal/config"
	"grog/internal/label"
	"grog/internal/model"

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

type scriptAnnotation struct {
	Name                 string                `yaml:"name"`
	Dependencies         []string              `yaml:"dependencies"`
	Inputs               []string              `yaml:"inputs"`
	ExcludeInputs        []string              `yaml:"exclude_inputs"`
	Outputs              []string              `yaml:"outputs"`
	BinOutput            string                `yaml:"bin_output"`
	Tags                 []string              `yaml:"tags"`
	Fingerprint          map[string]string     `yaml:"fingerprint"`
	EnvironmentVariables map[string]string     `yaml:"environment_variables"`
	Timeout              string                `yaml:"timeout"`
	Platform             *model.PlatformConfig `yaml:"platform"`
	OutputChecks         []model.OutputCheck   `yaml:"output_checks"`
}

func (p *scriptParser) parse() (PackageDTO, bool, error) {
	lineNumber := 0
	foundAnnotation := false
	var annotationLines []string
	var annotationLineNumbers []int

	for p.scanner.Scan() {
		lineNumber++
		line := p.scanner.Text()
		trimmed := strings.TrimSpace(line)

		if !foundAnnotation {
			if trimmed == "" {
				continue
			}
			if strings.HasPrefix(trimmed, "#!") {
				// allow shebangs before the annotation block
				continue
			}
			if strings.HasPrefix(trimmed, "# @grog") {
				foundAnnotation = true
				continue
			}
			// The first non-empty, non-shebang line was not an annotation.
			break
		}

		if trimmed == "" {
			break
		}

		if strings.HasPrefix(trimmed, "#") {
			content := strings.TrimSpace(trimmed[1:])
			annotationLines = append(annotationLines, content)
			annotationLineNumbers = append(annotationLineNumbers, lineNumber)
			continue
		}

		break
	}

	if err := p.scanner.Err(); err != nil {
		return PackageDTO{}, false, fmt.Errorf("failed to scan %s: %w", p.file, err)
	}

	if !foundAnnotation {
		return PackageDTO{}, false, nil
	}

	var annotation scriptAnnotation
	if len(annotationLines) > 0 {
		content := strings.Join(annotationLines, "\n")
		if err := yaml.Unmarshal([]byte(content), &annotation); err != nil {
			firstLine := 0
			lastLine := 0
			if len(annotationLineNumbers) > 0 {
				firstLine = annotationLineNumbers[0]
				lastLine = annotationLineNumbers[len(annotationLineNumbers)-1]
			}
			if firstLine == 0 {
				return PackageDTO{}, false, fmt.Errorf("failed to parse annotation in %s: %w", p.file, err)
			}
			return PackageDTO{}, false, fmt.Errorf("failed to parse annotation in %s L%d-%d: %w", p.file, firstLine, lastLine, err)
		}
	}

	defaultName := filepath.Base(p.file)
	targetName := annotation.Name
	if targetName == "" {
		targetName = defaultName
	}

	target := &TargetDTO{
		Name:                 targetName,
		Dependencies:         annotation.Dependencies,
		Inputs:               prependUnique(annotation.Inputs, defaultName),
		ExcludeInputs:        annotation.ExcludeInputs,
		Outputs:              annotation.Outputs,
		BinOutput:            annotation.BinOutput,
		Tags:                 annotation.Tags,
		Fingerprint:          annotation.Fingerprint,
		EnvironmentVariables: annotation.EnvironmentVariables,
		Timeout:              annotation.Timeout,
		Platform:             annotation.Platform,
		OutputChecks:         annotation.OutputChecks,
	}

	if target.BinOutput == "" {
		target.BinOutput = defaultName
	}

	pkg := PackageDTO{
		SourceFilePath: p.file,
		Targets:        []*TargetDTO{target},
	}

	return pkg, true, nil
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

func mergePackages(into *model.Package, from *model.Package) error {
	if into.Targets == nil {
		into.Targets = make(map[label.TargetLabel]*model.Target)
	}
	if into.Aliases == nil {
		into.Aliases = make(map[label.TargetLabel]*model.Alias)
	}

	for lbl, target := range from.Targets {
		if _, exists := into.Targets[lbl]; exists {
			return fmt.Errorf("duplicate target label: %s (defined in %s and %s)", lbl, into.SourceFilePath, from.SourceFilePath)
		}
		into.Targets[lbl] = target
	}

	for lbl, alias := range from.Aliases {
		if _, exists := into.Targets[lbl]; exists {
			return fmt.Errorf("duplicate target label: %s (defined in %s and %s)", lbl, into.SourceFilePath, from.SourceFilePath)
		}
		if _, exists := into.Aliases[lbl]; exists {
			return fmt.Errorf("duplicate alias label: %s (defined in %s and %s)", lbl, into.SourceFilePath, from.SourceFilePath)
		}
		into.Aliases[lbl] = alias
	}

	return nil
}
