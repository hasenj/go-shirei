package shirei

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.hasen.dev/udplib"
)

func TestInputCommandMoveMouseKeyText(t *testing.T) {
	ResetInputSession()
	ui.Host.Input.MousePoint = Vec2{10, 10}

	if got := HandleInputCommand("move 40 50"); got != "" {
		t.Fatalf("move: %q", got)
	}
	if ui.Host.Input.MousePoint != (Vec2{40, 50}) {
		t.Fatalf("MousePoint=%v", ui.Host.Input.MousePoint)
	}
	if ui.Host.FrameInput.Motion != (Vec2{30, 40}) {
		t.Fatalf("Motion=%v", ui.Host.FrameInput.Motion)
	}

	if got := HandleInputCommand("mouse_down"); got != "" {
		t.Fatalf("mouse_down: %q", got)
	}
	if ui.Host.FrameInput.Mouse != MouseClick {
		t.Fatal("mouse_down did not set MouseClick")
	}
	if ui.Host.Input.MouseButton != MousePrimary {
		t.Fatalf("MouseButton=%v", ui.Host.Input.MouseButton)
	}

	if got := HandleInputCommand("mouse_up secondary"); got != "" {
		t.Fatalf("mouse_up: %q", got)
	}
	if ui.Host.FrameInput.Mouse != MouseRelease {
		t.Fatal("mouse_up did not set MouseRelease")
	}
	if ui.Host.Input.MouseButton != MouseSecondary {
		t.Fatalf("MouseButton=%v want secondary", ui.Host.Input.MouseButton)
	}

	if got := HandleInputCommand("wheel 24"); got != "" {
		t.Fatalf("wheel: %q", got)
	}
	if ui.Host.FrameInput.Scroll != (Vec2{0, 24}) {
		t.Fatalf("Scroll=%v", ui.Host.FrameInput.Scroll)
	}

	if got := HandleInputCommand("key_down Tab shift"); got != "" {
		t.Fatalf("key_down: %q", got)
	}
	if ui.Host.FrameInput.Key != KeyTab {
		t.Fatalf("Key=%v", ui.Host.FrameInput.Key)
	}
	if ui.Host.Input.Modifiers&ModShift == 0 {
		t.Fatal("shift not set")
	}
	if !slices.Contains(ui.Host.Input.DownKeys, KeyTab) {
		t.Fatalf("DownKeys=%v", ui.Host.Input.DownKeys)
	}

	if got := HandleInputCommand("key_up Tab"); got != "" {
		t.Fatalf("key_up: %q", got)
	}
	if ui.Host.FrameInput.Key != 0 {
		t.Fatalf("key_up set Key=%v", ui.Host.FrameInput.Key)
	}
	if slices.Contains(ui.Host.Input.DownKeys, KeyTab) {
		t.Fatalf("DownKeys still has Tab: %v", ui.Host.Input.DownKeys)
	}

	if got := HandleInputCommand("move 1 1"); got != "" {
		t.Fatalf("move after key: %q", got)
	}
	if ui.Host.Input.Modifiers != 0 {
		t.Fatalf("modifiers stuck after move: %v", ui.Host.Input.Modifiers)
	}

	if got := HandleInputCommand(`text "ab"`); got != "" {
		t.Fatalf("text: %q", got)
	}
	if ui.Host.FrameInput.Text != "ab" {
		t.Fatalf("Text=%q", ui.Host.FrameInput.Text)
	}

	if got := HandleInputCommand("park"); !strings.HasPrefix(got, "error: unknown") {
		t.Fatalf("park should be gone: %q", got)
	}
	if got := HandleInputCommand("key Tab"); !strings.HasPrefix(got, "error: unknown") {
		t.Fatalf("key should be gone: %q", got)
	}
	if got := HandleInputCommand("tick"); !strings.HasPrefix(got, "error: unknown") {
		t.Fatalf("tick should be gone: %q", got)
	}
}

func TestInputCommandShowQuery(t *testing.T) {
	ResetInputSession()
	ui.Host.WindowSize = Vec2{400, 300}
	RunFrameFn(func() {
		NextAccessName("top_bar")
		Container(Attrs(Row, FixSize(400, 40)), func() {
			AssignAccess()
			NextAccessName("files_browser")
			Container(Attrs(FixSize(80, 30)), func() {
				NextAccessRole("button")
				AssignAccess()
			})
		})
	})

	got := HandleInputCommand("show top_bar files_browser")
	if !strings.Contains(got, "id: #") {
		t.Fatalf("show id:\n%s", got)
	}
	if !strings.Contains(got, "path: top_bar files_browser") {
		t.Fatalf("show path:\n%s", got)
	}
	if !strings.Contains(got, "role: button") {
		t.Fatalf("show role:\n%s", got)
	}
	if !strings.Contains(got, "rect:") {
		t.Fatalf("show rect:\n%s", got)
	}

	q := HandleInputCommand("query files_browser")
	if !strings.HasPrefix(q, "count: 1\n") || !strings.Contains(q, "path: top_bar files_browser") {
		t.Fatalf("query:\n%s", q)
	}
	if got := HandleInputCommand("count files_browser"); got != "1" {
		t.Fatalf("count files_browser: %q", got)
	}
	if got := HandleInputCommand("count nope"); got != "0" {
		t.Fatalf("count nope: %q", got)
	}

	all := HandleInputCommand("query")
	if !strings.HasPrefix(all, "count: ") || !strings.Contains(all, "path: top_bar") || !strings.Contains(all, "files_browser") {
		t.Fatalf("query all:\n%s", all)
	}

	if got := HandleInputCommand("show nope"); got != "error: no match" {
		t.Fatalf("missing: %q", got)
	}
	if got := HandleInputCommand("ping"); got != "pong" {
		t.Fatalf("ping: %q", got)
	}
	if got := HandleInputCommand("frob"); !strings.HasPrefix(got, "error: unknown") {
		t.Fatalf("unknown: %q", got)
	}
}

