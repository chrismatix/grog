// Command coveragereport renders a package-level coverage dashboard and badge
// from a Go coverage profile.
//
// Usage: go run ./tools/coveragereport <profile> <output-directory>
package main

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/tools/cover"
)

const (
	modulePrefix     = "github.com/chrismatix/grog/"
	thresholdPercent = 80.0
)

//go:embed templates
var templateFiles embed.FS

var templates = template.Must(template.New("").Funcs(template.FuncMap{
	"percent": func(percent float64) string { return fmt.Sprintf("%.1f%%", percent) },
	"level":   level,
	"base":    path.Base,
}).ParseFS(templateFiles, "templates/*"))

type summary struct {
	Covered int
	Total   int
}

func (coverage summary) Percent() float64 {
	return 100 * float64(coverage.Covered) / float64(coverage.Total)
}

func (coverage summary) Missed() int {
	return coverage.Total - coverage.Covered
}

func (coverage *summary) add(other summary) {
	coverage.Covered += other.Covered
	coverage.Total += other.Total
}

type fileCoverage struct {
	summary

	Name string
	// DetailsAnchor addresses the file in the `go tool cover -html` report,
	// which numbers files in the profile's sorted order.
	DetailsAnchor string
}

type packageCoverage struct {
	summary

	Name  string
	Files []fileCoverage
}

type report struct {
	summary

	Packages         []packageCoverage
	ThresholdPercent float64
	GeneratedAt      time.Time
}

func (coverageReport report) PackagesBelowThreshold() int {
	count := 0
	for _, packageSummary := range coverageReport.Packages {
		if packageSummary.Percent() < coverageReport.ThresholdPercent {
			count++
		}
	}
	return count
}

func level(percent float64) string {
	switch {
	case percent >= thresholdPercent:
		return "good"
	case percent >= 60:
		return "fair"
	default:
		return "poor"
	}
}

func main() {
	if len(os.Args) != 3 {
		log.Fatalf("usage: %s <profile> <output-directory>", filepath.Base(os.Args[0]))
	}
	profiles, err := cover.ParseProfiles(os.Args[1])
	if err != nil {
		log.Fatalf("failed to parse coverage profile: %v", err)
	}
	coverageReport := buildReport(profiles, time.Now().UTC())

	outputDirectory := os.Args[2]
	if err := writeTemplate(filepath.Join(outputDirectory, "coverage.html"), "coverage.html.tmpl", coverageReport); err != nil {
		log.Fatal(err)
	}
	if err := writeTemplate(filepath.Join(outputDirectory, "coverage.svg"), "coverage.svg.tmpl", badge(coverageReport.Percent())); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Total coverage: %.1f%%\n", coverageReport.Percent())
}

func buildReport(profiles []*cover.Profile, generatedAt time.Time) report {
	packagesByName := map[string]*packageCoverage{}
	for index, profile := range profiles {
		file := fileCoverage{
			Name:          strings.TrimPrefix(profile.FileName, modulePrefix),
			DetailsAnchor: fmt.Sprintf("file%d", index),
		}
		for _, block := range profile.Blocks {
			file.Total += block.NumStmt
			if block.Count > 0 {
				file.Covered += block.NumStmt
			}
		}

		packageName := path.Dir(file.Name)
		packageSummary, ok := packagesByName[packageName]
		if !ok {
			packageSummary = &packageCoverage{Name: packageName}
			packagesByName[packageName] = packageSummary
		}
		packageSummary.Files = append(packageSummary.Files, file)
		packageSummary.add(file.summary)
	}

	coverageReport := report{ThresholdPercent: thresholdPercent, GeneratedAt: generatedAt}
	for _, packageSummary := range packagesByName {
		sort.Slice(packageSummary.Files, func(left, right int) bool {
			return lowestFirst(packageSummary.Files[left].summary, packageSummary.Files[right].summary,
				packageSummary.Files[left].Name, packageSummary.Files[right].Name)
		})
		coverageReport.Packages = append(coverageReport.Packages, *packageSummary)
		coverageReport.add(packageSummary.summary)
	}
	sort.Slice(coverageReport.Packages, func(left, right int) bool {
		return lowestFirst(coverageReport.Packages[left].summary, coverageReport.Packages[right].summary,
			coverageReport.Packages[left].Name, coverageReport.Packages[right].Name)
	})
	return coverageReport
}

func lowestFirst(left, right summary, leftName, rightName string) bool {
	if left.Percent() != right.Percent() {
		return left.Percent() < right.Percent()
	}
	return leftName < rightName
}

type badgeData struct {
	Value       string
	Color       string
	LabelWidth  int
	ValueWidth  int
	TotalWidth  int
	LabelCenter float64
	ValueCenter float64
}

func badge(percent float64) badgeData {
	colors := map[string]string{"good": "#4c1", "fair": "#dfb317", "poor": "#e05d44"}
	value := fmt.Sprintf("%.1f%%", percent)
	labelWidth := 61
	valueWidth := 10 + 7*len(value)
	return badgeData{
		Value:       value,
		Color:       colors[level(percent)],
		LabelWidth:  labelWidth,
		ValueWidth:  valueWidth,
		TotalWidth:  labelWidth + valueWidth,
		LabelCenter: float64(labelWidth) / 2,
		ValueCenter: float64(labelWidth) + float64(valueWidth)/2,
	}
}

func writeTemplate(outputPath, templateName string, data any) error {
	var output bytes.Buffer
	if err := templates.ExecuteTemplate(&output, templateName, data); err != nil {
		return fmt.Errorf("failed to render %s: %w", outputPath, err)
	}
	return os.WriteFile(outputPath, output.Bytes(), 0o644)
}
