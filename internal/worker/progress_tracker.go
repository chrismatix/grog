package worker

import (
	"io"
	"sync"

	"grog/internal/console"
)

// ProgressTracker aggregates progress updates and throttles UI refreshes.
// The tracker is safe for concurrent use across goroutines working on the
// same logical unit of work. Progress can be subdivided into child trackers
// when multiple operations contribute to the same overall status.
type ProgressTracker struct {
	status string
	update StatusFunc

	mu       sync.Mutex
	total    int64
	current  int64
	lastSent int64
	step     int64

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

	tracker := &ProgressTracker{
		status: status,
		total:  total,
		update: update,
		step:   computeStep(total),
	}

	tracker.maybeSend(status, 0, total)
	return tracker
}

// SubTracker creates a child tracker whose progress contributes to this
// tracker. Child updates are aggregated and emitted through the root tracker.
func (pt *ProgressTracker) SubTracker(status string, total int64) *ProgressTracker {
	if pt == nil || total <= 0 {
		return nil
	}

	child := &ProgressTracker{
		status: status,
		total:  total,
		update: pt.update,
		step:   computeStep(total),
		parent: pt,
	}

	pt.mu.Lock()
	if pt.children == nil {
		pt.children = make(map[*ProgressTracker]*progressState)
	}
	pt.children[child] = &progressState{status: status, total: total}
	current, totalProgress := pt.aggregateLocked()
	pt.step = computeStep(totalProgress)
	pt.mu.Unlock()

	pt.maybeSend(status, current, totalProgress)
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
	pt.mu.Unlock()

	if parent != nil {
		parent.onChildDelta(pt, delta)
		return
	}

	if send {
		pt.send(status, current, total)
	}
}

func (pt *ProgressTracker) WrapReader(reader io.Reader) io.Reader {
	if pt == nil {
		return reader
	}

	return &progressReader{reader: reader, tracker: pt}
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
	status := child.status
	pt.mu.Unlock()

	if send {
		pt.send(status, current, total)
	}
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
	pt.update(StatusWithProgress(status, &console.Progress{Current: current, Total: total}))
}

func computeStep(total int64) int64 {
	const minStep = 32 * 1024
	step := total / 100
	if step < minStep {
		step = minStep
	}
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

type progressReader struct {
	reader  io.Reader
	tracker *ProgressTracker
}

func (p *progressReader) Read(buf []byte) (int, error) {
	n, err := p.reader.Read(buf)
	p.tracker.Add(int64(n))
	return n, err
}
