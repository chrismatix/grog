package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/tools/cover"
)

func TestBuildReport(t *testing.T) {
	profilePath := filepath.Join(t.TempDir(), "coverage.out")
	profile := `mode: set
github.com/chrismatix/grog/internal/alpha/a.go:1.1,2.1 3 1
github.com/chrismatix/grog/internal/alpha/a.go:3.1,4.1 1 0
github.com/chrismatix/grog/internal/alpha/b.go:1.1,2.1 4 0
github.com/chrismatix/grog/internal/beta/c.go:1.1,2.1 2 1
`
	if err := os.WriteFile(profilePath, []byte(profile), 0o644); err != nil {
		t.Fatal(err)
	}
	profiles, err := cover.ParseProfiles(profilePath)
	if err != nil {
		t.Fatal(err)
	}

	coverageReport := buildReport(profiles, time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC))

	if coverageReport.Covered != 5 || coverageReport.Total != 10 {
		t.Fatalf("total = %d/%d, want 5/10", coverageReport.Covered, coverageReport.Total)
	}
	if coverageReport.PackagesBelowThreshold() != 1 {
		t.Fatalf("packages below threshold = %d, want 1", coverageReport.PackagesBelowThreshold())
	}

	alpha := coverageReport.Packages[0]
	if alpha.Name != "internal/alpha" || alpha.Covered != 3 || alpha.Total != 8 {
		t.Fatalf("first package = %s %d/%d, want internal/alpha 3/8", alpha.Name, alpha.Covered, alpha.Total)
	}
	if alpha.Files[0].Name != "internal/alpha/b.go" || alpha.Files[0].DetailsAnchor != "file1" {
		t.Fatalf("first file = %s#%s, want internal/alpha/b.go#file1", alpha.Files[0].Name, alpha.Files[0].DetailsAnchor)
	}

	var page strings.Builder
	if err := templates.ExecuteTemplate(&page, "coverage.html.tmpl", coverageReport); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"50.0%", "coverage-details.html#file1", "internal/beta"} {
		if !strings.Contains(page.String(), want) {
			t.Errorf("page does not contain %q", want)
		}
	}
}

func TestBadge(t *testing.T) {
	tests := []struct {
		percent float64
		color   string
	}{
		{percent: 87.4, color: "#4c1"},
		{percent: 65, color: "#dfb317"},
		{percent: 12.5, color: "#e05d44"},
	}
	for _, test := range tests {
		t.Run(test.color, func(t *testing.T) {
			if color := badge(test.percent).Color; color != test.color {
				t.Fatalf("badge(%v).Color = %s, want %s", test.percent, color, test.color)
			}
		})
	}
}
