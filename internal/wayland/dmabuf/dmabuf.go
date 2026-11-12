// Package dmabuf implements the client half of zwp_linux_dmabuf_v1
// (create_params / add / create / create_immed) and v4+ feedback
// (format table + modifiers). Hand-written like textinput/ and cursorshape/.
package dmabuf

import (
	"encoding/binary"
	"errors"
	"sync"

	wos "go.hasen.dev/shirei/internal/wayland/os"
	"go.hasen.dev/shirei/internal/wayland/wl"
)

// Interface is the global advertised in the registry.
const Interface = "zwp_linux_dmabuf_v1"

// FormatXRGB8888 is drm fourcc 'XR24' — the same layout as wl_shm XRGB8888.
const FormatXRGB8888 = 0x34325258

// FormatARGB8888 is drm fourcc AR24 with premultiplied alpha.
const FormatARGB8888 = 0x34325241

// ModLinear is DRM_FORMAT_MOD_LINEAR.
const ModLinear = uint64(0)

// ModInvalid is DRM_FORMAT_MOD_INVALID: implicit modifier (gbm_bo_create
// without an explicit modifier list). Some compositors advertise only this.
const ModInvalid = uint64(0x00ffffffffffffff)

const trancheScanout = 1

var errNoContext = errors.New("dmabuf: proxy has no context")

// Dmabuf is zwp_linux_dmabuf_v1.
type Dmabuf struct {
	wl.BaseProxy
	version uint32
	mu      sync.Mutex
	formats map[uint32]struct{}
	mods    map[uint32][]uint64 // modifiers by format from feedback
}

func (d *Dmabuf) init() {
	d.formats = make(map[uint32]struct{})
	d.mods = make(map[uint32][]uint64)
}

// Bind binds zwp_linux_dmabuf_v1. Version is capped at 5; 3 is required
// (create_immed). v4+ enables modifier feedback.
func Bind(registry *wl.Registry, name, version uint32) (*Dmabuf, error) {
	ctx := registry.Context()
	if ctx == nil {
		return nil, errNoContext
	}
	if version < 3 {
		return nil, errors.New("dmabuf: compositor version < 3 (no create_immed)")
	}
	if version > 5 {
		version = 5
	}
	d := &Dmabuf{version: version}
	d.init()
	ctx.Register(d)
	if err := registry.Bind(name, Interface, version, d); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *Dmabuf) CanFeedback() bool { return d != nil && d.version >= 4 }

// HasFormat reports whether the compositor advertised fourcc (v3 format
// events, or a v4 format table).
func (d *Dmabuf) HasFormat(fourcc uint32) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.formats[fourcc]
	return ok
}

// Modifiers returns compositor-preferred modifiers for fourcc from the
// last completed feedback. Empty if feedback was not used or listed none.
func (d *Dmabuf) Modifiers(fourcc uint32) []uint64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]uint64, len(d.mods[fourcc]))
	copy(out, d.mods[fourcc])
	return out
}

func (d *Dmabuf) Dispatch(event *wl.Event) {
	switch event.Opcode {
	case 0: // format (unused since v4, still sent on v3)
		fmt := event.Uint32()
		d.mu.Lock()
		d.formats[fmt] = struct{}{}
		d.mu.Unlock()
	case 1: // modifier (v3): format, modifier_hi, modifier_lo
		fmt := event.Uint32()
		hi := event.Uint32()
		lo := event.Uint32()
		d.mu.Lock()
		d.formats[fmt] = struct{}{}
		mod := uint64(hi)<<32 | uint64(lo)
		d.mods[fmt] = appendUniq(d.mods[fmt], mod)
		d.mu.Unlock()
	}
}

// Params is zwp_linux_buffer_params_v1.
type Params struct {
	wl.BaseProxy
	buf    *wl.Buffer
	failed bool
}

func (p *Params) Dispatch(event *wl.Event) {
	switch event.Opcode {
	case 0: // created
		p.buf = wl.BufferFromEventId(p.Context(), event.Uint32())
	case 1: // failed
		p.failed = true
	}
}

// CreateParams makes a temporary params object.
func (d *Dmabuf) CreateParams() (*Params, error) {
	ctx := d.Context()
	if ctx == nil {
		return nil, errNoContext
	}
	p := &Params{}
	ctx.Register(p)
	if err := ctx.SendRequest(d, 1, p); err != nil {
		return nil, err
	}
	return p, nil
}

// Add attaches one dmabuf plane. fd is not closed.
func (p *Params) Add(fd uintptr, planeIdx, offset, stride, modifierHi, modifierLo uint32) error {
	ctx := p.Context()
	if ctx == nil {
		return errNoContext
	}
	return ctx.SendRequest(p, 1, fd, planeIdx, offset, stride, modifierHi, modifierLo)
}

// Create asks the compositor to import the planes. After a roundtrip,
// Buffer() is the result: a wl_buffer on success, or an error if the
// compositor sent failed. A rejected import is not a fatal protocol error.
func (p *Params) Create(width, height int32, format, flags uint32) error {
	ctx := p.Context()
	if ctx == nil {
		return errNoContext
	}
	return ctx.SendRequest(p, 2, width, height, format, flags)
}

// Buffer is the wl_buffer from a completed Create, after the compositor
// has sent created or failed.
func (p *Params) Buffer() (*wl.Buffer, error) {
	if p.failed || p.buf == nil {
		return nil, errors.New("dmabuf: compositor rejected import")
	}
	return p.buf, nil
}

