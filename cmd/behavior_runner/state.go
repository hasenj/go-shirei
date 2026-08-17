package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"go.hasen.dev/shirei"
)

type status int

const (
	statusIdle     status = iota
	statusQueued          // waiting for build worker
	statusBuilding        // go build in flight
	statusBuilt           // binary ready; waiting for run turn
	statusRunning
	statusPass
	statusFail
)

func (s status) String() string {
	switch s {
	case statusIdle:
		return "idle"
	case statusQueued:
		return "queued"
	case statusBuilding:
		return "building"
	case statusBuilt:
		return "ready"
	case statusRunning:
		return "running"
	case statusPass:
		return "pass"
	case statusFail:
		return "fail"
	default:
		return "?"
	}
}

var errStopped = errors.New("stopped")

type buildResult struct {
	binPath string
	log     string
	err     error
}

type testItem struct {
	Name     string
	Status   status
	Log      string
	Duration time.Duration // run duration (not build)
}

type appState struct {
	mu    sync.Mutex
	root  string
	tests []testItem

	running  bool // run-all or single in flight
	stop     bool
	runCmd   *exec.Cmd
	buildCmd *exec.Cmd
	outDir   string

	detailIdx int // -1 = closed; else modal for tests[i]
	selected  int
}

func newAppState(root string, names []string) *appState {
	tests := make([]testItem, len(names))
	for i, n := range names {
		tests[i] = testItem{Name: n, Status: statusIdle}
	}
	return &appState{
		root:      root,
		tests:     tests,
		detailIdx: -1,
		selected:  0,
	}
}

func discoverTests(root string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, "behavior_test"))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if name == "btmode" {
			continue
		}
		mainPath := filepath.Join(root, "behavior_test", name, "main.go")
		if _, err := os.Stat(mainPath); err != nil {
			continue
		}
		out = append(out, name)
	}
	return out, nil
}

func (s *appState) counts() (pass, fail, building, ready, running, total int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	total = len(s.tests)
	for i := range s.tests {
		switch s.tests[i].Status {
		case statusPass:
			pass++
		case statusFail:
			fail++
		case statusBuilding, statusQueued:
			building++
		case statusBuilt:
			ready++
		case statusRunning:
			running++
		}
	}
	return
}

func (s *appState) ensureOutDir() error {
	if s.outDir != "" {
		_ = os.RemoveAll(s.outDir)
	}
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("shirei-behavior-runner-%d", os.Getpid()))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	s.outDir = dir
	return nil
}

func (s *appState) startRunAll() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.stop = false
	if err := s.ensureOutDir(); err != nil {
		s.running = false
		s.mu.Unlock()
		return
	}
	n := len(s.tests)
	for i := range s.tests {
		s.tests[i].Status = statusQueued
		s.tests[i].Log = ""
		s.tests[i].Duration = 0
	}
	s.mu.Unlock()
	shirei.RequestNextFrame()

	results := make([]chan buildResult, n)
	for i := range results {
		results[i] = make(chan buildResult, 1)
	}

	go s.buildLoop(results)
	go func() {
		defer func() {
			s.mu.Lock()
			s.running = false
			s.runCmd = nil
			s.buildCmd = nil
			s.mu.Unlock()
			shirei.RequestNextFrame()
		}()
		s.runLoop(results, []string{"--close"})
	}()
}

func (s *appState) buildLoop(results []chan buildResult) {
	for i := range results {
		s.mu.Lock()
		stopped := s.stop
		s.mu.Unlock()
		if stopped {
			results[i] <- buildResult{err: errStopped}
			continue
		}
		results[i] <- s.buildOne(i)
	}
}

func (s *appState) runLoop(results []chan buildResult, args []string) {
	for i := range results {
		br := <-results[i]

		s.mu.Lock()
		stopped := s.stop || errors.Is(br.err, errStopped)
		s.mu.Unlock()
		if stopped {
			s.mu.Lock()
			s.tests[i].Status = statusIdle
			s.mu.Unlock()
			// Drain so buildLoop senders never block; mark remaining idle.
			for j := i + 1; j < len(results); j++ {
				<-results[j]
				s.mu.Lock()
				if s.tests[j].Status == statusQueued ||
					s.tests[j].Status == statusBuilding ||
					s.tests[j].Status == statusBuilt {
					s.tests[j].Status = statusIdle
				}
				s.mu.Unlock()
			}
			shirei.RequestNextFrame()
			return
		}

		if br.err != nil {
			s.mu.Lock()
			s.tests[i].Status = statusFail
			log := br.log
			if log == "" {
				log = fmt.Sprintf("build error: %v\n", br.err)
			} else {
				log = log + fmt.Sprintf("\nbuild error: %v\n", br.err)
			}
			s.tests[i].Log = log
			s.mu.Unlock()
			shirei.RequestNextFrame()
			continue
		}
		s.execBin(i, br.binPath, args, br.log)
	}
}

