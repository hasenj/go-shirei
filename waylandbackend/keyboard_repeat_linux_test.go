//go:build linux

package waylandbackend

import (
	"testing"
	"time"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/internal/wayland/wl"
	xkb "go.hasen.dev/shirei/internal/wayland/xkbcommon"
)

func TestKeyboardRepeatRoutingModifiersAndComposition(t *testing.T) {
	oldContext, oldKeymap, oldState := xkbContext, xkbKeymap, xkbState
	oldWindow, oldRepeat, oldRate, oldDelay := keyboardWindow, repeatKey, repeatRate, repeatDelay
	oldSource, oldSerial, oldMask, oldLast := activationSource, activationSerial, seatModifierMask, lastSerial
	t.Cleanup(func() {
		xkbContext, xkbKeymap, xkbState = oldContext, oldKeymap, oldState
		keyboardWindow, repeatKey, repeatRate, repeatDelay = oldWindow, oldRepeat, oldRate, oldDelay
		activationSource, activationSerial, seatModifierMask, lastSerial = oldSource, oldSerial, oldMask, oldLast
	})
	xkbContext = xkb.ContextNew(xkb.ContextNoFlags)
	keymap := []byte(`xkb_keymap {
 xkb_keycodes { include "evdev+aliases(qwerty)" };
 xkb_types { include "complete" };
 xkb_compatibility { include "complete" };
 xkb_symbols { include "pc+us" };
 };` + "\x00")
	xkbKeymap = xkbContext.KeymapNewFromString(keymap, xkb.KeymapFormatTextV1, 0)
	if xkbKeymap == nil {
		t.Fatal("cannot compile keyboard test keymap")
	}
	xkbState = xkbKeymap.StateNew()
	if xkbState == nil {
		t.Fatal("cannot create keyboard state")
	}
	w := newWindowState(shirei.NewUI())
	keyboardWindow = w
	h.HandleKeyboardRepeatInfo(wl.KeyboardRepeatInfoEvent{Rate: 25, Delay: 300})
	h.HandleKeyboardKey(wl.KeyboardKeyEvent{Key: 30, State: wl.KeyboardKeyStatePressed})
	first := repeatKey.next
	if w.pendingText != "a" || repeatKey.window != w {
		t.Fatal("press does not establish a repeat target")
	}
	pumpKeyboardRepeat(first.Add(-time.Nanosecond))
	if w.pendingText != "a" {
		t.Fatal("repeat ignores its initial delay")
	}
	pumpKeyboardRepeat(first)
	if w.pendingText != "aa" {
		t.Fatal("held printable key does not repeat")
	}
	h.HandleKeyboardModifiers(wl.KeyboardModifiersEvent{ModsDepressed: 1})
	pumpKeyboardRepeat(repeatKey.next)
	if w.pendingText != "aaA" {
		t.Fatalf("repeat does not use current layout/modifiers: %q", w.pendingText)
	}
	w.ui.Host.Input.Composition = "かな"
	pumpKeyboardRepeat(repeatKey.next)
	if w.pendingText != "aaA" {
		t.Fatal("repeat inserts text during IME composition")
	}
	w.ui.Host.Input.Composition = ""
	h.HandleKeyboardKey(wl.KeyboardKeyEvent{Key: 30, State: wl.KeyboardKeyStateReleased})
	pumpKeyboardRepeat(first.Add(time.Second))
	if repeatKey.window != nil || w.pendingText != "aaA" {
		t.Fatal("key release leaves a repeat armed")
	}
	h.HandleKeyboardKey(wl.KeyboardKeyEvent{Key: 30, State: wl.KeyboardKeyStatePressed})
	keyboardWindow = newWindowState(shirei.NewUI())
	pumpKeyboardRepeat(repeatKey.next)
	if repeatKey.window != nil || keyboardWindow.pendingText != "" {
		t.Fatal("repeat crosses focus between windows")
	}
	keyboardWindow = w
	h.HandleKeyboardKey(wl.KeyboardKeyEvent{Key: 30, State: wl.KeyboardKeyStatePressed})
	w.hidden = true
	pumpKeyboardRepeat(repeatKey.next)
	if repeatKey.window != nil {
		t.Fatal("hidden window retains key repeat")
	}
	w.hidden = false
	h.HandleKeyboardKey(wl.KeyboardKeyEvent{Key: 30, State: wl.KeyboardKeyStatePressed})
	h.HandleKeyboardRepeatInfo(wl.KeyboardRepeatInfoEvent{})
	if repeatKey.window != nil {
		t.Fatal("compositor cannot disable key repeat")
	}
}