// CreateImmed imports the planes as a wl_buffer immediately (v3).
// A compositor that cannot import raises a fatal protocol error.
func (p *Params) CreateImmed(width, height int32, format, flags uint32) (*wl.Buffer, error) {
	ctx := p.Context()
	if ctx == nil {
		return nil, errNoContext
	}
	buf := wl.NewBuffer(ctx)
	if err := ctx.SendRequest(p, 3, buf, width, height, format, flags); err != nil {
		return nil, err
	}
	return buf, nil
}

// Destroy destroys the params object. Buffers created from it stay valid.
func (p *Params) Destroy() error {
	ctx := p.Context()
	if ctx == nil {
		return errNoContext
	}
	return ctx.SendRequest(p, 0)
}

// Feedback is zwp_linux_dmabuf_feedback_v1 (since v4).
type Feedback struct {
	wl.BaseProxy
	parent *Dmabuf
	done   bool

	table      []feedbackEntry
	tableMem   []byte
	tranche    uint32 // flags; set before tranche_done
	pendingIdx []uint16
	scanMods   map[uint32][]uint64
	otherMods  map[uint32][]uint64
}

// Done reports receipt of a complete feedback batch on the dispatch thread.
func (f *Feedback) Done() bool { return f.done }

type feedbackEntry struct {
	format uint32
	mod    uint64
}

func (f *Feedback) Dispatch(event *wl.Event) {
	switch event.Opcode {
	case 0: // done
		f.finish()
	case 1: // format_table
		fd, err := event.FD()
		size := event.Uint32()
		if err != nil || size < 16 {
			if fd != 0 {
				wos.Close(int(fd))
			}
			return
		}
		mem, err := wos.Mmap(int(fd), 0, int(size), wos.ProtRead, wos.MapPrivate)
		wos.Close(int(fd))
		if err != nil {
			return
		}
		n := int(size) / 16
		f.table = make([]feedbackEntry, n)
		for i := 0; i < n; i++ {
			off := i * 16
			f.table[i] = feedbackEntry{
				format: binary.LittleEndian.Uint32(mem[off:]),
				mod:    binary.LittleEndian.Uint64(mem[off+8:]),
			}
		}
		f.tableMem = mem
	case 2: // main_device
		_ = event.ArrayBytes()
	case 3: // tranche_done — flags and formats have already arrived
		for _, idx := range f.pendingIdx {
			if int(idx) >= len(f.table) {
				continue
			}
			e := f.table[idx]
			if f.tranche&trancheScanout != 0 {
				f.scanMods[e.format] = appendUniq(f.scanMods[e.format], e.mod)
			} else {
				f.otherMods[e.format] = appendUniq(f.otherMods[e.format], e.mod)
			}
		}
		f.pendingIdx = f.pendingIdx[:0]
		f.tranche = 0
	case 4: // tranche_target_device
		_ = event.ArrayBytes()
	case 5: // tranche_formats — uint16 indices into the format table
		b := event.ArrayBytes()
		for i := 0; i+1 < len(b); i += 2 {
			f.pendingIdx = append(f.pendingIdx, binary.LittleEndian.Uint16(b[i:]))
		}
	case 6: // tranche_flags
		f.tranche = event.Uint32()
	}
}

func (f *Feedback) finish() {
	f.done = true
	// Window buffers are textured, not scanned out. Use the compositing
	// tranche as advertised (including MOD_INVALID); scanout last.
	mods := make(map[uint32][]uint64)
	for format, list := range f.otherMods {
		mods[format] = append([]uint64(nil), list...)
	}
	for format, list := range f.scanMods {
		for _, m := range list {
			mods[format] = appendUniq(mods[format], m)
		}
	}
	if f.parent != nil {
		f.parent.mu.Lock()
		f.parent.mods = mods
		for _, e := range f.table {
			f.parent.formats[e.format] = struct{}{}
		}
		f.parent.mu.Unlock()
	}
	if f.tableMem != nil {
		wos.Munmap(f.tableMem)
		f.tableMem = nil
	}
}

// GetSurfaceFeedback starts per-surface modifier advertisement (v4+).
// A roundtrip is needed before Modifiers() is populated.
func (d *Dmabuf) GetSurfaceFeedback(surface *wl.Surface) (*Feedback, error) {
	if !d.CanFeedback() {
		return nil, errors.New("dmabuf: feedback needs version 4")
	}
	ctx := d.Context()
	if ctx == nil {
		return nil, errNoContext
	}
	fb := &Feedback{parent: d, scanMods: make(map[uint32][]uint64), otherMods: make(map[uint32][]uint64)}
	ctx.Register(fb)
	if err := ctx.SendRequest(d, 3, fb, surface); err != nil {
		return nil, err
	}
	return fb, nil
}

// Destroy unbinds the feedback object.
func (f *Feedback) Destroy() error {
	ctx := f.Context()
	if ctx == nil {
		return errNoContext
	}
	if f.tableMem != nil {
		wos.Munmap(f.tableMem)
		f.tableMem = nil
	}
	return ctx.SendRequest(f, 0)
}

func appendUniq(dst []uint64, v uint64) []uint64 {
	for _, x := range dst {
		if x == v {
			return dst
		}
	}
	return append(dst, v)
}
