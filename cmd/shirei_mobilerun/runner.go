package main

import (
	"fmt"
	"strings"
	"sync"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/widgets"
)

// Runner owns the background build/launch job and a log ring for the UI.
type Runner struct {
	mu   sync.Mutex
	ring *widgets.TextRing
	busy bool
}

func newRunner() *Runner {
	return &Runner{ring: widgets.NewTextRing(2 << 20)}
}

func (r *Runner) appendf(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	shirei.WithFrameLock(func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.ring != nil {
			r.ring.AppendLine(line)
		}
	})
	shirei.RequestNextFrame()
}

func (r *Runner) IsBusy() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.busy
}

func (r *Runner) Ring() *widgets.TextRing {
	return r.ring
}

// Start dispatches to the Android or iOS pipeline. pkgDir must be absolute.
func (r *Runner) Start(pkgDir string, cfg Config) error {
	r.mu.Lock()
	if r.busy {
		r.mu.Unlock()
		return fmt.Errorf("already running")
	}
	r.busy = true
	r.mu.Unlock()

	go func() {
		var err error
		switch cfg.Platform {
		case platformAndroid:
			err = r.runAndroid(pkgDir, cfg)
		case platformIOS:
			err = r.runIOS(pkgDir, cfg)
		default:
			err = fmt.Errorf("unknown platform %q", cfg.Platform)
		}
		r.mu.Lock()
		r.busy = false
		r.mu.Unlock()
		if err != nil {
			r.appendf("— error: %v —", err)
		} else {
			r.appendf("— done —")
		}
		shirei.RequestNextFrame()
	}()
	return nil
}

func (r *Runner) runAndroid(pkgDir string, cfg Config) error {
	appID := strings.TrimSpace(cfg.AppID)
	label := strings.TrimSpace(cfg.AppName)
	icon := resolveIconPath(pkgDir, cfg.IconPath)
	r.appendf("— android %s (arch=%s serial=%s)", pkgDir, orDefault(cfg.Arch, "arm64"), orDefault(cfg.Serial, "default"))
	return buildAndRun(AndroidOpts{
		PkgDir:   pkgDir,
		Arch:     cfg.Arch,
		AppID:    appID,
		Label:    label,
		IconPath: icon,
		IDPrefix: cfg.effectiveAppIDPrefix(),
		Serial:   cfg.Serial,
	}, r.appendf)
}

func (r *Runner) runIOS(pkgDir string, cfg Config) error {
	return startIOS(pkgDir, cfg, r.appendf)
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
