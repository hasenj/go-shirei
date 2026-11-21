package shirei

import (
	"strings"
	"testing"
)

// runFrameOn builds one frame for u rather than the active UI.
func runFrameOn(u *UI, fn FrameFn) {
	restore := EnterUI(u)
	RunFrameFnLocked(fn)
	restore()
}

// sendInputCmd applies one wire command through a listener's dispatch. Going
// through the same server type the UDP loop uses keeps the test deterministic
// without a socket.
func sendInputCmd(t *testing.T, server *inputCmdServer, line string) string {
	t.Helper()
	var resp string
	WithFrameLock(func() {
		resp = server.handleInput(line)
	})
	return resp
}

func requireReplyContains(t *testing.T, label, reply string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(reply, want) {
			t.Fatalf("%s: reply missing %q:\n%s", label, want, reply)
		}
	}
}

func TestWindowCommands(t *testing.T) {
	mainUI := NewUI()
	mainUI.Host.WindowSize = Vec2{400, 300}
	mainUI.Host.WindowTitle = "Test app"
	popupUI := NewUI()
	popupUI.Host.WindowSize = Vec2{200, 100}
	popupUI.Host.WindowTitle = "Busy alarm"
	popupUI.Host.WindowHandle = "busy_alarm_popup"

	unregisterMain := RegisterWindowUI(mainUI)
	defer unregisterMain()
	if err := ValidateWindowHandle("busy_alarm_popup"); err != nil {
		t.Fatalf("validate popup handle: %v", err)
	}
	unregisterPopup := RegisterWindowUI(popupUI)
	defer unregisterPopup()
	if err := ValidateWindowHandle("busy_alarm_popup"); err == nil {
		t.Fatal("duplicate window handle accepted")
	}

	thirdUI := NewUI()
	thirdUI.Host.WindowSize = Vec2{10, 20}
	unregisterThird := RegisterWindowUI(thirdUI)
	defer unregisterThird()

	windowRegistry.RLock()
	var thirdHandle windowHandle
	for _, record := range windowRegistry.list {
		if record.ui == thirdUI {
			thirdHandle = record.handle
		}
	}
	windowRegistry.RUnlock()
	if thirdHandle == "" || thirdHandle == "main" || thirdHandle == "busy_alarm_popup" {
		t.Fatalf("generated handle = %q", thirdHandle)
	}

	runFrameOn(popupUI, func() {
		Container(Attrs(FixSize(50, 20)), func() {
			NextAccessName("alarm_dismiss")
			AssignAccess()
		})
	})

	server := new(inputCmdServer)

	listing := sendInputCmd(t, server, "windows")
	requireReplyContains(t, "windows", listing,
		"count: 3",
		"id: main\nkind: main\ntitle: Test app\nsize: 400 300",
		"target: true",
		"id: busy_alarm_popup\nkind: popup\ntitle: Busy alarm\nsize: 200 100",
		"id: "+string(thirdHandle)+"\nkind: popup\nsize: 10 20",
	)

	if got := sendInputCmd(t, server, "window"); got != "main" {
		t.Fatalf("window target = %q, want main", got)
	}

	// The default target is the main window.
	if got := sendInputCmd(t, server, "move 1 2"); got != "" {
		t.Fatalf("move reply = %q", got)
	}
	if mainUI.Host.Input.MousePoint != (Vec2{1, 2}) {
		t.Fatalf("main mouse point = %v", mainUI.Host.Input.MousePoint)
	}

	if got := sendInputCmd(t, server, "window busy_alarm_popup"); got != "ok" {
		t.Fatalf("select popup = %q", got)
	}
	if got := sendInputCmd(t, server, "window"); got != "busy_alarm_popup" {
		t.Fatalf("window target = %q, want busy_alarm_popup", got)
	}
	if got := sendInputCmd(t, server, "count alarm_dismiss"); got != "1" {
		t.Fatalf("popup count = %q, want 1", got)
	}
	show := sendInputCmd(t, server, "show alarm_dismiss")
	requireReplyContains(t, "show", show, "path: alarm_dismiss", "rect: 0 0 50 20")

	// Injects land in the selected window, not the main one.
	if got := sendInputCmd(t, server, "move 12 34"); got != "" {
		t.Fatalf("move reply = %q", got)
	}
	if popupUI.Host.Input.MousePoint != (Vec2{12, 34}) {
		t.Fatalf("popup mouse point = %v", popupUI.Host.Input.MousePoint)
	}
	if mainUI.Host.Input.MousePoint != (Vec2{1, 2}) {
		t.Fatalf("main mouse point moved to %v", mainUI.Host.Input.MousePoint)
	}

	if got := sendInputCmd(t, server, "window main"); got != "ok" {
		t.Fatalf("select main = %q", got)
	}
	if got := sendInputCmd(t, server, "count alarm_dismiss"); got != "0" {
		t.Fatalf("main count = %q, want 0", got)
	}

	if got := sendInputCmd(t, server, "window nope"); !strings.HasPrefix(got, "error: no such window") {
		t.Fatalf("unknown window reply = %q", got)
	}
	if got := sendInputCmd(t, server, "window a b"); !strings.HasPrefix(got, "error: usage") {
		t.Fatalf("window usage reply = %q", got)
	}
	if got := sendInputCmd(t, server, "windows extra"); !strings.HasPrefix(got, "error: usage") {
		t.Fatalf("windows usage reply = %q", got)
	}

	// A selected window that closes reverts the target to the main window.
	if got := sendInputCmd(t, server, "window busy_alarm_popup"); got != "ok" {
		t.Fatalf("select popup = %q", got)
	}
	unregisterPopup()
	if got := sendInputCmd(t, server, "window"); got != "main" {
		t.Fatalf("target after close = %q, want main", got)
	}
	if got := sendInputCmd(t, server, "count alarm_dismiss"); got != "0" {
		t.Fatalf("count after close = %q, want 0", got)
	}
	listing = sendInputCmd(t, server, "windows")
	requireReplyContains(t, "windows after close", listing, "count: 2")
	if strings.Contains(listing, "busy_alarm_popup") {
		t.Fatalf("closed window still listed:\n%s", listing)
	}
}

func TestValidateWindowHandle(t *testing.T) {
	tests := []struct {
		handle string
		valid  bool
	}{
		{"", true},
		{"a", true},
		{"busy_alarm_popup", true},
		{"popup-1", true},
		{"a-b_c9", true},
		{"main", false},
		{"Main", false},
		{"1abc", false},
		{"with space", false},
		{"with/slash", false},
		{strings.Repeat("a", 32), true},
		{strings.Repeat("a", 33), false},
	}
	for _, test := range tests {
		err := ValidateWindowHandle(test.handle)
		if (err == nil) != test.valid {
			t.Errorf("ValidateWindowHandle(%q) = %v, valid=%v", test.handle, err, test.valid)
		}
	}
}
