package handlers

import (
	"sort"
	"sync"
)

// PushReport captures the outcome of a single oci-push:: push attempt. It is
// recorded by the push write plan after Execute() resolves, win or lose, and
// read by the build command at end-of-build to render a per-push summary
// and decide the process exit code.
//
// Skipped is set when the destination probe found the image already at the
// requested digest (idempotent re-run); the push performed no transfer. A
// skipped push is a success.
type PushReport struct {
	TargetLabel string
	Destination string
	Skipped     bool
	Err         error
}

// PushReporter accumulates PushReport entries from concurrent push plans. It
// is owned by the output Registry so a single reporter spans every push in a
// build invocation. Reads via Reports() return a stable, label-sorted copy
// suitable for rendering and exit-code decisions.
type PushReporter struct {
	mu      sync.Mutex
	entries []PushReport
}

func NewPushReporter() *PushReporter {
	return &PushReporter{}
}

// Record appends a report. Safe for concurrent callers.
func (p *PushReporter) Record(report PushReport) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.entries = append(p.entries, report)
}

// Reports returns a sorted copy of the recorded reports. Sorting by (target
// label, destination) keeps the build summary deterministic regardless of
// async push ordering.
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

// HasFailures reports whether any recorded push errored. Used by the build
// command to decide the process exit code: any push failure causes a
// non-zero exit even if the build itself succeeded.
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
