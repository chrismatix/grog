package worker

import (
	"io"
	"sync"
	"time"

	"grog/internal/config"
	"grog/internal/console"
)

// ProgressTracker aggregates progress updates and throttles UI refreshes.
// The tracker is safe for concurrent use across goroutines working on the
// same logical unit of work. Progress can be subdivided into child trackers
// when multiple operations contribute to the same overall status.
type ProgressTracker struct {
	status string
	update StatusFunc

	// statusSet is flipped to true the first time a caller explicitly calls
	// SetStatus on this tracker. Once set, statusForChildStatusLocked always
	// returns pt.status — even when there is exactly one child — so phase
	// summaries and other caller-provided labels aren't masked by a single
	// child inheriting the base status. Before any SetStatus call, the
	// default "single child inherits" behaviour is preserved (and tested).
	statusSet bool

	mu       sync.Mutex
	total    int64
	current  int64
	lastSent int64
	step     int64

	// Record the time at which the tracker was created
	// so that the UI can decide whether to show a progress bar
	startedAtSec int64

	parent   *ProgressTracker
	children map[*ProgressTracker]*progressState
}

type progressState struct {
	status  string
	total   int64
	current int64
}

// NewProgressTracker builds a root tracker for a status message. When total is
// unknown upfront, pass 0 and provide totals through child trackers.
func NewProgressTracker(status string, total int64, update StatusFunc) *ProgressTracker {
	if update == nil {
		return nil
	}

	if config.Global.DisableProgressTracker {
		return nil
	}

	return &ProgressTracker{
		status:       status,
		total:        total,
		update:       update,
		startedAtSec: time.Now().Unix(),
		step:         computeStep(total),
	}
}

// SubTracker creates a child tracker whose progress contributes to this
// tracker. Child updates are aggregated and emitted through the root tracker.
func (pt *ProgressTracker) SubTracker(status string, total int64) *ProgressTracker {
	if pt == nil || total <= 0 {
		return nil
	}

	child := &ProgressTracker{
		status:       status,
		total:        total,
		update:       pt.update,
		step:         computeStep(total),
		parent:       pt,
		startedAtSec: pt.startedAtSec,
	}

	pt.mu.Lock()
	if pt.children == nil {
		pt.children = make(map[*ProgressTracker]*progressState)
	}
	pt.children[child] = &progressState{status: status, total: total}
	current, totalProgress := pt.aggregateLocked()
	pt.step = computeStep(totalProgress)
	trackerStatus := pt.statusForChildStatusLocked(status)
	pt.mu.Unlock()

	pt.maybeSend(trackerStatus, current, totalProgress)
	return child
}

func (pt *ProgressTracker) Add(delta int64) {
	if pt == nil || delta == 0 {
		return
	}

	pt.mu.Lock()
	pt.current += delta
	if pt.current > pt.total {
		pt.current = pt.total
	}

	parent := pt.parent
	current, total := pt.aggregateLocked()
	pt.step = computeStep(total)
	send := parent == nil && pt.shouldSendLocked(current, total)
	status := pt.status

	if parent != nil {
		pt.mu.Unlock()
		parent.onChildDelta(pt, delta)
		return
	}

	if send {
		pt.send(status, current, total)
	}
	pt.mu.Unlock()
}

// WrapReader returns a reader that updates the tracker on each read.
// Wrapping multiple readers is safe, because Add() is thread-safe.
func (pt *ProgressTracker) WrapReader(reader io.Reader) io.Reader {
	if pt == nil {
		return reader
	}

	return &progressReader{reader: reader, tracker: pt}
}

func (pt *ProgressTracker) WrapReadCloser(readCloser io.ReadCloser) io.ReadCloser {
	if pt == nil {
		return readCloser
	}

	return &progressReadCloser{readCloser: readCloser, tracker: pt}
}

func (pt *ProgressTracker) Complete() {
	if pt == nil {
		return
	}

	pt.mu.Lock()
	remaining := pt.total - pt.current
	pt.mu.Unlock()
	pt.Add(remaining)
}

