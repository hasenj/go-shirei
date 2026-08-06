package main

import (
	"fmt"
	"os/exec"
	"sync"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/widgets"
)

// Step status for the progress UI.
const (
	stepPending = 0
	stepActive  = 1
	stepDone    = 2
	stepFailed  = 3
)

// Progress tracks one bundling job for the GUI.
type Progress struct {
	mu              sync.Mutex
	ring            *widgets.TextRing
	busy            bool
	cancelRequested bool
	runningCmd      *exec.Cmd
	steps           []string
	status          []int // parallel to steps
	result          string
	err             string
}

func newProgress() *Progress {
	return &Progress{ring: widgets.NewTextRing(2 << 20)}
}

func (p *Progress) reset(steps []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.steps = append([]string(nil), steps...)
	p.status = make([]int, len(steps))
	p.result = ""
	p.err = ""
	p.busy = true
	p.cancelRequested = false
	p.runningCmd = nil
	p.ring = widgets.NewTextRing(2 << 20)
}

// RequestCancel marks the job cancelled and kills the current subprocess if any.
// Safe to call from a button handler (does not take the frame lock).
func (p *Progress) RequestCancel() {
	p.mu.Lock()
	p.cancelRequested = true
	cmd := p.runningCmd
	p.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		// Kill off the UI thread so a stuck process cannot freeze the frame.
		go func() { _ = cmd.Process.Kill() }()
	}
}

func (p *Progress) Cancelled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cancelRequested
}

func (p *Progress) setRunningCmd(cmd *exec.Cmd) {
	p.mu.Lock()
	p.runningCmd = cmd
	p.mu.Unlock()
}

func (p *Progress) clearRunningCmd() {
	p.mu.Lock()
	p.runningCmd = nil
	p.mu.Unlock()
}

func (p *Progress) snapshot() (steps []string, status []int, result, errMsg string, busy bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	steps = append([]string(nil), p.steps...)
	status = append([]int(nil), p.status...)
	return steps, status, p.result, p.err, p.busy
}

func (p *Progress) Ring() *widgets.TextRing {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ring
}

func (p *Progress) IsBusy() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.busy
}

// appendf appends a log line from a background goroutine (takes the frame lock).
// Do not call from button handlers / widget bodies — use appendfOnFrame instead.
func (p *Progress) appendf(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	shirei.WithFrameLock(func() {
		p.appendLine(line)
	})
	shirei.RequestNextFrame()
}

// appendfOnFrame appends from UI code that already holds the frame lock.
func (p *Progress) appendfOnFrame(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	p.appendLine(line)
}

func (p *Progress) appendLine(line string) {
	p.mu.Lock()
	if p.ring != nil {
		p.ring.AppendLine(line)
	}
	p.mu.Unlock()
}

// beginStep marks step i active and all previous as done (if still active/pending).
// Background only.
func (p *Progress) beginStep(i int) {
	shirei.WithFrameLock(func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		for j := 0; j < len(p.status); j++ {
			if j < i && p.status[j] != stepFailed {
				p.status[j] = stepDone
			}
			if j == i {
				p.status[j] = stepActive
			}
		}
	})
	shirei.RequestNextFrame()
}

func (p *Progress) succeedStep(i int) {
	shirei.WithFrameLock(func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if i >= 0 && i < len(p.status) {
			p.status[i] = stepDone
		}
	})
	shirei.RequestNextFrame()
}

// failStep ends the job with an error. Background only (takes frame lock).
func (p *Progress) failStep(i int, err error) {
	shirei.WithFrameLock(func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if i >= 0 && i < len(p.status) {
			p.status[i] = stepFailed
		}
		if err != nil {
			p.err = err.Error()
		}
		p.busy = false
		p.runningCmd = nil
	})
	shirei.RequestNextFrame()
}

// finishOK ends the job successfully. Background only.
func (p *Progress) finishOK(result string) {
	shirei.WithFrameLock(func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		for i := range p.status {
			if p.status[i] != stepFailed {
				p.status[i] = stepDone
			}
		}
		p.result = result
		p.busy = false
		p.runningCmd = nil
	})
	shirei.RequestNextFrame()
}

// clearChrome resets the progress UI to idle (call from the frame/UI thread).
func (p *Progress) clearChrome() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.busy = false
	p.result = ""
	p.err = ""
	p.steps = nil
	p.status = nil
	p.cancelRequested = false
	p.runningCmd = nil
}

// JobHub holds one Progress per platform so several systems can bundle at once.
type JobHub struct {
	mu   sync.Mutex
	jobs map[string]*Progress // platform id → job
}

func newJobHub() *JobHub {
	return &JobHub{jobs: map[string]*Progress{}}
}

// get returns the progress for a platform (creates idle if missing).
func (h *JobHub) get(platform string) *Progress {
	h.mu.Lock()
	defer h.mu.Unlock()
	p := h.jobs[platform]
	if p == nil {
		p = newProgress()
		h.jobs[platform] = p
	}
	return p
}

// tryBegin starts a new job for platform. Returns nil if that platform is already busy.
func (h *JobHub) tryBegin(platform string, steps []string) *Progress {
	h.mu.Lock()
	p := h.jobs[platform]
	if p == nil {
		p = newProgress()
		h.jobs[platform] = p
	}
	h.mu.Unlock()

	p.mu.Lock()
	if p.busy {
		p.mu.Unlock()
		return nil
	}
	// Inline reset under lock (same as reset()).
	p.steps = append([]string(nil), steps...)
	p.status = make([]int, len(steps))
	p.result = ""
	p.err = ""
	p.busy = true
	p.cancelRequested = false
	p.runningCmd = nil
	p.ring = widgets.NewTextRing(2 << 20)
	p.mu.Unlock()
	return p
}

// isBusy reports whether platform has an active job.
func (h *JobHub) isBusy(platform string) bool {
	h.mu.Lock()
	p := h.jobs[platform]
	h.mu.Unlock()
	if p == nil {
		return false
	}
	return p.IsBusy()
}

// anyBusy is true if any platform job is running.
func (h *JobHub) anyBusy() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, p := range h.jobs {
		p.mu.Lock()
		busy := p.busy
		p.mu.Unlock()
		if busy {
			return true
		}
	}
	return false
}

// list returns platforms with a job that is busy or still showing a result/error.
func (h *JobHub) listActiveChrome() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for plat, p := range h.jobs {
		p.mu.Lock()
		show := p.busy || p.result != "" || p.err != ""
		p.mu.Unlock()
		if show {
			out = append(out, plat)
		}
	}
	// Stable order for UI.
	order := []string{platformIOS, platformAndroid, platformMacOS, "macos-notarize", platformLinux, platformWindows}
	var ordered []string
	seen := map[string]bool{}
	for _, o := range order {
		for _, plat := range out {
			if plat == o && !seen[plat] {
				ordered = append(ordered, plat)
				seen[plat] = true
			}
		}
	}
	for _, plat := range out {
		if !seen[plat] {
			ordered = append(ordered, plat)
		}
	}
	return ordered
}

// activeFailIndex is the step index to mark failed for this progress snapshot.
func activeFailIndexFor(p *Progress) int {
	_, status, _, _, _ := p.snapshot()
	failIdx := 0
	for i, s := range status {
		if s == stepActive {
			return i
		}
		if s == stepDone {
			failIdx = i + 1
		}
	}
	return failIdx
}
