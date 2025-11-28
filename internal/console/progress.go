package console

import (
	"fmt"
	"strings"
	"time"

	"github.com/lucasb-eyer/go-colorful"
	"github.com/muesli/termenv"
)

// RenderAfterSeconds determines how long to wait before rendering progress bars.
const RenderAfterSeconds = 1

// Progress represents a unit of work with a current and total value.
// It is used to render progress bars in the task UI.
type Progress struct {
	StartedAtSec int64
	Current      int64
	Total        int64
}

// percent clamps the progress to the 0-100 range.
func (p Progress) percent() int {
	if p.Total <= 0 {
		return 0
	}

	if p.Current >= p.Total {
		return 100
	}

	if p.Current <= 0 {
		return 0
	}

	return int((p.Current * 100) / p.Total)
}

func (p Progress) hasTotal() bool {
	return p.Total > 0
}

func (p Progress) isComplete() bool {
	return p.Current >= p.Total
}

func (p Progress) shouldRender() bool {
	return p.hasTotal() && time.Since(time.Unix(p.StartedAtSec, 0)).Seconds() > RenderAfterSeconds && !p.isComplete()
}

func formatProgressBar(p Progress, width int) string {
	if !p.hasTotal() || width <= 0 {
		return ""
	}

	percent := p.percent()
	filled := (percent * width) / 100

	// light green to dark green gradient
	startColor, _ := colorful.Hex("#3CCF6D")
	endColor, _ := colorful.Hex("#0A8F3A")
	cp := termenv.ColorProfile()

	var b strings.Builder
	for i := 0; i < width; i++ {
		if i < filled {
			ratio := 0.0
			if width > 1 {
				ratio = float64(i) / float64(width-1)
			}
			c := startColor.BlendLuv(endColor, ratio).Hex()
			b.WriteString(termenv.String("=").Foreground(cp.Color(c)).String())
		} else if i == filled {
			ratio := 0.0
			if width > 1 {
				ratio = float64(i) / float64(width-1)
			}
			c := startColor.BlendLuv(endColor, ratio).Hex()
			b.WriteString(termenv.String(">").Foreground(cp.Color(c)).String())
		} else {
			b.WriteString(" ")
		}
	}
	return fmt.Sprintf("[%s] %3d%% %s/%s", b.String(), percent, formatBytes(p.Current), formatBytes(p.Total))
}

// formatBytes renders a human-readable byte count for progress bars.
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}

	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit && exp < len("KMGTPE")-1; n /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
