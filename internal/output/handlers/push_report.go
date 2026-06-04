package handlers

import (
	"sort"
	"sync"
	"sync/atomic"

	"grog/internal/console"
)

// PushReport captures the outcome of a single oci-push:: push attempt.
// Skipped means the HEAD probe matched and no bytes were sent.
type PushReport struct {
	TargetLabel string
	Destination string
	Skipped     bool
	Err         error
}

// PushReporter accumulates PushReport entries and carries the build-level
// abort flag that --fail-fast trips. One instance per build, owned by the
// output Registry and shared across all docker handlers.
type PushReporter struct {
	failFast func() bool

	mu      sync.Mutex
	entries []PushReport

	aborted atomic.Bool
}

func NewPushReporter(failFast func() bool) *PushReporter {
	if failFast == nil {
		failFast = func() bool { return false }
	}
	return &PushReporter{failFast: failFast}
}

func (p *PushReporter) Record(report PushReport) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.entries = append(p.entries, report)
	p.mu.Unlock()
	if report.Err != nil && p.failFast() {
		p.aborted.Store(true)
	}
}

// Aborted reports whether a prior fail-fast failure has tripped the abort.
// Push plans check this before issuing a copy.
func (p *PushReporter) Aborted() bool {
	if p == nil {
		return false
	}
	return p.aborted.Load()
}

func (p *PushReporter) Reports() []PushReport {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]PushReport, len(p.entries))
	copy(out, p.entries)
	sort.Slice(out, func(i, j int) bool {
		if out[i].TargetLabel != out[j].TargetLabel {
			return out[i].TargetLabel < out[j].TargetLabel
		}
		return out[i].Destination < out[j].Destination
	})
	return out
}

func (p *PushReporter) HasFailures() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, r := range p.entries {
		if r.Err != nil {
			return true
		}
	}
	return false
}

// RenderSummary logs the per-push counts and per-entry detail and reports
// whether any push failed. Nil/empty reporter renders nothing.
func (p *PushReporter) RenderSummary(logger *console.Logger) bool {
	if p == nil {
		return false
	}
	reports := p.Reports()
	if len(reports) == 0 {
		return false
	}

	var pushed, skipped, failed int
	for _, r := range reports {
		switch {
		case r.Err != nil:
			failed++
		case r.Skipped:
			skipped++
		default:
			pushed++
		}
	}

	logger.Infof("Pushes: %d pushed, %d already current, %d failed.", pushed, skipped, failed)
	for _, r := range reports {
		switch {
		case r.Err != nil:
			logger.Errorf("  FAILED %s -> %s: %v", r.TargetLabel, r.Destination, r.Err)
		case r.Skipped:
			logger.Infof("  CURRENT %s -> %s", r.TargetLabel, r.Destination)
		default:
			logger.Infof("  PUSHED %s -> %s", r.TargetLabel, r.Destination)
		}
	}
	return failed > 0
}
