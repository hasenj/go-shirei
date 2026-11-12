//go:build linux

package waylandbackend

import (
	"encoding/binary"
	"testing"
	"time"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/internal/wayland/wl"
	"go.hasen.dev/shirei/internal/wayland/wlclient"
)

func TestPointerTravelIgnoresDelayedSurfaceMotion(t *testing.T) {
	ctx, peer := controlWire(t)
	oldManager, oldRelative, oldPointer, oldWindow := relativeManager, relativePointerObject, pointer, pointerWindow
	t.Cleanup(func() {
		relativeManager, relativePointerObject, pointer, pointerWindow = oldManager, oldRelative, oldPointer, oldWindow
	})
	relativeManager, relativePointerObject, pointer = &relativePointerManager{}, nil, nil
	ctx.Register(relativeManager)
	ensureRelativePointer() // Registry globals and seat capabilities can arrive in either order.
	if relativePointerObject != nil {
		t.Fatal("relative pointer exists before wl_pointer")
	}
	pointer = wl.NewPointer(ctx)
	wlclient.PointerAddListener(pointer, h)
	ensureRelativePointer()
	get := readControl(t, peer, relativeManager, 1)
	if len(get) != 8 || binary.LittleEndian.Uint32(get[4:]) != uint32(pointer.Id()) {
		t.Fatal("relative pointer does not use the seat pointer")
	}
	object := ctx.LookupProxy(wl.ProxyId(binary.LittleEndian.Uint32(get)))
	ensureRelativePointer()
	if object != relativePointerObject {
		t.Fatal("repeated seat updates replace the relative pointer")
	}
	a, b := newWindowState(shirei.NewUI()), newWindowState(shirei.NewUI())
	pointerWindow = a
	c := Context{window: a}
	send := func(proxy wl.Proxy, opcode uint32, words ...uint32) {
		t.Helper()
		data := make([]byte, 8+4*len(words))
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
	// Pointer movement before a position request, followed by compositor motion
	// that describes different surface origins, must count the physical delta once.
	send(pointer, 2, 1, 100*256, 60*256)
	send(relativePointerObject, 0, 0, 1000, 20*256, 10*256, 20*256, 10*256)
	send(pointer, 2, 2, 80*256, 50*256)
	send(pointer, 2, 3, 100*256, 60*256)
	if travel, ok := c.PointerTravel(); !ok || travel != (shirei.Vec2{20, 10}) {
		t.Fatalf("surface movement changes physical pointer travel: %v, %v", travel, ok)
	}
	// The protocol does not guarantee absolute/relative event order.
	send(relativePointerObject, 0, 0, 2000, 12*256, 4*256, 12*256, 4*256)
	send(pointer, 2, 4, 92*256, 54*256)
	send(pointer, 2, 5, 80*256, 50*256)
	if travel, _ := c.PointerTravel(); travel != (shirei.Vec2{32, 14}) {
		t.Fatalf("event ordering changes pointer travel: %v", travel)
	}
	pointerWindow = b
	send(relativePointerObject, 0, 0, 3000, 128, 64, 128, 64)
	if _, ok := c.PointerTravel(); ok {
		t.Fatal("unfocused window supplies drag motion")
	}
	if travel, ok := (Context{window: b}).PointerTravel(); !ok || travel != (shirei.Vec2{.5, .25}) || a.pointerTravel != (shirei.Vec2{32, 14}) {
		t.Fatal("fractional motion crosses window focus")
	}
	b.closed = true
	send(relativePointerObject, 0, 0, 4000, 256, 256, 256, 256)
	if b.pointerTravel != (shirei.Vec2{.5, .25}) {
		t.Fatal("late relative motion revives a closed window")
	}
	pointerWindow = nil
	send(relativePointerObject, 0, 0, 5000, 256, 256, 256, 256)
}
