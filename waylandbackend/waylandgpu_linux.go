//go:build linux

package waylandbackend

import (
	"fmt"
	"os"
	"slices"
	"time"

	g "go.hasen.dev/generic"
	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/gpurender"
	"go.hasen.dev/shirei/internal/wayland/dmabuf"
	"go.hasen.dev/shirei/internal/wayland/wl"
	"go.hasen.dev/shirei/internal/wayland/wlclient"
)

const gpuPoolSize = 3

var (
	linuxDmabuf *dmabuf.Dmabuf
)

type gpuSlot struct {
	target *gpurender.Target
	buf    *wl.Buffer
	busy   bool
	w, h   int
}

func (s *gpuSlot) HandleBufferRelease(wl.BufferReleaseEvent) { s.busy = false }

func bindLinuxDmabuf(name, version uint32) {
	d, err := dmabuf.Bind(registry, name, version)
	if err != nil {
		perfLog("[wl] linux-dmabuf: %v", err)
		return
	}
	linuxDmabuf = d
	perfLog("[wl] zwp_linux_dmabuf_v1 bound")
}

func tryInitGPU() {
	if g.EnvFalsy("SHIREI_GPU") {
		perfLog("[wl] GPU off (SHIREI_GPU=0)")
		return
	}
	if linuxDmabuf == nil {
		perfLog("[wl] GPU skipped: no zwp_linux_dmabuf_v1")
		return
	}
	if linuxDmabuf.CanFeedback() && currentWindow.surface != nil {
		fb, err := linuxDmabuf.GetSurfaceFeedback(currentWindow.surface)
		if err != nil {
			perfLog("[wl] GPU feedback: %v", err)
			return
		} else {
			if err := wlclient.DisplayRoundtrip(disp); err != nil {
				perfLog("[wl] GPU feedback roundtrip: %v", err)
			}
			deadline := time.Now().Add(time.Second)
			for !fb.Done() && time.Now().Before(deadline) {
				err := wlclient.DisplayDispatchTimeout(disp, 16*time.Millisecond)
				if err != nil && err != wl.ErrContextRunTimeout && err != wl.ErrContextRunProxyNil {
					break
				}
			}
			fb.Destroy()
			if !fb.Done() {
				perfLog("[wl] GPU skipped: incomplete surface feedback")
				return
			}
		}
	}
	format := uint32(dmabuf.FormatXRGB8888)
	if shirei.GetHost().TransparentBackground {
		format = dmabuf.FormatARGB8888
		if !linuxDmabuf.HasFormat(format) {
			perfLog("[wl] GPU skipped: compositor did not advertise ARGB8888")
			return
		}
	}
	currentWindow.gpuModifiers = linuxDmabuf.Modifiers(format)
	gpurender.SetModifiers(currentWindow.gpuModifiers)
	if mods := currentWindow.gpuModifiers; len(mods) > 0 {
		perfLog("[wl] GPU modifiers %d first=%#x", len(mods), mods[0])
	} else {
		perfLog("[wl] GPU skipped: no advertised modifiers for format %#x", format)
		return
	}
	if err := gpurender.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "gpurender: fallback to software: %v\n", err)
		return
	}
	if err := probeGPUBuffer(); err != nil {
		fmt.Fprintf(os.Stderr, "gpurender: fallback to software: %v\n", err)
		return
	}
	currentWindow.gpuOK = true
	perfLog("[wl] GPU %s", gpurender.DeviceName())
}

func probeGPUBuffer() error {
	t, err := gpurender.AllocTarget(64, 64)
	if err != nil {
		return err
	}
	buf, err := dmabufBuffer(t)
	if buf != nil {
		destroyWindowBuffer(buf)
		_ = wlclient.DisplayRoundtrip(disp)
	}
	gpurender.FreeTarget(t)
	return err
}

func disableGPU(err error) {
	if !currentWindow.gpuOK {
		return
	}
	fmt.Fprintf(os.Stderr, "gpurender: fallback to software: %v\n", err)
	destroyGPUBuffers()
	currentWindow.gpuOK = false
}

