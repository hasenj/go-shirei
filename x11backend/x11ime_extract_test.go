//go:build linux

package x11backend

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestExtractIBusTextPlainString(t *testing.T) {
	if got := extractIBusText("にほんご"); got != "にほんご" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractIBusTextVariant(t *testing.T) {
	v := dbus.MakeVariant("かな")
	if got := extractIBusText(v); got != "かな" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractIBusTextNestedStruct(t *testing.T) {
	// Approximate IBus.Text nesting: type name + payload string.
	body := []interface{}{
		"IBusText",
		[]interface{}{
			map[string]dbus.Variant{},
			"日本語",
			[]interface{}{},
		},
	}
	if got := extractIBusText(body); got != "日本語" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractIBusTextIgnoresTypeName(t *testing.T) {
	body := []interface{}{"IBusText", "に"}
	if got := extractIBusText(body); got != "に" {
		t.Fatalf("got %q", got)
	}
}