func TestInputCommandScreenshot(t *testing.T) {
	ResetInputSession()
	ui.FrameNumber = 0
	ui.Host.WindowSize = Vec2{}
	ui.Host.WindowScale = 0
	if got := HandleInputCommand("screenshot"); got != "error: usage: screenshot <path>" {
		t.Fatalf("usage: %q", got)
	}
	if got := HandleInputCommand("screenshot /tmp/x.png"); got != "error: no frame" {
		t.Fatalf("no frame: %q", got)
	}

	ui.Host.WindowSize = Vec2{80, 40}
	ui.Host.WindowScale = 1
	RunFrameFn(func() {
		Container(Attrs(FixSize(80, 40), Background(200, 80, 40, 1)), func() {})
	})
	path := filepath.Join(t.TempDir(), "shot.png")
	if got := HandleInputCommand("screenshot " + path); got != "ok" {
		t.Fatalf("screenshot: %q", got)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	if b.Dx() != 80 || b.Dy() != 40 {
		t.Fatalf("png size %dx%d want 80x40", b.Dx(), b.Dy())
	}
}

func TestClipUDPReplyWholeRecords(t *testing.T) {
	rec := "path: proc\nrect: 1 2 3 4\nrole: running\nvalue: 1"
	raw := strings.Repeat(rec+"\n\n", 40)
	got := string(clipUDPReply(raw))
	if len(got) > maxUDPReply {
		t.Fatalf("len %d > %d", len(got), maxUDPReply)
	}
	if !strings.HasSuffix(got, rec) {
		t.Fatalf("cut mid-record:\n%s", got[max(0, len(got)-80):])
	}
	for _, c := range strings.Split(got, "\n\n") {
		if !strings.Contains(c, "rect:") || !strings.Contains(c, "value:") {
			t.Fatalf("incomplete record %q", c)
		}
	}
}

func TestJoinAccessNodesFitsUDP(t *testing.T) {
	nodes := make([]AccessNode, 40)
	for i := range nodes {
		nodes[i] = AccessNode{
			AccessAttrs: AccessAttrs{Name: "proc", Role: "running", Value: "12345"},
			Path:        "proc",
			Rect:        Rect{Origin: Vec2{10, 20}, Size: Vec2{800, 22}},
		}
	}
	got := joinAccessNodes(nodes, maxUDPReply)
	if len(got) > maxUDPReply {
		t.Fatalf("len %d > %d", len(got), maxUDPReply)
	}
	n := strings.Count(got, "path: proc")
	if n < 2 {
		t.Fatalf("packed %d records", n)
	}
	if strings.Count(got, "path: proc") != strings.Count(got, "rect:") {
		t.Fatal("torn record")
	}
}

func TestFormatQueryReplyCount(t *testing.T) {
	nodes := make([]AccessNode, 40)
	for i := range nodes {
		nodes[i] = AccessNode{
			AccessAttrs: AccessAttrs{Name: "proc", Role: "running", Value: "12345"},
			Path:        "proc",
			Rect:        Rect{Origin: Vec2{10, 20}, Size: Vec2{800, 22}},
		}
	}
	got := formatQueryReply(nodes)
	if !strings.HasPrefix(got, "count: 40\n") {
		t.Fatalf("head: %q", got[:min(40, len(got))])
	}
	packed := strings.Count(got, "path: proc")
	if packed >= 40 {
		t.Fatalf("expected packed < 40, got %d", packed)
	}
	if packed < 2 {
		t.Fatalf("packed %d", packed)
	}
}

func TestInputCommandUDP(t *testing.T) {
	port, err := udplib.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	AcceptInputCommands(port)
	resp, ok := udplib.SendUDPMessageString(port, "ping")
	if !ok || resp != "pong" {
		t.Fatalf("udp ping = %q ok=%v", resp, ok)
	}
	resp, ok = udplib.SendUDPMessageString(port, "move 1 2")
	if !ok {
		t.Fatal("udp move: no datagram")
	}
	if resp != "" {
		t.Fatalf("udp move = %q, want empty", resp)
	}
}

func TestInputCommandShowFocused(t *testing.T) {
	ResetInputSession()
	ui.Host.WindowSize = Vec2{400, 300}
	var id ContainerId
	RunFrameFn(func() {
		id = Container(Attrs(Focusable, FixSize(20, 20)), func() {
			NextAccessName("field")
			AssignAccess()
		})
		FocusImmediateOn(id)
	})
	n, ok := QueryContainer("field")
	if !ok {
		t.Fatal("field")
	}
	got := HandleInputCommand("focused")
	leaf := fmt.Sprintf("#%d", n.ID)
	ids := strings.Fields(got)
	if len(ids) == 0 || ids[0] != leaf {
		t.Fatalf("focused leaf: got %q want first %s", got, leaf)
	}
	shown := HandleInputCommand("show " + leaf)
	if !strings.Contains(shown, "path: field") || !strings.Contains(shown, "focused: true") {
		t.Fatalf("show %s:\n%s", leaf, shown)
	}
	if got := HandleInputCommand("show focused"); got != "error: no match" {
		t.Fatalf("show focused is a name lookup, got:\n%s", got)
	}
}

func TestInputKeyPunctuation(t *testing.T) {
	for _, name := range []string{"-", "=", "[", "]", "\\", ";", "'", "`", ",", ".", "/"} {
		if code, ok := lookupInputKey(name); !ok || code != KeyCode(name[0]) {
			t.Errorf("lookupInputKey(%q) = %q, %v", name, code, ok)
		}
	}
}