func destroyGPUBuffers() {
	if currentWindow.gpuOK {
		gpurender.WaitIdle()
	}
	for i := range currentWindow.gpuSlots {
		s := &currentWindow.gpuSlots[i]
		if s.buf != nil {
			destroyWindowBuffer(s.buf)
			s.buf = nil
		}
		if s.target != nil {
			gpurender.FreeTarget(s.target)
			s.target = nil
		}
		s.busy = false
		s.w, s.h = 0, 0
	}
}

func nextGPUBuffer() *gpuSlot {
	var s *gpuSlot
	for i := range currentWindow.gpuSlots {
		if !currentWindow.gpuSlots[i].busy {
			s = &currentWindow.gpuSlots[i]
			break
		}
	}
	if s == nil {
		return nil
	}
	if s.target != nil && (s.w != currentWindow.curW || s.h != currentWindow.curH) {
		if s.buf != nil {
			destroyWindowBuffer(s.buf)
			s.buf = nil
		}
		gpurender.FreeTarget(s.target)
		s.target = nil
	}
	if s.target == nil {
		gpurender.SetModifiers(currentWindow.gpuModifiers)
		w, h := currentWindow.curW, currentWindow.curH
		var t *gpurender.Target
		var buf *wl.Buffer
		for i := 0; i < 8; i++ {
			if w <= 0 || h <= 0 {
				return nil
			}
			nt, err := gpurender.AllocTarget(w, h)
			if err != nil {
				perfLog("[wl] GPU alloc: %v", err)
				return nil
			}
			nb, err := dmabufBuffer(nt)
			if err != nil {
				gpurender.FreeTarget(nt)
				perfLog("[wl] GPU wl_buffer: %v", err)
				disableGPU(err)
				return nil
			}
			// create+roundtrip can deliver another configure; do not paint
			// a target whose size no longer matches the window.
			if currentWindow.curW == w && currentWindow.curH == h {
				t, buf = nt, nb
				break
			}
			destroyWindowBuffer(nb)
			_ = wlclient.DisplayRoundtrip(disp)
			gpurender.FreeTarget(nt)
			w, h = currentWindow.curW, currentWindow.curH
		}
		if t == nil {
			return nil
		}
		perfLog("[wl] GPU buffer %dx%d modifier=0x%x planes=%d", w, h, t.Modifier(), t.Planes())
		s.target = t
		s.buf = buf
		s.w, s.h = t.Size()
		wlclient.BufferAddListener(buf, s)
	}
	return s
}

func dmabufBuffer(t *gpurender.Target) (*wl.Buffer, error) {
	// Unsupported pairs are protocol errors even with asynchronous create.
	// Validate the allocation against this surface's completed advertisement.
	format := uint32(dmabuf.FormatXRGB8888)
	if currentWindow.ui.Host.TransparentBackground {
		format = dmabuf.FormatARGB8888
	}
	if t.Format() != format || !slices.Contains(currentWindow.gpuModifiers, t.Modifier()) {
		return nil, fmt.Errorf("unadvertised DMA-BUF format/modifier %#x/%#x", t.Format(), t.Modifier())
	}
	p, err := linuxDmabuf.CreateParams()
	if err != nil {
		return nil, err
	}
	mod := t.Modifier()
	for i := 0; i < t.Planes(); i++ {
		fd, off, stride := t.Plane(i)
		if err := p.Add(uintptr(fd), uint32(i), uint32(off), uint32(stride),
			uint32(mod>>32), uint32(mod)); err != nil {
			p.Destroy()
			return nil, err
		}
	}
	w, h := t.Size()
	if err := p.Create(int32(w), int32(h), t.Format(), 0); err != nil {
		p.Destroy()
		return nil, err
	}
	if err := wlclient.DisplayRoundtrip(disp); err != nil {
		p.Destroy()
		return nil, err
	}
	buf, err := p.Buffer()
	p.Destroy()
	if err != nil {
		return nil, fmt.Errorf("%w (format=%#x modifier=%#x planes=%d)",
			err, t.Format(), t.Modifier(), t.Planes())
	}
	return buf, nil
}
