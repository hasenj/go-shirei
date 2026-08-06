package main

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"go.hasen.dev/shirei"

	"go.hasen.dev/shirei/examples/ferry/remote"
	"go.hasen.dev/shirei/examples/ferry/remote/remotetest"
)

// resetApp gives each test a fresh app state (tests share the global).
func resetApp() {
	appData = &AppState{splitRatio: 0.5, connectErrs: map[string]error{}}
}

func waitFor(t *testing.T, max time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal(msg)
}

func checkSnap(t *testing.T, r shirei.SnapResult) {
	t.Helper()
	switch {
	case r.Status == shirei.SnapSkip:
		t.Skip(r.Reason)
	case r.Err != nil:
		t.Fatal(r.Err)
	case r.Status == shirei.SnapMismatch:
		t.Errorf("render does not match snapshot %s; wrote %s",
			shirei.SnapAbsPath(r.Golden), shirei.SnapAbsPath(r.Actual))
	case r.Status == shirei.SnapCreated:
		t.Logf("created snapshot %s; review it and commit it", shirei.SnapAbsPath(r.Golden))
	}
}

// snapshot is like shirei.Snapshot, but loads ferry icon fonts and the delete
// stamp before rendering (those must run outside RenderToImage). Emits
// SHIREI_SNAP_REPORT lines via ReportSnap for shirei_tester.
func snapshot(t *testing.T, name string, w, h int, fn shirei.FrameFn) {
	t.Helper()
	shirei.InitFontSubsystem()
	ensureIconFonts()
	ensureDeleteStamp()
	shaped := shirei.ShapeText("alpha", shirei.DefaultTextStyle())
	if len(shaped.Lines) != 1 || len(shaped.Lines[0].Segments) == 0 {
		r := shirei.SnapResult{Name: name, Status: shirei.SnapSkip, Reason: "no usable system fonts for text shaping"}
		shirei.ReportSnap(t.Name(), r)
		t.Skip(r.Reason)
	}

	scope := new(int) // fresh identity per invocation, stable across settle frames
	img := shirei.RenderToImage(w, h, func() {
		shirei.ContainerWithKey(scope, shirei.Attrs(shirei.Viewport), fn)
	})

	path := filepath.Join("testdata", "snapshots", name+".png")
	r := shirei.CompareImage(name, path, img)
	shirei.ReportSnap(t.Name(), r)
	checkSnap(t, r)
}

// --- deterministic fake FS for goldens ---------------------------------

// fakeFS serves a fixed tree rooted at /home/demo, so goldens don't depend
// on the machine's real home directory.
type fakeFS struct {
	files map[string]string // path -> content; dirs are implied by paths
}

