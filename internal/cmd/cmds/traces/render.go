package traces

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"

	"grog/internal/console"
)

// styled returns true if lipgloss rendering should be used.
func styled() bool {
	return console.UseTea()
}

var (
	headerStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	sectionStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")).MarginTop(1)
	hintStyle       = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("241"))
	impactHighStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196")) // red
	impactMedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // orange
	impactLowStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("226")) // yellow
	labelStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("75"))
	dimStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	statsTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	statsLabelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("248")).Width(16)
	statsValueStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	statsGoodStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))  // green
	statsWarnStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214")) // orange
	statsBadStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")) // red
)

func renderImpact(impact float64) string {
	bar := fmt.Sprintf("%.0fms", impact)
	if !styled() {
		return bar
	}
	if impact > 10000 {
		return impactHighStyle.Render(bar)
	} else if impact > 3000 {
		return impactMedStyle.Render(bar)
	}
	return impactLowStyle.Render(bar)
}

func renderSection(title string) string {
	if styled() {
		return sectionStyle.Render(title)
	}
	return "\n" + title
}

func renderHint(text string) string {
	if styled() {
		return hintStyle.Render("  " + text)
	}
	return "  " + text
}

func renderLabel(label string) string {
	if styled() {
		return labelStyle.Render(label)
	}
	return label
}

func renderDim(text string) string {
	if styled() {
		return dimStyle.Render(text)
	}
	return text
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

func formatMillis(ms int64) string {
	return formatDuration(time.Duration(ms) * time.Millisecond)
}
