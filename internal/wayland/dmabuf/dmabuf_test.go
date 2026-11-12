package dmabuf

import (
	"encoding/binary"
	"slices"
	"testing"

	"go.hasen.dev/shirei/internal/wayland/wl"
)

func TestFormatSpecificFeedback(t *testing.T) {
	var d Dmabuf
	d.init()
	// Version 3 advertises modifiers directly on the global.
	for _, entry := range []feedbackEntry{{FormatXRGB8888, 11}, {FormatARGB8888, ModInvalid}, {FormatARGB8888, ModInvalid}} {
		data := make([]byte, 12)
		binary.LittleEndian.PutUint32(data, entry.format)
		binary.LittleEndian.PutUint32(data[4:], uint32(entry.mod>>32))
		binary.LittleEndian.PutUint32(data[8:], uint32(entry.mod))
		d.Dispatch(&wl.Event{Opcode: 1, Data: data})
	}
	if !slices.Equal(d.Modifiers(FormatARGB8888), []uint64{ModInvalid}) || !slices.Equal(d.Modifiers(FormatXRGB8888), []uint64{11}) {
		t.Fatal("v3 modifiers mix formats or contain duplicates")
	}
	// Version 4 surface feedback replaces the modifier set. Compositing
	// tranches take priority over scanout, independently for each format.
	f := Feedback{parent: &d, table: []feedbackEntry{
		{FormatXRGB8888, 20}, {FormatARGB8888, 30},
		{FormatARGB8888, ModLinear}, {FormatXRGB8888, ModInvalid},
	}, scanMods: make(map[uint32][]uint64), otherMods: make(map[uint32][]uint64)}
	for _, tranche := range []struct {
		flags   uint32
		indices []uint16
	}{
		{trancheScanout, []uint16{0, 1, 2}},
		{0, []uint16{2, 3, 2, 99}},
	} {
		flags := make([]byte, 4)
		binary.LittleEndian.PutUint32(flags, tranche.flags)
		f.Dispatch(&wl.Event{Opcode: 6, Data: flags})
		n := len(tranche.indices) * 2
		data := make([]byte, 4+(n+3)/4*4)
		binary.LittleEndian.PutUint32(data, uint32(n))
		for i, idx := range tranche.indices {
			binary.LittleEndian.PutUint16(data[4+i*2:], idx)
		}
		f.Dispatch(&wl.Event{Opcode: 5, Data: data})
		f.Dispatch(&wl.Event{Opcode: 3})
	}
	if f.Done() {
		t.Fatal("feedback publishes before done")
	}
	f.Dispatch(&wl.Event{Opcode: 0})
	if !f.Done() {
		t.Fatal("feedback completion is not observable")
	}
	for format, want := range map[uint32][]uint64{
		FormatARGB8888: {ModLinear, 30}, FormatXRGB8888: {ModInvalid, 20},
	} {
		if got := d.Modifiers(format); !slices.Equal(got, want) {
			t.Fatalf("format %#x: got %v, want %v", format, got, want)
		}
		if !d.HasFormat(format) {
			t.Fatalf("format %#x unavailable", format)
		}
	}
}
