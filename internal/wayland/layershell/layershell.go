// Package layershell implements zwlr_layer_shell_v1 version 4 for decorationless
// windows with on-demand keyboard focus. Surface state applies on wl_surface.commit.
package layershell

import (
	"fmt"

	"go.hasen.dev/shirei/internal/wayland/wl"
)

const Interface = "zwlr_layer_shell_v1"
const LayerTop uint32 = 2
const KeyboardOnDemand uint32 = 2
const AnchorTopLeft uint32 = 1 | 4

type Manager struct{ wl.BaseProxy }

func (*Manager) Dispatch(*wl.Event) {}

// Bind requires version 4 for on-demand keyboard focus.
func Bind(registry *wl.Registry, name, version uint32) (*Manager, error) {
	if version < 4 {
		return nil, fmt.Errorf("layer-shell version %d has no on-demand keyboard focus", version)
	}
	m := &Manager{}
	registry.Context().Register(m)
	if err := registry.Bind(name, Interface, 4, m); err != nil {
		m.Unregister()
		return nil, err
	}
	return m, nil
}

type ConfigureEvent struct{ Serial, Width, Height uint32 }
type Listener interface {
	HandleLayerConfigure(ConfigureEvent)
	HandleLayerClosed()
}

type Surface struct {
	wl.BaseProxy
	listener Listener
}

// GetSurface assigns a layer role before the surface's first commit. A nil output
// lets the compositor choose an output. Namespace identifies the surface's purpose.
func (m *Manager) GetSurface(surface *wl.Surface, output *wl.Output, namespace string, listener Listener) (*Surface, error) {
	s := &Surface{listener: listener}
	m.Context().Register(s)
	if err := m.Context().SendRequest(m, 0, s, surface, output, LayerTop, namespace); err != nil {
		s.Unregister()
		return nil, err
	}
	return s, nil
}

func (s *Surface) Dispatch(ev *wl.Event) {
	switch ev.Opcode {
	case 0:
		e := ConfigureEvent{ev.Uint32(), ev.Uint32(), ev.Uint32()}
		s.listener.HandleLayerConfigure(e)
	case 1:
		s.listener.HandleLayerClosed()
	}
}

func (s *Surface) SetSize(width, height uint32) error {
	return s.Context().SendRequest(s, 0, width, height)
}
func (s *Surface) SetExclusiveZone(zone int32) error { return s.Context().SendRequest(s, 2, zone) }
func (s *Surface) SetKeyboardInteractivity(mode uint32) error {
	return s.Context().SendRequest(s, 4, mode)
}
func (s *Surface) AckConfigure(serial uint32) error { return s.Context().SendRequest(s, 6, serial) }
func (s *Surface) Destroy() error                   { return s.Context().SendRequest(s, 7) }

func (s *Surface) SetAnchor(anchor uint32) error { return s.Context().SendRequest(s, 1, anchor) }
func (s *Surface) SetMargin(top, right, bottom, left int32) error {
	return s.Context().SendRequest(s, 3, top, right, bottom, left)
}
