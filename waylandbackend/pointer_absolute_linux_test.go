//go:build linux

package waylandbackend

import (
	"encoding/binary"
	"testing"
	"time"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/internal/wayland/layershell"
	"go.hasen.dev/shirei/internal/wayland/wl"
	"go.hasen.dev/shirei/internal/wayland/wlclient"
)

func TestAbsolutePointerTravelAcrossQueuedWindowMoves(t *testing.T) {
	ctx, peer := controlWire(t)
	oldDisp, oldPointer, oldRelative := disp, pointer, relativePointerObject
	oldWindow, oldCurrent, oldOutputs := pointerWindow, currentWindow, outputs
	oldSerial, oldLast, oldSource, oldActivation := pointerSerial, lastSerial, activationSource, activationSerial
	t.Cleanup(func() {
		disp, pointer, relativePointerObject = oldDisp, oldPointer, oldRelative
		pointerWindow, currentWindow, outputs = oldWindow, oldCurrent, oldOutputs
		pointerSerial, lastSerial, activationSource, activationSerial = oldSerial, oldLast, oldSource, oldActivation
	})
	disp = ctx.LookupProxy(1).(*wl.Display)
	pointer = wl.NewPointer(ctx)
	wlclient.PointerAddListener(pointer, h)
	relativePointerObject = nil // Absolute input works without relative-pointer events or support.
	w := newWindowState(shirei.NewUI())
	w.surface, w.layerSurface = &wl.Surface{}, &layershell.Surface{}
	ctx.Register(w.surface)
	ctx.Register(w.layerSurface)
	w.csdEnabled = false
	w.winW, w.winH = 520, 360
	w.position = &shirei.Vec2{40, 80}
	w.surfaceStateDirty = true
	pointerWindow, currentWindow, outputs = w, w, nil
	c := Context{window: w}
	send := func(proxy wl.Proxy, opcode uint32, words ...uint32) {
		t.Helper()
		data := make([]byte, 8+len(words)*4)
		binary.LittleEndian.PutUint32(data, uint32(proxy.Id()))
		binary.LittleEndian.PutUint32(data[4:], uint32(len(data))<<16|opcode)
		for i, word := range words {
			binary.LittleEndian.PutUint32(data[8+4*i:], word)
		}
		if _, err := peer.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := ctx.RunTimeout(time.Second); err != nil {
			t.Fatal(err)
		}
	}
	properties := func() {
		t.Helper()
		for _, op := range []uint32{0, 1, 3, 2} {
			readControl(t, peer, w.layerSurface, op)
		}
	}
	check := func(expected shirei.Vec2) {
		t.Helper()
		if travel, ok := c.PointerTravel(); !ok || travel != expected {
			t.Fatalf("absolute pointer displacement %v, %v; want %v", travel, ok, expected)
		}
	}
	if err := commitWindowSurface(); err != nil {
		t.Fatal(err)
	}
	properties()
	readControl(t, peer, w.surface, 6)
	w.havePresented = true
	send(pointer, 2, 1, 120*256, 55*256)
	send(pointer, 3, 1, 1, btnLeft, wl.PointerButtonStatePressed)
	check(shirei.Vec2{})
	send(pointer, 2, 2, 128*256, 58*256)
	check(shirei.Vec2{8, 3})
	move := func(origin shirei.Vec2) *wl.Callback {
		t.Helper()
		w.position = &origin
		w.surfaceStateDirty = true
		if err := commitWindowSurface(); err != nil {
			t.Fatal(err)
		}
		properties()
		sync := readControl(t, peer, disp, 0)
		callback := ctx.LookupProxy(wl.ProxyId(binary.LittleEndian.Uint32(sync))).(*wl.Callback)
		readControl(t, peer, w.surface, 6)
		return callback
	}
	first := move(shirei.Vec2{48, 83})
	// Input already queued by the compositor still describes the old origin.
	send(pointer, 2, 3, 136*256, 61*256)
	check(shirei.Vec2{16, 6})
	send(first, 0, 1)
	// Repositioning alone changes local coordinates, not pointer displacement.
	send(pointer, 2, 4, 128*256, 58*256)
	check(shirei.Vec2{16, 6})
	send(pointer, 2, 5, 136*256, 61*256)
	check(shirei.Vec2{24, 9})
	second := move(shirei.Vec2{64, 89})
	send(pointer, 2, 6, 136*256, 61*256)
	check(shirei.Vec2{24, 9})
	send(second, 0, 2)
	send(pointer, 2, 7, 120*256, 55*256)
	check(shirei.Vec2{24, 9})
	send(pointer, 2, 8, 128*256, 58*256)
	check(shirei.Vec2{32, 12})
	// A marker for a retired native surface cannot rebase its replacement.
	late := move(shirei.Vec2{72, 92})
	w.surface = &wl.Surface{}
	ctx.Register(w.surface)
	send(late, 0, 3)
	if w.pointerOrigin != (shirei.Vec2{64, 89}) {
		t.Fatal("old surface marker changes the replacement's origin")
	}
}
