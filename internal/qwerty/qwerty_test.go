package qwerty

import (
	"testing"

	"go.hasen.dev/shirei"
)

// both tables must cover exactly the same 47 physical keys: 26 letters,
// 10 digits, and the 11 punctuation keys of the writing block
func TestTablesAgree(t *testing.T) {
	if len(scanTable) != 47 || len(macTable) != 47 {
		t.Fatalf("table sizes: scan=%d mac=%d, want 47", len(scanTable), len(macTable))
	}
	scanSet := map[shirei.KeyCode]bool{}
	for _, kc := range scanTable {
		if scanSet[kc] {
			t.Fatalf("scanTable maps %q twice", kc)
		}
		scanSet[kc] = true
	}
	for vk, kc := range macTable {
		if !scanSet[kc] {
			t.Errorf("macTable vk %#x -> %q missing from scanTable", vk, kc)
		}
	}
}

// spot checks against the published constants: the same physical key must
// resolve to the same KeyCode from both code spaces
func TestKnownPositions(t *testing.T) {
	checks := []struct {
		scan, macVK uint16
		want        shirei.KeyCode
	}{
		{0x11, 0x0D, 'W'},  // second key, top letter row
		{0x1E, 0x00, 'A'},  // first key, home row
		{0x27, 0x29, ';'},  // right of L
		{0x28, 0x27, '\''}, // right of ;
		{0x2B, 0x2A, '\\'},
		{0x06, 0x17, '5'},
		{0x0D, 0x18, '='},
		{0x35, 0x2C, '/'},
	}
	for _, c := range checks {
		if got := FromScan(c.scan); got != c.want {
			t.Errorf("FromScan(%#x) = %q, want %q", c.scan, got, c.want)
		}
		if got := FromMacVK(c.macVK); got != c.want {
			t.Errorf("FromMacVK(%#x) = %q, want %q", c.macVK, got, c.want)
		}
	}
}