// SetStatus updates the tracker's status text and pushes a refresh to the UI,
// even if no bytes of progress have been recorded. This is useful when a long
// operation has distinct phases (preparing, streaming, flushing, finalizing)
// and the caller wants the user to see phase transitions without needing a
// progress bar.
//
// Calling SetStatus also flips the tracker into "explicit status" mode: from
// now on, statusForChildStatusLocked will always return pt.status — even when
// a single child exists. This matters for docker pushes where the daemon's
// progress stream creates per-layer sub-trackers with the same base status as
// the parent; without this override the parent's phase-summary updates would
// be masked by the single in-flight child. Callers that do not call SetStatus
// keep the default behaviour (single child inherits the parent's display).
//
// Status changes on a child tracker do not propagate; callers updating a
// plan's top-level status should target the tracker they own.
//
// An update is emitted only when the *displayed* text actually changes, so
// re-calling SetStatus with the same string is cheap and won't flood the UI.
func (pt *ProgressTracker) SetStatus(status string) {
	if pt == nil {
		return
	}

	pt.mu.Lock()
	prevDisplayed := pt.statusForChildStatusLocked(pt.status)
	pt.status = status
	pt.statusSet = true
	newDisplayed := pt.statusForChildStatusLocked(status)
	current, total := pt.aggregateLocked()
	pt.mu.Unlock()

	if prevDisplayed == newDisplayed {
		return
	}
	pt.send(newDisplayed, current, total)
}

// SetSubStatus sets a secondary detail line on the task without changing the
// main status text. This is useful for contextual information that would
// otherwise crowd the primary status line (e.g., Docker layer phase summaries).
func (pt *ProgressTracker) SetSubStatus(subStatus string) {
	if pt == nil {
		return
	}

	pt.mu.Lock()
	current, total := pt.aggregateLocked()
	status := pt.statusForChildStatusLocked(pt.status)
	pt.mu.Unlock()

	pt.update(StatusUpdate{
		Status:    status,
		SubStatus: subStatus,
		Progress:  &console.Progress{Current: current, Total: total, StartedAtSec: pt.startedAtSec},
	})
}

func (pt *ProgressTracker) onChildDelta(child *ProgressTracker, delta int64) {
	if pt == nil || delta == 0 {
		return
	}

	pt.mu.Lock()
	state, ok := pt.children[child]
	if !ok {
		pt.mu.Unlock()
		return
	}

	state.current += delta
	if state.current > state.total {
		state.current = state.total
	}

	current, total := pt.aggregateLocked()
	pt.step = computeStep(total)
	send := pt.shouldSendLocked(current, total)
	status := pt.statusForChildStatusLocked(child.status)

	if send {
		pt.send(status, current, total)
	}
	pt.mu.Unlock()
}

func (pt *ProgressTracker) aggregateLocked() (int64, int64) {
	current := pt.current
	total := pt.total
	for _, child := range pt.children {
		current += child.current
		total += child.total
	}
	return current, total
}

func (pt *ProgressTracker) shouldSendLocked(current, total int64) bool {
	if total <= 0 {
		return false
	}

	if current > total {
		current = total
	}

	if current >= total {
		pt.lastSent = current
		return true
	}

	if current-pt.lastSent >= pt.step {
		pt.lastSent = current
		return true
	}

	return false
}

func (pt *ProgressTracker) send(status string, current, total int64) {
	pt.update(StatusWithProgress(status, &console.Progress{Current: current, Total: total, StartedAtSec: pt.startedAtSec}))
}

func computeStep(total int64) int64 {
	const minStep = 32 * 1024
	step := max(total/100, minStep)
	return step
}

func (pt *ProgressTracker) maybeSend(status string, current, total int64) {
	pt.mu.Lock()
	shouldSend := pt.shouldSendLocked(current, total)
	pt.mu.Unlock()
	if shouldSend {
		pt.send(status, current, total)
	}
}

// statusForChildStatusLocked picks the status string to display for aggregated
// progress updates. Child status lines are shown only when the tracker has a
// single child and the tracker's own status has not been explicitly set — the
// latter case lets callers like consumeDockerProgress push phase summaries
// onto the parent without being silently overridden by a single child.
func (pt *ProgressTracker) statusForChildStatusLocked(childStatus string) string {
	if pt.statusSet {
		return pt.status
	}
	if len(pt.children) == 1 {
		return childStatus
	}
	return pt.status
}

type progressReader struct {
	reader  io.Reader
	tracker *ProgressTracker
}

func (p *progressReader) Read(buf []byte) (int, error) {
	n, err := p.reader.Read(buf)
	if n > 0 {
		p.tracker.Add(int64(n))
	}
	return n, err
}

type progressReadCloser struct {
	readCloser io.ReadCloser
	tracker    *ProgressTracker
}

func (p *progressReadCloser) Read(buf []byte) (int, error) {
	n, err := p.readCloser.Read(buf)
	if n > 0 {
		p.tracker.Add(int64(n))
	}
	return n, err
}

func (p *progressReadCloser) Close() error {
	return p.readCloser.Close()
}