func (s *appState) stopRun() {
	s.mu.Lock()
	s.stop = true
	runCmd := s.runCmd
	buildCmd := s.buildCmd
	s.mu.Unlock()
	if runCmd != nil && runCmd.Process != nil {
		_ = runCmd.Process.Kill()
	}
	if buildCmd != nil && buildCmd.Process != nil {
		_ = buildCmd.Process.Kill()
	}
	shirei.RequestNextFrame()
}

// startSingle builds then runs one test (no ahead-of-time queue).
func (s *appState) startSingle(idx int, args []string) {
	s.mu.Lock()
	if s.running || idx < 0 || idx >= len(s.tests) {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.stop = false
	if err := s.ensureOutDir(); err != nil {
		s.running = false
		s.mu.Unlock()
		return
	}
	s.tests[idx].Status = statusQueued
	s.tests[idx].Log = ""
	s.tests[idx].Duration = 0
	s.mu.Unlock()
	shirei.RequestNextFrame()

	go func() {
		defer func() {
			s.mu.Lock()
			s.running = false
			s.runCmd = nil
			s.buildCmd = nil
			s.mu.Unlock()
			shirei.RequestNextFrame()
		}()
		br := s.buildOne(idx)
		if br.err != nil {
			s.mu.Lock()
			if errors.Is(br.err, errStopped) || s.stop {
				s.tests[idx].Status = statusIdle
			} else {
				s.tests[idx].Status = statusFail
				log := br.log
				if log == "" {
					log = fmt.Sprintf("build error: %v\n", br.err)
				}
				s.tests[idx].Log = log
			}
			s.mu.Unlock()
			shirei.RequestNextFrame()
			return
		}
		s.execBin(idx, br.binPath, args, br.log)
	}()
}

func (s *appState) buildOne(idx int) buildResult {
	s.mu.Lock()
	if s.stop {
		s.mu.Unlock()
		return buildResult{err: errStopped}
	}
	name := s.tests[idx].Name
	s.tests[idx].Status = statusBuilding
	outDir := s.outDir
	root := s.root
	s.mu.Unlock()
	shirei.RequestNextFrame()

	bin := filepath.Join(outDir, name)
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, "./behavior_test/"+name)
	cmd.Dir = root
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	s.mu.Lock()
	s.buildCmd = cmd
	stopped := s.stop
	s.mu.Unlock()
	if stopped {
		return buildResult{err: errStopped}
	}

	err := cmd.Run()

	s.mu.Lock()
	s.buildCmd = nil
	if s.stop {
		s.mu.Unlock()
		return buildResult{log: buf.String(), err: errStopped}
	}
	if err != nil {
		s.mu.Unlock()
		return buildResult{log: buf.String(), err: err}
	}
	s.tests[idx].Status = statusBuilt
	s.mu.Unlock()
	shirei.RequestNextFrame()
	return buildResult{binPath: bin, log: buf.String()}
}

func (s *appState) execBin(idx int, bin string, args []string, buildLog string) {
	s.mu.Lock()
	if s.stop {
		s.tests[idx].Status = statusIdle
		s.mu.Unlock()
		return
	}
	s.tests[idx].Status = statusRunning
	s.mu.Unlock()
	shirei.RequestNextFrame()

	cmd := exec.Command(bin, args...)
	cmd.Dir = s.root
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	t0 := time.Now()
	s.mu.Lock()
	s.runCmd = cmd
	stopped := s.stop
	s.mu.Unlock()
	if stopped {
		s.mu.Lock()
		s.tests[idx].Status = statusIdle
		s.mu.Unlock()
		return
	}

	err := cmd.Run()
	dur := time.Since(t0)
	log := buf.String()
	if buildLog != "" {
		// Keep build chatter only when the run log is empty (unusual).
		if log == "" {
			log = buildLog
		}
	}
	ok := err == nil

	s.mu.Lock()
	if s.stop && !ok {
		s.tests[idx].Status = statusIdle
		s.tests[idx].Log = log
		s.tests[idx].Duration = dur
	} else if ok {
		s.tests[idx].Status = statusPass
		s.tests[idx].Log = log
		s.tests[idx].Duration = dur
	} else {
		s.tests[idx].Status = statusFail
		if log == "" {
			log = fmt.Sprintf("exit error: %v\n", err)
		}
		s.tests[idx].Log = log
		s.tests[idx].Duration = dur
	}
	s.runCmd = nil
	s.mu.Unlock()
	shirei.RequestNextFrame()
}
