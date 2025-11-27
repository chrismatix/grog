package console

import (
	"fmt"
	"time"
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

func (p Progress) shouldRender() bool {
	return p.hasTotal() && time.Since(time.Unix(p.StartedAtSec, 0)).Seconds() > RenderAfterSeconds
}

func formatProgressBar(p Progress, width int) string {
	if !p.hasTotal() || width <= 0 {
		return ""
	}

	percent := p.percent()
	filled := (percent * width) / 100

	bar := make([]rune, width)
	for i := 0; i < width; i++ {
		if i < filled {
			bar[i] = '='
		} else if i == filled {
			bar[i] = '>'
		} else {
			bar[i] = ' '
		}
	}

	return fmt.Sprintf("[%s] %3d%%", string(bar), percent)
}
