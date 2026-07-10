// Package cursorshape implements the staging wp-cursor-shape-v1 protocol
// (not shipped by upstream neurlang; originally hand-written inside shirei's
// wayland backend and graduated here). It lets the compositor draw the real
// themed cursor — the same cursor the rest of the desktop uses, correctly
// sized on HiDPI — with the client only naming a shape.
//
// Protocol:
//
//	wp_cursor_shape_manager_v1.get_pointer(new_id device, wl_pointer) -> opcode 1
//	wp_cursor_shape_device_v1.set_shape(serial, shape)                -> opcode 1
//
// Neither interface has events, so the proxies' Dispatch is a no-op.
package cursorshape

import (
	"errors"

	"go.hasen.dev/shirei/internal/wayland/wl"
)

// ShapeDefault is wp_cursor_shape_device_v1.shape.default — the arrow.
const ShapeDefault = 1

// Interface is the global's advertised interface name in the registry.
const Interface = "wp_cursor_shape_manager_v1"

type Manager struct{ wl.BaseProxy }

func (*Manager) Dispatch(*wl.Event) {}

type Device struct{ wl.BaseProxy }

func (*Device) Dispatch(*wl.Event) {}

var errNoContext = errors.New("cursorshape: proxy has no context")

// BindManager binds wp_cursor_shape_manager_v1 from the registry.
func BindManager(registry *wl.Registry, name, version uint32) (*Manager, error) {
	ctx := registry.Context()
	if ctx == nil {
		return nil, errNoContext
	}
	mgr := &Manager{}
	ctx.Register(mgr)
	if err := registry.Bind(name, Interface, version, mgr); err != nil {
		return nil, err
	}
	return mgr, nil
}

// GetPointer creates the per-pointer shape device.
func (m *Manager) GetPointer(pointer *wl.Pointer) (*Device, error) {
	ctx := m.Context()
	if ctx == nil {
		return nil, errNoContext
	}
	dev := &Device{}
	ctx.Register(dev)
	if err := ctx.SendRequest(m, 1, dev, pointer); err != nil {
		return nil, err
	}
	return dev, nil
}

// SetShape asks the compositor to draw the named shape for this enter serial.
func (d *Device) SetShape(serial, shape uint32) error {
	ctx := d.Context()
	if ctx == nil {
		return errNoContext
	}
	return ctx.SendRequest(d, 1, serial, shape)
}
