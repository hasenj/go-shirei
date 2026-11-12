//go:build linux

package waylandbackend

import (
	"fmt"
	"os"
	"sync/atomic"

	"go.hasen.dev/shirei/internal/wayland/wl"
)

var activationManager *activationGlobal
var activationRequested atomic.Bool
var activationSerial uint32
var activationSource *windowState
var launchActivationToken string
var quiet bool

type activationGlobal struct{ wl.BaseProxy }

func (*activationGlobal) Dispatch(*wl.Event) {}

type activationToken struct {
	wl.BaseProxy
	target *windowState
}

func (t *activationToken) Dispatch(ev *wl.Event) {
	if ev.Opcode != 0 {
		return
	}
	token := ev.String()
	if w := t.target; !w.closed && !w.closing && !w.hidden {
		_ = activationManager.Context().SendRequest(activationManager, 2, token, w.surface)
	}
	_ = t.Context().SendRequest(t, 4)
}

func bindActivation(name uint32) {
	m := &activationGlobal{}
	registry.Context().Register(m)
	if err := registry.Bind(name, "xdg_activation_v1", 1, m); err != nil {
		m.Unregister()
		return
	}
	activationManager = m
}

// Activate requests main-window activation and maps a hidden background window.
// Safe from any goroutine. The compositor can deny the focus request.
func Activate() { activationRequested.Store(true) }

func applyActivationRequests() {
	if !activationRequested.Swap(false) {
		return
	}
	w := mainWindow
	if w.closed || w.closing {
		return
	}
	if w.hidden {
		wlDebug("restore main %q (new-surface=%v)", w.winTitle, w.surfaceNeedsCreate)
		w.hidden = false
		w.waitConfigure, w.hasAck = true, false
		w.pendingW, w.pendingH = 0, 0
		w.havePresented = false
		if w.surfaceNeedsCreate {
			if err := createWindow(); err != nil {
				panic(fmt.Errorf("waylandbackend: restore window: %w", err))
			}
			w.surfaceNeedsCreate = false
			if w.xdgToplevel != nil {
				for _, child := range popupWindows {
					if child.owner == w && !child.topLevel && child.xdgToplevel != nil && !child.closed {
						_ = child.xdgToplevel.SetParent(w.xdgToplevel)
					}
				}
			}
		} else {
			if w.layerSurface != nil {
				_ = configureLayerWindow(w)
			}
			_ = w.surface.Commit()
		}
	}
	w.activateAfterPresent, w.dirty = true, true
	// A mapped idle surface needs no additional presentation to request focus.
	if w.havePresented {
		requestActivation(w)
	}
}

func requestActivation(w *windowState) {
	w.activateAfterPresent = false
	if activationManager == nil {
		return
	}
	if w == mainWindow && launchActivationToken != "" {
		token := launchActivationToken
		launchActivationToken = ""
		_ = activationManager.Context().SendRequest(activationManager, 2, token, w.surface)
		return
	}
	t := &activationToken{target: w}
	ctx := activationManager.Context()
	ctx.Register(t)
	if err := ctx.SendRequest(activationManager, 1, t); err != nil {
		t.Unregister()
		return
	}
	if activationSource != nil && !activationSource.closed && !activationSource.hidden {
		_ = ctx.SendRequest(t, 2, activationSource.surface)
		if activationSerial != 0 && seat != nil {
			_ = ctx.SendRequest(t, 0, activationSerial, seat)
		}
	}
	_ = ctx.SendRequest(t, 1, appID())
	_ = ctx.SendRequest(t, 3)
}

func consumeLaunchActivationToken() {
	launchActivationToken = os.Getenv("XDG_ACTIVATION_TOKEN")
	_ = os.Unsetenv("XDG_ACTIVATION_TOKEN")
	if quiet {
		launchActivationToken = ""
	}
	mainWindow.activateAfterPresent = launchActivationToken != ""
}