// demoPNG is a deterministic little gradient for image-preview coverage.
func demoPNG() string {
	img := image.NewRGBA(image.Rect(0, 0, 96, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 96; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 255 / 95), uint8(y * 255 / 63), 160, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.String()
}

func demoPaneFS() PaneFS {
	demo := fakeFS{files: map[string]string{
		"/home/demo/.profile":               "# hidden unless the toggle is on\n",
		"/home/demo/readme.txt":             "ferry demo fixture\n\nThis file previews in the bottom panel.\nTabs, unicode — 日本語 — and long lines all render through LargeText.\n",
		"/home/demo/photo.png":              demoPNG(),
		"/home/demo/data.bin":               "\x00\x01binary\x00bytes",
		"/home/demo/notes/ideas.md":         "# ideas\n\n- two panes\n- one ferry\n",
		"/home/demo/projects/todo.txt":      "ship phase 1\n",
		"/home/demo/projects/ferry/main.go": "package main\n\nfunc main() {}\n",
	}}
	return PaneFS{Label: "Local", Home: "/home/demo", List: demo.List, ReadHead: demo.ReadHead}
}

func (f fakeFS) List(dir string) ([]remote.Entry, error) {
	prefix := dir + "/"
	seen := map[string]remote.Entry{}
	var names []string
	for p, content := range f.files {
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		rest := strings.TrimPrefix(p, prefix)
		name, deeper, _ := strings.Cut(rest, "/")
		if _, ok := seen[name]; ok {
			continue
		}
		modTime := time.Date(2026, 6, 15, 9, 0, 0, 0, time.UTC).Add(time.Duration(len(name)) * time.Hour)
		e := remote.Entry{Name: name, Mode: 0o644, Size: int64(len(content)), ModTime: modTime}
		if deeper != "" {
			e = remote.Entry{Name: name, Mode: fs.ModeDir | 0o755, ModTime: modTime}
		}
		seen[name] = e
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil, fs.ErrNotExist
	}
	entries := make([]remote.Entry, 0, len(names))
	for _, n := range names {
		entries = append(entries, seen[n])
	}
	return entries, nil
}

func (f fakeFS) ReadHead(p string, n int) ([]byte, error) {
	content, ok := f.files[p]
	if !ok {
		return nil, fs.ErrNotExist
	}
	if len(content) > n {
		content = content[:n]
	}
	return []byte(content), nil
}

func rowNamed(t *testing.T, p *Pane, name string) *FileRow {
	t.Helper()
	for _, r := range p.Rows {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("no row %q in %s", name, p.CWD)
	return nil
}

// TestSnapshotShell is the phase-1 golden: two panes over the splitter,
// left dived into a subdirectory (".." row showing) with a text preview,
// right at its root with a binary selection.
func TestSnapshotShell(t *testing.T) {
	resetApp()
	appData.left = newPane(demoPaneFS(), true)
	right := setActiveSession(&remote.Conn{}, "ferry-a", newPane(demoPaneFS(), true)).Pane

	left := appData.left
	left.enter(rowNamed(t, left, "projects"))
	if left.CWD != "/home/demo/projects" {
		t.Fatalf("dive failed, cwd = %s", left.CWD)
	}
	left.selectRow(rowNamed(t, left, "todo.txt"))
	if left.Preview.Err != nil || left.Preview.Binary {
		t.Fatalf("preview fixture broken: %+v", left.Preview)
	}

	right.selectRow(rowNamed(t, right, "photo.png"))
	if right.Preview.Img == nil || right.Preview.Err != nil {
		t.Fatalf("photo.png should preview as an image: %+v", right.Preview)
	}

	snapshot(t, "shell_two_panes", 1200, 800, RootView)
}

// TestSortColumns pins the dirs-first invariant under every sort key and
// the direction toggle.
func TestSortColumns(t *testing.T) {
	p := newPane(demoPaneFS(), true)
	names := func() (out []string) {
		for _, r := range p.Rows {
			out = append(out, r.Name)
		}
		return
	}
	assertDirsFirst := func() {
		t.Helper()
		seenFile := false
		for _, r := range p.Rows {
			if !r.IsDir {
				seenFile = true
			} else if seenFile {
				t.Fatalf("dir after file: %v", names())
			}
		}
	}

	want := []string{"notes", "projects", ".profile", "data.bin", "photo.png", "readme.txt"}
	if got := strings.Join(names(), " "); got != strings.Join(want, " ") {
		t.Fatalf("default name sort: %v", got)
	}

	p.setSort(SortBySize)
	assertDirsFirst()
	var prev int64 = -1
	for _, r := range p.Rows {
		if r.IsDir {
			continue
		}
		if r.Size < prev {
			t.Fatalf("size ascending violated: %v", names())
		}
		prev = r.Size
	}

	p.setSort(SortBySize) // same column again → descending
	if !p.SortDesc {
		t.Fatal("second click should toggle to descending")
	}
	assertDirsFirst()
	prev = 1 << 62
	for _, r := range p.Rows {
		if r.IsDir {
			continue
		}
		if r.Size > prev {
			t.Fatalf("size descending violated: %v", names())
		}
		prev = r.Size
	}

	p.setSort(SortByTime)
	if p.SortDesc {
		t.Fatal("new column should start ascending")
	}
	assertDirsFirst()
	var prevT time.Time
	for _, r := range p.Rows {
		if r.IsDir {
			continue
		}
		if r.ModTime.Before(prevT) {
			t.Fatalf("time ascending violated: %v", names())
		}
		prevT = r.ModTime
	}
}

// TestImagePreview: image files decode into the preview; binary
// non-images still sniff as binary.
func TestImagePreview(t *testing.T) {
	p := newPane(demoPaneFS(), true)

	p.selectRow(rowNamed(t, p, "photo.png"))
	pv := &p.Preview
	if pv.Err != nil || pv.Img == nil || pv.ImgId == 0 {
		t.Fatalf("image preview failed: %+v", pv)
	}
	if b := pv.Img.Bounds(); b.Dx() != 96 || b.Dy() != 64 {
		t.Fatalf("decoded size %dx%d, want 96x64", b.Dx(), b.Dy())
	}

	p.selectRow(rowNamed(t, p, "data.bin"))
	if !p.Preview.Binary || p.Preview.Img != nil {
		t.Fatalf("data.bin should sniff as binary: %+v", p.Preview)
	}
}

func demoHosts() []remote.Host {
	return []remote.Host{
		{Alias: "ferry-a", Hostname: "127.0.0.1", User: "hasen", Port: "2281"},
		{Alias: "ferry-b", Hostname: "127.0.0.1", User: "hasen", Port: "2282"},
		{Alias: "ferry-c", Hostname: "127.0.0.1", User: "hasen", Port: "2283"},
	}
}

// TestSnapshotServers is the first-screen golden: host list with one
// inline connect error.
func TestSnapshotServers(t *testing.T) {
	resetApp()
	appData.screen = ScreenServers
	appData.hosts = demoHosts()
	appData.connectErrs["ferry-b"] = errors.New("dial tcp 127.0.0.1:2282: connection refused")
	snapshot(t, "servers_screen", 1200, 800, RootView)
}

// TestSnapshotTabs pins the tab bar with several connections — the
// active one highlighted, a background one, and a dropped one (red dot).
func TestSnapshotTabs(t *testing.T) {
	resetApp()
	appData.left = newPane(demoPaneFS(), true)
	setActiveSession(&remote.Conn{}, "prod-a", newPane(demoPaneFS(), true))
	setActiveSession(&remote.Conn{}, "staging", newPane(demoPaneFS(), true))
	dropped := setActiveSession(&remote.Conn{}, "prod-b", newPane(demoPaneFS(), true))
	dropped.Disconnected = true
	activateSession(appData.sessions[1]) // "staging" is the active tab
	snapshot(t, "server_tabs", 1200, 800, RootView)
}

// TestSnapshotServersLong pins the fix for the clipped long list: with
// more hosts than fit, the card caps at the window height and the list
// scrolls inside it (scrollbar visible) instead of spilling past the
// bottom.
func TestSnapshotServersLong(t *testing.T) {
	resetApp()
	appData.screen = ScreenServers
	var hosts []remote.Host
	for i := 0; i < 24; i++ {
		hosts = append(hosts, remote.Host{
			Alias:    fmt.Sprintf("host-%02d", i),
			Hostname: fmt.Sprintf("10.0.0.%d", i),
			User:     "deploy",
			Port:     "22",
		})
	}
	appData.hosts = hosts
	snapshot(t, "servers_screen_long", 1200, 800, RootView)
}

// TestServersListScrolls drives a real wheel scroll over a long host list
// and asserts the viewport actually moved (the bug was that it couldn't).
func TestSnapshotHostKeyModal(t *testing.T) {
	resetApp()
	appData.screen = ScreenServers
	appData.hosts = demoHosts()
	appData.connecting = "ferry-b"
	appData.hostKeyReq = &HostKeyRequest{
		Addr:        "127.0.0.1:2282",
		Fingerprint: "SHA256:5f4dcc3b5aa765d61d8327deb882cf99demo",
		Answer:      make(chan bool, 1),
	}
	snapshot(t, "hostkey_modal", 1200, 800, RootView)
}

// TestSnapshotPasswordModal pins the password dialog on a retry (attempt
// 2 shows the wrong-password warning; the buffer renders masked).
func TestSnapshotPasswordModal(t *testing.T) {
	resetApp()
	appData.screen = ScreenServers
	appData.hosts = demoHosts()
	appData.connecting = "ferry-b"
	appData.passwordReq = &PasswordRequest{
		User:    "pwuser",
		Addr:    "127.0.0.1:2283",
		Attempt: 2,
		Buf:     "hunter2",
		Answer:  make(chan passwordAnswer, 1),
	}
	snapshot(t, "password_modal", 1200, 800, RootView)
}

// binDemoApp fabricates an established-looking session with two staged
// rows (one also selected, pinning the deep-red combo) for the delete
// goldens. The Conn is a hollow shell — snapshots never dial.
// TestSessionLifecycle pins the tab model: newest session is active,
// switching works, closing a background tab leaves the active one,
// closing the active tab activates a neighbor, and closing the last one
// returns to the servers screen.
func TestSessionLifecycle(t *testing.T) {
	resetApp()
	appData.left = newPane(demoPaneFS(), true)
	a := setActiveSession(&remote.Conn{}, "a", newPane(demoPaneFS(), true))
	b := setActiveSession(&remote.Conn{}, "b", newPane(demoPaneFS(), true))
	c := setActiveSession(&remote.Conn{}, "c", newPane(demoPaneFS(), true))
	if appData.active != c || len(appData.sessions) != 3 {
		t.Fatal("the newest session should be active")
	}
	activateSession(a)
	if appData.active != a || appData.activePane != a.Pane {
		t.Fatal("activate should switch both the session and the arrow-key pane")
	}
	closeSession(b) // a background tab
	if appData.active != a || len(appData.sessions) != 2 {
		t.Fatal("closing a background tab must not change the active one")
	}
	closeSession(a) // the active tab
	if appData.active != c || len(appData.sessions) != 1 {
		t.Fatalf("closing the active tab should activate a neighbor, got %v", appData.active)
	}
	closeSession(c) // the last one
	if appData.active != nil || len(appData.sessions) != 0 || appData.screen != ScreenServers {
		t.Fatal("closing the last tab returns to the servers screen")
	}
}

// TestPerSessionBin: each session's deletion bin is its own — staging in
// one must not touch another, and rowBinned only marks the active one.
func TestPerSessionBin(t *testing.T) {
	resetApp()
	appData.left = newPane(demoPaneFS(), true)
	a := setActiveSession(&remote.Conn{}, "a", newPane(demoPaneFS(), true))
	b := setActiveSession(&remote.Conn{}, "b", newPane(demoPaneFS(), true))

	activateSession(a)
	a.Pane.selectRow(rowNamed(t, a.Pane, "readme.txt"))
	stageDelete(a.Pane)
	if len(a.deleteBin) != 1 {
		t.Fatalf("stage should fill the active session's bin: %d", len(a.deleteBin))
	}
	if len(b.deleteBin) != 0 {
		t.Fatal("the other session's bin must stay empty")
	}
	if !rowBinned(a.Pane, rowNamed(t, a.Pane, "readme.txt")) {
		t.Fatal("the staged row should read as binned in its own session")
	}

	// switch to b: a's staged row must not read as binned for b's pane
	activateSession(b)
	if rowBinned(a.Pane, rowNamed(t, a.Pane, "readme.txt")) {
		t.Fatal("a's staged row must not mark while b is active")
	}
}

// closeAllSessions is a test cleanup that closes every open connection.
func closeAllSessions() {
	for _, s := range append([]*Session(nil), appData.sessions...) {
		closeSession(s)
	}
}

// setActiveSession wires an established session for tests (never dials) —
// the replacement for the old appData.server/right/serverAlias trio.
func setActiveSession(conn *remote.Conn, alias string, pane *Pane) *Session {
	pane.FS.Label = alias // real sessions label the pane with the alias
	s := &Session{Conn: conn, Alias: alias, Pane: pane}
	appData.sessions = append(appData.sessions, s)
	appData.active = s
	appData.activePane = pane
	appData.screen = ScreenMain
	return s
}

func binDemoApp(t *testing.T) {
	t.Helper()
	resetApp()
	appData.left = newPane(demoPaneFS(), true)
	s := setActiveSession(&remote.Conn{}, "ferry-a", newPane(demoPaneFS(), true))
	s.binned = map[string]bool{
		"/home/demo/readme.txt": true,
		"/home/demo/notes":      true,
		"/home/demo/projects":   true,
		"/home/demo/data.bin":   true,
	}
	s.deleteBin = []BinItem{
		{Path: "/home/demo/notes", IsDir: true},
		{Path: "/home/demo/projects", IsDir: true},
		{Path: "/home/demo/data.bin"},
		{Path: "/home/demo/readme.txt"},
	}
	rowNamed(t, s.Pane, "readme.txt").Selected = true
	s.Pane.lead = rowNamed(t, s.Pane, "readme.txt")
	s.Pane.refreshPreview() // previewRow drives the preview panel
}

// TestSnapshotDeleteBin pins the staged state: red stamped rows that
// stay in the listing, and the collapsed bin strip BELOW the preview —
// the two panels must not compete for the same slot.
func TestSnapshotDeleteBin(t *testing.T) {
	binDemoApp(t)
	snapshot(t, "delete_bin", 1200, 800, RootView)
}

// TestSnapshotDeleteBinExpanded pins the strip's open state: the
// virtual-list table of staged paths with per-item restore.
func TestSnapshotDeleteBinExpanded(t *testing.T) {
	binDemoApp(t)
	appData.active.binExpanded = true
	snapshot(t, "delete_bin_expanded", 1200, 800, RootView)
}

// TestSnapshotDeleteConfirm pins the cannot-be-undone dialog.
func TestSnapshotDeleteConfirm(t *testing.T) {
	binDemoApp(t)
	appData.deleteConfirm = true
	snapshot(t, "delete_confirm_modal", 1200, 800, RootView)
}

// TestSnapshotLeaveConfirm pins the other warning direction: leaving
// with staged deletions that never ran.
func TestSnapshotLeaveConfirm(t *testing.T) {
	binDemoApp(t)
	appData.leaveConfirm = true
	appData.closeTarget = appData.active
	snapshot(t, "leave_confirm_modal", 1200, 800, RootView)
}

// TestBinRestoreClick reproduces the 2026-07-05 field crash: clicking
// Restore fires in the middle of the frame that is rendering the bin's
// virtual list, shrinking the bin under the list's own closures — the
// frame must keep rendering the slice it started with (unstageDelete
// builds a new slice; the panel snapshots the header).
func TestGUIPasswordFlowHeadless(t *testing.T) {
	hostname, port := remotetest.StartPasswordServer(t, "sesame")
	host := remote.Host{Alias: "pwbox", Hostname: hostname, User: "tester", Port: port}

	resetApp()
	appData.screen = ScreenServers
	appData.hosts = []remote.Host{host}
	appData.knownHostsPath = filepath.Join(t.TempDir(), "known_hosts")
	appData.left = newPane(demoPaneFS(), true)

	shirei.WithFrameLock(func() { startConnect(host, "") })

	waitFor(t, 5*time.Second, func() bool {
		var req *HostKeyRequest
		shirei.WithFrameLock(func() { req = appData.hostKeyReq })
		if req != nil {
			req.Answer <- true
			return true
		}
		return false
	}, "first-contact prompt never appeared")

	var firstReq *PasswordRequest
	waitFor(t, 5*time.Second, func() bool {
		var req *PasswordRequest
		shirei.WithFrameLock(func() { req = appData.passwordReq })
		if req != nil {
			if req.Attempt != 1 {
				t.Fatalf("first prompt should be attempt 1, got %d", req.Attempt)
			}
			firstReq = req
			req.Answer <- passwordAnswer{password: "nope", ok: true}
			return true
		}
		return false
	}, "password prompt never appeared")

	waitFor(t, 5*time.Second, func() bool {
		var req *PasswordRequest
		shirei.WithFrameLock(func() { req = appData.passwordReq })
		if req == nil || req == firstReq {
			return false // answered request may linger until its goroutine wakes
		}
		if req.Attempt != 2 {
			t.Fatalf("retry prompt should be attempt 2, got %d", req.Attempt)
		}
		req.Answer <- passwordAnswer{password: "sesame", ok: true}
		return true
	}, "retry prompt never appeared")

	waitFor(t, 5*time.Second, func() bool {
		var done bool
		shirei.WithFrameLock(func() {
			if err := appData.connectErrs[host.Alias]; err != nil {
				t.Errorf("connect failed: %v", err)
				done = true
				return
			}
			done = appData.screen == ScreenMain && appData.active != nil
		})
		return done
	}, "connect never completed")
	if t.Failed() {
		t.FailNow()
	}
	defer closeAllSessions()

	// the pane really is the password-auth'd sftp session
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "via_password.txt"), []byte("in\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var pane *Pane
	shirei.WithFrameLock(func() {
		pane = appData.active.Pane
		pane.syncLoad = true
		pane.CWD = dir
		pane.reload("")
	})
	rowNamed(t, pane, "via_password.txt")
}

// TestConnectFlowHeadless drives the real connect state machine against
// the sshd fixture: dial → first-contact prompt → main screen → sftp
// listing → remote preview.
func TestConnectFlowHeadless(t *testing.T) {
	fx := remotetest.StartSSHD(t)
	host := remote.Host{
		Alias:         "fixture",
		Hostname:      fx.Hostname,
		User:          fx.User,
		Port:          fx.Port,
		IdentityFiles: []string{fx.IdentityFile},
	}

	resetApp()
	appData.screen = ScreenServers
	appData.hosts = []remote.Host{host}
	appData.knownHostsPath = fx.KnownHosts
	appData.left = newPane(demoPaneFS(), true)

	shirei.WithFrameLock(func() { startConnect(host, "") })

	waitFor(t, 5*time.Second, func() bool {
		var req *HostKeyRequest
		shirei.WithFrameLock(func() { req = appData.hostKeyReq })
		if req != nil {
			req.Answer <- true
			return true
		}
		return false
	}, "first-contact prompt never appeared")

	waitFor(t, 5*time.Second, func() bool {
		var done bool
		shirei.WithFrameLock(func() {
			if err := appData.connectErrs[host.Alias]; err != nil {
				t.Errorf("connect failed: %v", err)
				done = true
				return
			}
			done = appData.screen == ScreenMain && appData.active != nil
		})
		return done
	}, "connect never completed")
	if t.Failed() {
		t.FailNow()
	}
	defer closeAllSessions()

	// browse a prepared directory over sftp and preview a file
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("over sftp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var pane *Pane
	shirei.WithFrameLock(func() {
		pane = appData.active.Pane
		pane.syncLoad = true
		pane.CWD = dir
		pane.reload("")
	})
	hello := rowNamed(t, pane, "hello.txt")
	pane.selectRow(hello)
	if pane.Preview.Err != nil || pane.Preview.Binary || pane.Preview.Text != "over sftp\n" {
		t.Fatalf("remote preview wrong: %+v", pane.Preview)
	}

	// kill the server: the watcher must raise the disconnect state
	fx.Stop()
	waitFor(t, 5*time.Second, func() bool {
		var down bool
		shirei.WithFrameLock(func() { down = appData.active != nil && appData.active.Disconnected })
		return down
	}, "disconnect watcher never fired")
}

// TestLiveConnectFerryPW dials ferry-pw — ferry-c's password-only user —
// through the real GUI state machine: publickey fails (no authorized
// keys), real sshd rejects a wrong password, the retry prompt comes back,
// and the committed dev password lands on the main screen. Guarded:
// FERRY_LIVE=1 go test -run TestLiveConnectFerryPW
func TestLiveConnectFerryPW(t *testing.T) {
	if os.Getenv("FERRY_LIVE") == "" {
		t.Skip("set FERRY_LIVE=1 to run against the ferry-c lima box")
	}
	host, err := remote.ResolveHost("dev_ssh_config", "ferry-pw")
	if err != nil {
		t.Fatal(err)
	}

	resetApp()
	appData.screen = ScreenServers
	appData.hosts = []remote.Host{host}
	appData.knownHostsPath = filepath.Join(t.TempDir(), "known_hosts")
	appData.left = newPane(demoPaneFS(), true)

	shirei.WithFrameLock(func() { startConnect(host, "") })

	waitFor(t, 15*time.Second, func() bool {
		var req *HostKeyRequest
		shirei.WithFrameLock(func() { req = appData.hostKeyReq })
		if req != nil {
			req.Answer <- true
			return true
		}
		return false
	}, "first-contact prompt never appeared")

	var firstReq *PasswordRequest
	waitFor(t, 15*time.Second, func() bool {
		var req *PasswordRequest
		shirei.WithFrameLock(func() { req = appData.passwordReq })
		if req != nil {
			t.Logf("password prompt for %s@%s (attempt %d)", req.User, req.Addr, req.Attempt)
			firstReq = req
			req.Answer <- passwordAnswer{password: "not the password", ok: true}
			return true
		}
		return false
	}, "password prompt never appeared")

	waitFor(t, 15*time.Second, func() bool {
		var req *PasswordRequest
		shirei.WithFrameLock(func() { req = appData.passwordReq })
		if req == nil || req == firstReq {
			return false
		}
		if req.Attempt != 2 {
			t.Fatalf("retry prompt should be attempt 2, got %d", req.Attempt)
		}
		req.Answer <- passwordAnswer{password: "ferry123", ok: true}
		return true
	}, "retry prompt never appeared")

	waitFor(t, 15*time.Second, func() bool {
		var done bool
		shirei.WithFrameLock(func() {
			if err := appData.connectErrs[host.Alias]; err != nil {
				t.Errorf("connect failed: %v", err)
				done = true
				return
			}
			done = appData.screen == ScreenMain && appData.active != nil
		})
		return done
	}, "connect never completed")
	if t.Failed() {
		t.FailNow()
	}
	defer closeAllSessions()

	var home string
	shirei.WithFrameLock(func() { home = appData.active.Pane.CWD })
	if home != "/home/pwuser" {
		t.Errorf("remote pane should start at pwuser's home, got %q", home)
	}
}

// TestLiveConnectFerryB runs the same flow against the ferry-b lima box
// with a fresh known_hosts — genuine first contact over a real network
// connection. Guarded: FERRY_LIVE=1 go test -run TestLiveConnectFerryB
func TestLiveConnectFerryB(t *testing.T) {
	if os.Getenv("FERRY_LIVE") == "" {
		t.Skip("set FERRY_LIVE=1 to run against the ferry-b lima box")
	}
	host, err := remote.ResolveHost("dev_ssh_config", "ferry-b")
	if err != nil {
		t.Fatal(err)
	}

	resetApp()
	appData.screen = ScreenServers
	appData.hosts = []remote.Host{host}
	appData.knownHostsPath = filepath.Join(t.TempDir(), "known_hosts")
	appData.left = newPane(demoPaneFS(), true)

	shirei.WithFrameLock(func() { startConnect(host, "") })

	waitFor(t, 15*time.Second, func() bool {
		var req *HostKeyRequest
		shirei.WithFrameLock(func() { req = appData.hostKeyReq })
		if req != nil {
			t.Logf("first contact with %s, key %s", req.Addr, req.Fingerprint)
			req.Answer <- true
			return true
		}
		return false
	}, "first-contact prompt never appeared")

	waitFor(t, 15*time.Second, func() bool {
		var done bool
		shirei.WithFrameLock(func() {
			if err := appData.connectErrs[host.Alias]; err != nil {
				t.Errorf("connect failed: %v", err)
				done = true
				return
			}
			done = appData.screen == ScreenMain && appData.active != nil
		})
		return done
	}, "connect never completed")
	if t.Failed() {
		t.FailNow()
	}
	defer closeAllSessions()

	pane := appData.active.Pane
	waitFor(t, 10*time.Second, func() bool {
		var loaded bool
		shirei.WithFrameLock(func() { loaded = !pane.Loading && len(pane.Rows) > 0 })
		return loaded
	}, "remote home never listed")
	t.Logf("connected, home %s with %d entries", pane.CWD, len(pane.Rows))

	shirei.WithFrameLock(func() { pane.syncLoad = true })
	pane.selectRow(rowNamed(t, pane, ".bashrc"))
	if pane.Preview.Err != nil || pane.Preview.Binary || len(pane.Preview.Text) == 0 {
		t.Fatalf("remote preview wrong: %+v", pane.Preview)
	}
	t.Logf("previewed .bashrc: %d bytes", len(pane.Preview.Text))
}

// TestSnapshotTransfers pins the main screen with copy buttons visible
// and the queue strip showing every status flavor.
func TestSnapshotTransfers(t *testing.T) {
	resetApp()
	appData.left = newPane(demoPaneFS(), true)
	// non-nil conn unlocks the copy buttons; never dialed
	setActiveSession(&remote.Conn{}, "ferry-a", newPane(demoPaneFS(), true))
	// left: multi-selection → count on the button + summary panel
	appData.left.clickSelect(rowNamed(t, appData.left, "notes"), 0)
	appData.left.clickSelect(rowNamed(t, appData.left, "photo.png"), shirei.ModCmd)
	appData.left.clickSelect(rowNamed(t, appData.left, "readme.txt"), shirei.ModCmd)
	appData.active.Pane.selectRow(rowNamed(t, appData.active.Pane, "photo.png"))

	running := &Transfer{Dir: DirUpload, Label: "site-assets", Server: "ferry-a",
		DstDesc: "ferry-a:/home/demo", Status: TransferRunning}
	running.done.Store(42 << 20)
	running.total.Store(100 << 20)
	appData.transfers = []*Transfer{
		{Dir: DirDownload, Label: "backup.tar.gz", Server: "ferry-a", DstDesc: "Local:/home/demo",
			Status: TransferFailed, Err: errors.New("remote tar: exit status 2")},
		{Dir: DirUpload, Label: "notes", Server: "ferry-a", DstDesc: "ferry-a:/home/demo",
			Status: TransferDone},
		running,
	}
	appData.transfersExpanded = true
	snapshot(t, "transfers_strip", 1200, 800, RootView)

	// collapsed: one summary line, running progress inline
	appData.transfersExpanded = false
	snapshot(t, "transfers_collapsed", 1200, 800, RootView)
}

// TestSnapshotConflictModal pins the already-exists dialog (folder
// flavor: Merge / Replace / Skip).
func TestSnapshotConflictModal(t *testing.T) {
	resetApp()
	appData.left = newPane(demoPaneFS(), true)
	setActiveSession(&remote.Conn{}, "ferry-a", newPane(demoPaneFS(), true))
	appData.conflictReq = &ConflictRequest{
		Transfer: &Transfer{Dir: DirUpload, Label: "data",
			DstDesc: "ferry-a:/home/demo", Status: TransferAwaiting},
		Names:  []string{"data"},
		HasDir: true,
		Answer: make(chan conflictChoice, 1),
	}
	snapshot(t, "conflict_modal", 1200, 800, RootView)
}

// TestSnapshotConflictModalBatch pins the multi-item flavor: name list +
// Skip existing / Replace / Merge.
func TestSnapshotConflictModalBatch(t *testing.T) {
	resetApp()
	appData.left = newPane(demoPaneFS(), true)
	setActiveSession(&remote.Conn{}, "ferry-a", newPane(demoPaneFS(), true))
	appData.conflictReq = &ConflictRequest{
		Transfer: &Transfer{Dir: DirUpload, Label: "5 items",
			DstDesc: "ferry-a:/home/demo", Status: TransferAwaiting},
		Names:  []string{"notes", "photo.png", "readme.txt"},
		HasDir: true,
		Answer: make(chan conflictChoice, 1),
	}
	snapshot(t, "conflict_modal_batch", 1200, 800, RootView)
}

// Selection semantics: plain click, cmd-toggle, shift-range, ".." clears.
// TestHiddenToggleSelection pins "what you copy is what you can see": a
// dotfile selected while hidden files are shown must drop out of
// selection() when the toggle flips off, and come back when it flips on.
func TestHiddenToggleSelection(t *testing.T) {
	resetApp()
	appData.showHidden = true
	defer func() { appData.showHidden = false }() // global; don't leak into other tests
	p := newPane(demoPaneFS(), true)

	dot := rowNamed(t, p, ".profile")
	p.clickSelect(dot, shirei.ModNone)
	p.clickSelect(rowNamed(t, p, "readme.txt"), shirei.ModCmd)
	if len(p.selection()) != 2 {
		t.Fatalf("both rows should be selected: %d", len(p.selection()))
	}

	appData.showHidden = false
	sel := p.selection()
	if len(sel) != 1 || sel[0].Name != "readme.txt" {
		t.Fatalf("hidden dotfile must not ride along: %v", sel)
	}

	appData.showHidden = true
	if len(p.selection()) != 2 {
		t.Fatalf("re-showing should restore the dotfile's selection")
	}
}

func TestSelectionSemantics(t *testing.T) {
	p := newPane(demoPaneFS(), true) // rows: .., notes/, projects/, photo.png, readme.txt
	names := func() (out []string) {
		for _, r := range p.selection() {
			out = append(out, r.Name)
		}
		return
	}

	p.clickSelect(rowNamed(t, p, "notes"), 0)
	if len(names()) != 1 || names()[0] != "notes" {
		t.Fatalf("plain click: %v", names())
	}
	p.clickSelect(rowNamed(t, p, "photo.png"), shirei.ModCmd)
	if got := names(); len(got) != 2 {
		t.Fatalf("cmd-click toggle on: %v", got)
	}
	p.clickSelect(rowNamed(t, p, "photo.png"), shirei.ModCmd)
	if got := names(); len(got) != 1 || got[0] != "notes" {
		t.Fatalf("cmd-click toggle off: %v", got)
	}
	// shift from anchor photo.png (set by the last cmd-click) — reset anchor first
	p.clickSelect(rowNamed(t, p, "notes"), 0)
	p.clickSelect(rowNamed(t, p, "photo.png"), shirei.ModShift)
	if got := names(); len(got) != 4 { // notes, projects, data.bin, photo.png
		t.Fatalf("shift range: %v", got)
	}
	// multi-selection keeps a preview: the carousel starts at the lead
	// (the row the user just acted on)
	if p.previewRow == nil || p.previewRow.Name != "photo.png" {
		t.Fatalf("carousel should start at the lead, got %v", p.previewRow)
	}
}

// TestPreviewCarousel pins the multi-select preview: it follows the
// lead, cycles both directions with wrap-around, survives a selection
// change that keeps the current row, and clears with the selection.
func TestPreviewCarousel(t *testing.T) {
	resetApp()
	p := newPane(demoPaneFS(), true)
	name := func() string {
		if p.previewRow == nil {
			return "<nil>"
		}
		return p.previewRow.Name
	}

	// notes, projects, data.bin, photo.png selected (shift range)
	p.clickSelect(rowNamed(t, p, "notes"), 0)
	p.clickSelect(rowNamed(t, p, "photo.png"), shirei.ModShift)
	if name() != "photo.png" {
		t.Fatalf("carousel starts at the lead: %s", name())
	}
	if p.Preview.Path == "" || p.Preview.Img == nil {
		t.Fatal("the lead's preview should have loaded (photo.png is an image)")
	}

	p.cyclePreview(1) // wraps to the first selected row
	if name() != "notes" {
		t.Fatalf("cycle forward should wrap to the first: %s", name())
	}
	if p.Preview.Path != "/home/demo/notes" || p.Preview.Text != "" {
		t.Fatalf("folders get a header but no body: %+v", p.Preview)
	}

	p.cyclePreview(-1) // and back
	if name() != "photo.png" {
		t.Fatalf("cycle back should wrap to the last: %s", name())
	}

	p.cyclePreview(1)
	p.cyclePreview(1) // notes → projects
	if name() != "projects" {
		t.Fatalf("cycle forward twice: %s", name())
	}

	// cmd-clicking another row moves the preview there — it follows the
	// user's latest action
	p.clickSelect(rowNamed(t, p, "readme.txt"), shirei.ModCmd)
	if name() != "readme.txt" {
		t.Fatalf("preview should follow the cmd-clicked row: %s", name())
	}

	// cmd-toggling the current row OFF holds the carousel on a row that
	// is still selected (the lead itself just got deselected)
	p.clickSelect(rowNamed(t, p, "readme.txt"), shirei.ModCmd)
	if name() == "readme.txt" || p.previewRow == nil || !rowNamed(t, p, name()).Selected {
		t.Fatalf("carousel must land on a still-selected row: %s", name())
	}

	// single selection: plain preview
	p.clickSelect(rowNamed(t, p, "readme.txt"), 0)
	if name() != "readme.txt" || p.Preview.Text == "" {
		t.Fatalf("single selection previews directly: %s", name())
	}

	// none: blank
	p.clearSelection()
	p.refreshPreview()
	if p.previewRow != nil || p.Preview.Path != "" {
		t.Fatal("empty selection should blank the preview")
	}
}

// Drag-select: sweep from a row extends the range to whatever the press
// is over; shrinking works; cmd-sweeps add to the existing selection.
func TestDragSelect(t *testing.T) {
	p := newPane(demoPaneFS(), true) // notes/, projects/, photo.png, readme.txt
	count := func() int { return len(p.selection()) }

	start := rowNamed(t, p, "notes")
	p.clickSelect(start, 0)
	p.beginDragSelect(start, false)

	p.dragSelectTo(rowNamed(t, p, "readme.txt"))
	if count() != 5 {
		t.Fatalf("sweep to the end: want 5, got %d", count())
	}
	p.dragSelectTo(rowNamed(t, p, "projects"))
	if count() != 2 {
		t.Fatalf("sweep back shrinks: want 2, got %d", count())
	}
	// no-op when the target hasn't changed (preview must not re-trigger)
	before := p.Preview
	p.dragSelectTo(rowNamed(t, p, "projects"))
	if p.Preview != before {
		t.Fatal("same-target sweep should be a no-op")
	}
	p.endDragSelect()
	if p.dragStart != nil || p.dragLast != nil || p.dragBase != nil {
		t.Fatal("release should clear drag state")
	}

	// cmd-sweep: keep readme.txt, add the notes..projects range
	p.clickSelect(rowNamed(t, p, "readme.txt"), 0)
	notes := rowNamed(t, p, "notes")
	p.clickSelect(notes, shirei.ModCmd)
	p.beginDragSelect(notes, true)
	p.dragSelectTo(rowNamed(t, p, "projects"))
	if got := count(); got != 3 {
		t.Fatalf("additive sweep: want 3 (readme + notes + projects), got %d", got)
	}
	if !rowNamed(t, p, "readme.txt").Selected {
		t.Fatal("additive sweep dropped the prior selection")
	}
	p.endDragSelect()
}

// TestDeleteBinFlow drives the two-phase delete end to end over the
// fixture: stage a file and a directory (bin holds full paths, rows get
// stamped, nothing touches the server), restore one, commit — only then
// does the rm run; the spared and restored files survive.
func TestDeleteBinFlow(t *testing.T) {
	conn := wireFixtureConn(t)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "doomed.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "restored.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "victim-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "victim-dir", "inner.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "spared.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	appData.left = newPane(demoPaneFS(), true)
	setActiveSession(conn, "fixture", newPane(PaneFS{Label: "fixture", Home: dir,
		List: conn.List, ReadHead: conn.ReadHead}, true))
	p := appData.active.Pane

	// stage three; the server must see nothing yet
	p.selectRow(rowNamed(t, p, "doomed.txt"))
	p.clickSelect(rowNamed(t, p, "victim-dir"), shirei.ModCmd)
	p.clickSelect(rowNamed(t, p, "restored.txt"), shirei.ModCmd)
	stageDelete(p)
	if len(appData.active.deleteBin) != 3 {
		t.Fatalf("bin should hold 3, has %d", len(appData.active.deleteBin))
	}
	if !rowBinned(p, rowNamed(t, p, "doomed.txt")) || rowBinned(p, rowNamed(t, p, "spared.txt")) {
		t.Fatal("rowBinned should mark exactly the staged rows")
	}
	if len(p.selection()) != 0 {
		t.Fatal("staging should consume the selection")
	}
	if _, err := os.Stat(filepath.Join(dir, "doomed.txt")); err != nil {
		t.Fatal("staging alone must not delete anything")
	}

	// staging survives navigation: the paths are absolute
	p.enter(rowNamed(t, p, "victim-dir"))
	p.goUp()
	if len(appData.active.deleteBin) != 3 {
		t.Fatal("bin must survive navigation")
	}

	unstageDelete(appData.active, filepath.Join(dir, "restored.txt"))
	if len(appData.active.deleteBin) != 2 || rowBinned(p, rowNamed(t, p, "restored.txt")) {
		t.Fatal("restore should unstage just that path")
	}

	shirei.WithFrameLock(commitDeleteBin)
	waitFor(t, 5*time.Second, func() bool {
		var busy bool
		shirei.WithFrameLock(func() { busy = appData.active.deleteBusy })
		return !busy
	}, "delete never finished")

	shirei.WithFrameLock(func() {
		if appData.active.deleteErr != nil {
			t.Errorf("delete failed: %v", appData.active.deleteErr)
		}
		if len(appData.active.deleteBin) != 0 {
			t.Errorf("bin should clear after a successful delete")
		}
	})
	if t.Failed() {
		t.FailNow()
	}
	for _, gone := range []string{"doomed.txt", "victim-dir"} {
		if _, err := os.Stat(filepath.Join(dir, gone)); err == nil {
			t.Errorf("%s should be deleted", gone)
		}
	}
	for _, kept := range []string{"restored.txt", "spared.txt"} {
		if _, err := os.Stat(filepath.Join(dir, kept)); err != nil {
			t.Errorf("%s should survive: %v", kept, err)
		}
	}
	// the pane refreshed itself after the commit
	for _, r := range p.Rows {
		if r.Name == "doomed.txt" {
			t.Error("listing should have refreshed the deleted row away")
		}
	}
}

// TestCloseTabBinGuard: closing a tab with a non-empty bin must warn (the
// staged deletions never ran); closing anyway forgets the bin and drops
// the session. An empty-bin tab closes with no ceremony.
func TestCloseTabBinGuard(t *testing.T) {
	conn := wireFixtureConn(t)
	appData.left = newPane(demoPaneFS(), true)
	s := setActiveSession(conn, "fixture", newPane(demoPaneFS(), true))
	s.binned = map[string]bool{"/home/demo/readme.txt": true}
	s.deleteBin = []BinItem{{Path: "/home/demo/readme.txt"}}

	requestCloseTab(s)
	if !appData.leaveConfirm || appData.closeTarget != s || len(appData.sessions) != 1 {
		t.Fatal("a non-empty bin must warn instead of closing the tab")
	}

	appData.leaveConfirm = false
	appData.closeTarget = nil
	closeSession(s) // "Close anyway"
	if len(appData.sessions) != 0 || appData.active != nil {
		t.Fatal("closing must drop the session")
	}
	if appData.screen != ScreenServers {
		t.Fatal("closing the last tab lands on the servers screen")
	}

	// empty bin: closes immediately, no warning
	s2 := setActiveSession(conn, "fixture", newPane(demoPaneFS(), true))
	requestCloseTab(s2)
	if appData.leaveConfirm || len(appData.sessions) != 0 {
		t.Fatal("empty bin must close without a warning")
	}
}

// TestNewFolderFlow drives the name-before-create modal state: bad names
// are refused inline, creation lands on the server, the pane reloads
// with the new folder selected, and a server-side failure (existing
// name) surfaces inline with the modal still up.
func TestNewFolderFlow(t *testing.T) {
	conn := wireFixtureConn(t)
	dir := t.TempDir()
	appData.left = newPane(demoPaneFS(), true)
	setActiveSession(conn, "fixture", newPane(PaneFS{Label: "fixture", Home: dir,
		List: conn.List, ReadHead: conn.ReadHead,
		Mkdir: conn.Mkdir}, true))
	p := appData.active.Pane

	req := &NewFolderState{Pane: p, Name: "   "}
	appData.newFolder = req
	createNewFolder(req)
	if req.Err == nil || appData.newFolder != req {
		t.Fatal("blank names must be refused inline")
	}
	req.Name = "a/b"
	createNewFolder(req)
	if req.Err == nil {
		t.Fatal("slashes must be refused")
	}

	req.Name = " fresh dir " // trimmed
	createNewFolder(req)
	if appData.newFolder != nil {
		t.Fatalf("success should dismiss the modal (err: %v)", req.Err)
	}
	fi, err := os.Stat(filepath.Join(dir, "fresh dir"))
	if err != nil || !fi.IsDir() {
		t.Fatalf("folder should exist on the server: %v", err)
	}
	if r := rowNamed(t, p, "fresh dir"); !r.Selected {
		t.Fatal("the new folder should be selected after the reload")
	}

	// server refuses (name exists): error inline, modal stays
	req2 := &NewFolderState{Pane: p, Name: "fresh dir"}
	appData.newFolder = req2
	createNewFolder(req2)
	if req2.Err == nil || appData.newFolder != req2 {
		t.Fatal("a server-side mkdir failure must surface inline")
	}
	appData.newFolder = nil
}

// TestNeutralDeselect drives real clicks: the listing background (below
// the rows) and the header's path stretch both clear the selection.
func TestNeutralDeselect(t *testing.T) {
	shirei.InitFontSubsystem()
	ensureIconFonts()
	ensureDeleteStamp()
	binDemoApp(t)
	clearDeleteBin() // plain panes, no bin
	appData.active.Pane.clearSelection()
	appData.active.Pane.refreshPreview()
	p := appData.left

	runFrame := func(mouse shirei.MouseAction, at shirei.Vec2) {
		shirei.GetHost().WindowSize = shirei.Vec2{1200, 800}
		shirei.GetInputState().MousePoint = at
		shirei.GetFrameInput().Mouse = mouse
		shirei.GetFrameInput().Scroll = shirei.Vec2{}
		shirei.GetFrameInput().Motion = shirei.Vec2{}
		shirei.GetFrameInput().Key = 0
		shirei.GetFrameInput().Text = ""
		shirei.RunFrameFn(RootView)
	}
	away := shirei.Vec2{-100, -100}
	for range 4 {
		runFrame(0, away)
	}

	// left pane, well below the last row
	p.selectRow(rowNamed(t, p, "readme.txt"))
	pt := shirei.Vec2{280, 500}
	runFrame(0, pt)
	runFrame(shirei.MouseClick, pt)
	runFrame(shirei.MouseRelease, pt)
	if len(p.selection()) != 0 {
		t.Fatal("clicking the listing background should deselect")
	}

	// header neutral stretch (between the path and the buttons); y clears
	// the title bar (40) + tab bar (38) to land on the pane header
	p.selectRow(rowNamed(t, p, "readme.txt"))
	pt = shirei.Vec2{300, 94}
	runFrame(0, pt)
	runFrame(shirei.MouseClick, pt)
	runFrame(shirei.MouseRelease, pt)
	if len(p.selection()) != 0 {
		t.Fatal("clicking the header's neutral zone should deselect")
	}
}

// TestSnapshotNewFolderModal pins the name-entry dialog.
func TestSnapshotNewFolderModal(t *testing.T) {
	binDemoApp(t)
	clearDeleteBin()
	appData.active.Pane.clearSelection()
	appData.active.Pane.refreshPreview()
	appData.newFolder = &NewFolderState{Pane: appData.active.Pane, Name: "site-assets"}
	snapshot(t, "new_folder_modal", 1200, 800, RootView)
}

// TestSnapshotPreviewCollapsed pins the collapsed preview: header strip
// only, body height zero.
func TestSnapshotPreviewCollapsed(t *testing.T) {
	binDemoApp(t)
	clearDeleteBin()
	appData.active.Pane.previewOpen = false
	snapshot(t, "preview_collapsed", 1200, 800, RootView)
}

// TestSnapshotAppIcon pins the embedded dock icon (icon.png).
func TestSnapshotAppIcon(t *testing.T) {
	img := appIcon()
	path := filepath.Join("testdata", "snapshots", "app_icon.png")
	r := shirei.CompareImage("app_icon", path, img)
	shirei.ReportSnap(t.Name(), r)
	checkSnap(t, r)
	// Corners outside the squircle should stay transparent.
	if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
		t.Fatal("icon corners should be fully transparent")
	}
}

// wireFixtureConn dials the sshd fixture directly and wires the app as an
// established session — the connect flow itself is covered elsewhere.
func wireFixtureConn(t *testing.T) *remote.Conn {
	fx := remotetest.StartSSHD(t)
	conn, err := remote.Dial(remote.Host{
		Alias:         "fixture",
		Hostname:      fx.Hostname,
		User:          fx.User,
		Port:          fx.Port,
		IdentityFiles: []string{fx.IdentityFile},
	}, remote.DialOptions{
		KnownHostsPath: fx.KnownHosts,
		AcceptHostKey:  func(string, ssh.PublicKey) bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	resetApp()
	return conn
}

func localPaneAt(dir string) PaneFS {
	return PaneFS{Label: "Local", Home: dir, List: localList, ReadHead: localReadHead}
}

func waitForStatus(t *testing.T, tr *Transfer, want TransferStatus) {
	t.Helper()
	waitFor(t, 10*time.Second, func() bool {
		var s TransferStatus
		var e error
		shirei.WithFrameLock(func() { s, e = tr.Status, tr.Err })
		if s == TransferFailed && want != TransferFailed {
			t.Fatalf("transfer failed: %v", e)
		}
		return s == want
	}, "transfer never reached the expected status")
}

// TestGUIUploadWithConflictPrompt drives the queue exactly as the buttons
// and modal do: enqueue → engine reports the conflict → prompt →
// merge → destination merged and the destination pane refreshed.
func TestGUIUploadWithConflictPrompt(t *testing.T) {
	conn := wireFixtureConn(t)
	_ = conn

	srcDir, dstDir := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dstDir, "a.txt"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dstDir, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	appData.left = newPane(localPaneAt(srcDir), true)
	setActiveSession(conn, "fixture", newPane(PaneFS{Label: "fixture", Home: dstDir,
		List: conn.List, ReadHead: conn.ReadHead}, true))
	appData.left.selectRow(rowNamed(t, appData.left, "a.txt"))

	var tr *Transfer
	shirei.WithFrameLock(func() {
		enqueueCopy(appData.left, appData.active.Pane, DirUpload)
		tr = appData.transfers[0]
	})

	waitFor(t, 10*time.Second, func() bool {
		var req *ConflictRequest
		shirei.WithFrameLock(func() { req = appData.conflictReq })
		if req != nil {
			req.Answer <- choiceMerge
			return true
		}
		return false
	}, "conflict prompt never appeared")

	waitForStatus(t, tr, TransferDone)

	got, err := os.ReadFile(filepath.Join(dstDir, "a.txt"))
	if err != nil || string(got) != "v2" {
		t.Fatalf("a.txt = %q, %v; want v2", got, err)
	}
	if keep, err := os.ReadFile(filepath.Join(dstDir, "keep.txt")); err != nil || string(keep) != "keep" {
		t.Fatalf("keep.txt damaged: %q, %v", keep, err)
	}
	// destination pane refreshed: the merged file shows up
	shirei.WithFrameLock(func() { rowNamed(t, appData.active.Pane, "a.txt") })
}

// TestGUICancelLeavesDestUntouched is the phase-3 done-when: cancel a
// running transfer through the same path the Cancel button uses; the
// destination must stay untouched with no stage left behind.
func TestGUICancelLeavesDestUntouched(t *testing.T) {
	conn := wireFixtureConn(t)

	srcDir, dstDir := t.TempDir(), t.TempDir()
	big := filepath.Join(srcDir, "big.dat")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(64 << 20); err != nil {
		t.Fatal(err)
	}
	f.Close()

	appData.left = newPane(localPaneAt(srcDir), true)
	setActiveSession(conn, "fixture", newPane(PaneFS{Label: "fixture", Home: dstDir,
		List: conn.List, ReadHead: conn.ReadHead}, true))
	appData.left.selectRow(rowNamed(t, appData.left, "big.dat"))

	var tr *Transfer
	shirei.WithFrameLock(func() {
		enqueueCopy(appData.left, appData.active.Pane, DirUpload)
		tr = appData.transfers[0]
	})

	waitFor(t, 10*time.Second, func() bool {
		done, _ := tr.Progress()
		var running bool
		shirei.WithFrameLock(func() { running = tr.Status == TransferRunning })
		return running && done > 0
	}, "transfer never started moving bytes")

	shirei.WithFrameLock(func() { cancelTransfer(tr) })
	waitForStatus(t, tr, TransferCancelled)

	entries, err := os.ReadDir(dstDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		t.Errorf("destination should be untouched, found %s", e.Name())
	}
}

// TestGUIBatchSkipExisting: three items, one conflicted — "skip existing"
// copies the fresh two in one batch and leaves the conflicted file alone.
func TestGUIBatchSkipExisting(t *testing.T) {
	conn := wireFixtureConn(t)

	srcDir, dstDir := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("new a"), 0o644)
	os.WriteFile(filepath.Join(srcDir, "b.txt"), []byte("new b"), 0o644)
	os.MkdirAll(filepath.Join(srcDir, "sub"), 0o755)
	os.WriteFile(filepath.Join(srcDir, "sub/c.txt"), []byte("new c"), 0o644)
	os.WriteFile(filepath.Join(dstDir, "a.txt"), []byte("old a"), 0o644)

	appData.left = newPane(localPaneAt(srcDir), true)
	setActiveSession(conn, "fixture", newPane(PaneFS{Label: "fixture", Home: dstDir,
		List: conn.List, ReadHead: conn.ReadHead}, true))

	left := appData.left
	left.clickSelect(rowNamed(t, left, "sub"), 0)
	left.clickSelect(rowNamed(t, left, "a.txt"), shirei.ModCmd)
	left.clickSelect(rowNamed(t, left, "b.txt"), shirei.ModCmd)
	if len(left.selection()) != 3 {
		t.Fatalf("expected 3 selected, got %d", len(left.selection()))
	}

	var tr *Transfer
	shirei.WithFrameLock(func() {
		enqueueCopy(left, appData.active.Pane, DirUpload)
		tr = appData.transfers[len(appData.transfers)-1]
	})
	if tr.Label != "3 items" {
		t.Fatalf("label = %q", tr.Label)
	}

	waitFor(t, 10*time.Second, func() bool {
		var req *ConflictRequest
		shirei.WithFrameLock(func() { req = appData.conflictReq })
		if req != nil {
			if len(req.Names) != 1 || req.Names[0] != "a.txt" || req.HasDir {
				t.Errorf("unexpected conflict report: %+v", req.Names)
			}
			req.Answer <- choiceSkipExisting
			return true
		}
		return false
	}, "batch conflict prompt never appeared")

	waitForStatus(t, tr, TransferDone)

	if got, _ := os.ReadFile(filepath.Join(dstDir, "a.txt")); string(got) != "old a" {
		t.Fatalf("conflicted a.txt should be untouched, got %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dstDir, "b.txt")); string(got) != "new b" {
		t.Fatalf("b.txt = %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dstDir, "sub/c.txt")); string(got) != "new c" {
		t.Fatalf("sub/c.txt = %q", got)
	}
}

// Arrow keys: plain steps move the selection from the lead; shift-steps
// grow and shrink the anchor range; stepping clamps at the edges.
func TestArrowKeySelection(t *testing.T) {
	p := newPane(demoPaneFS(), true) // notes/, projects/, data.bin, photo.png, readme.txt
	names := func() (out []string) {
		for _, r := range p.selection() {
			out = append(out, r.Name)
		}
		return
	}

	p.stepSelection(1, false) // nothing selected: down selects the first row
	if got := names(); len(got) != 1 || got[0] != "notes" {
		t.Fatalf("first down: %v", got)
	}
	p.stepSelection(1, false)
	if got := names(); len(got) != 1 || got[0] != "projects" {
		t.Fatalf("plain down: %v", got)
	}
	p.stepSelection(1, true) // shift+down: projects..data.bin
	p.stepSelection(1, true) // shift+down: projects..photo.png
	if got := names(); len(got) != 3 {
		t.Fatalf("shift extension: %v", got)
	}
	if p.Anchor == nil || p.Anchor.Name != "projects" {
		t.Fatalf("anchor should stay at projects, got %+v", p.Anchor)
	}
	p.stepSelection(-1, true) // shift+up shrinks back to projects..data.bin
	if got := names(); len(got) != 2 {
		t.Fatalf("shift shrink: %v", got)
	}
	p.stepSelection(1, false) // plain down steps from the lead (data.bin)
	if got := names(); len(got) != 1 || got[0] != "photo.png" {
		t.Fatalf("plain step after range: %v", got)
	}
	p.stepSelection(1, false)
	p.stepSelection(1, false) // clamp at the last row
	if got := names(); len(got) != 1 || got[0] != "readme.txt" {
		t.Fatalf("clamp at end: %v", got)
	}
}

// TestLiveTransferFerryA pushes a directory to the ferry-a box through
// the GUI queue and reads it back. FERRY_LIVE=1 to run.
func TestLiveTransferFerryA(t *testing.T) {
	if os.Getenv("FERRY_LIVE") == "" {
		t.Skip("set FERRY_LIVE=1 to run against the ferry-a lima box")
	}
	host, err := remote.ResolveHost("dev_ssh_config", "ferry-a")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := remote.Dial(host, remote.DialOptions{
		KnownHostsPath: defaultKnownHostsPath("dev_ssh_config"),
		AcceptHostKey:  func(string, ssh.PublicKey) bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	resetApp()

	srcDir := t.TempDir()
	os.MkdirAll(filepath.Join(srcDir, "bundle/sub"), 0o755)
	os.WriteFile(filepath.Join(srcDir, "bundle/x.txt"), []byte("live x"), 0o644)
	os.WriteFile(filepath.Join(srcDir, "bundle/sub/y.txt"), []byte("live y"), 0o644)

	dstDir, err := conn.Output("mktemp -d")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Output("rm -rf " + dstDir) })

	appData.left = newPane(localPaneAt(srcDir), true)
	setActiveSession(conn, "ferry-a", newPane(PaneFS{Label: "ferry-a", Home: dstDir,
		List: conn.List, ReadHead: conn.ReadHead}, true))
	appData.left.selectRow(rowNamed(t, appData.left, "bundle"))

	var tr *Transfer
	shirei.WithFrameLock(func() {
		enqueueCopy(appData.left, appData.active.Pane, DirUpload)
		tr = appData.transfers[0]
	})
	waitForStatus(t, tr, TransferDone)

	back, err := conn.ReadHead(dstDir+"/bundle/sub/y.txt", 64)
	if err != nil || string(back) != "live y" {
		t.Fatalf("read back: %q, %v", back, err)
	}
	shirei.WithFrameLock(func() { rowNamed(t, appData.active.Pane, "bundle") })
	t.Logf("uploaded bundle to ferry-a:%s and read it back", dstDir)
}

// Going up via ".." must reselect the directory you came from.
func TestDiveAndUp(t *testing.T) {
	p := newPane(demoPaneFS(), true)
	p.enter(rowNamed(t, p, "projects"))
	p.enter(rowNamed(t, p, "ferry"))
	if p.CWD != "/home/demo/projects/ferry" {
		t.Fatalf("cwd = %s", p.CWD)
	}
	p.goUp()
	if p.CWD != "/home/demo/projects" {
		t.Fatalf("up failed, cwd = %s", p.CWD)
	}
	sel := p.selection()
	if len(sel) != 1 || sel[0].Name != "ferry" {
		t.Fatalf("up should reselect the dir we came from, got %+v", sel)
	}
}
