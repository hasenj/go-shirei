//go:build linux

package waylandbackend

import (
	"encoding/binary"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/internal/wayland/layershell"
	"go.hasen.dev/shirei/internal/wayland/wl"
	zxdg "go.hasen.dev/shirei/internal/wayland/xdg"
)

// Exercise queued window controls through their actual Wayland wire requests.
func controlWire(t *testing.T) (*wl.Context, *net.UnixConn) {
	t.Helper()
	dir, err := os.MkdirTemp("", "shirei-wl-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(dir, "test")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("WAYLAND_DISPLAY", "test")
	display, err := wl.Connect("test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { display.Context().Close() })
	peer, err := listener.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { peer.Close() })
	return display.Context(), peer
}

// A split configure sequence must not apply a client size under the old state
// or acknowledgement. Minimum hints include chrome but content requests do not.
func TestToplevelSizeConfigureTransaction(t *testing.T) {
	ctx, peer := controlWire(t)
	w := newWindowState(shirei.NewUI())
	w.surface, w.xdgSurface, w.xdgToplevel = &wl.Surface{}, &zxdg.Surface{}, &zxdg.Toplevel{}
	ctx.Register(w.surface)
	ctx.Register(w.xdgSurface)
	ctx.Register(w.xdgToplevel)
	w.logicalW, w.logicalH = 400, 334
	w.winW, w.winH = 400, 300
	w.waitConfigure = true
	leave := enterWindow(w)
	defer leave()
	c := Context{window: w}
	c.SetMinSize(300, 200)
	c.SetSize(100, 100)
	applyWindowSizeRequests()
	minimum := readControl(t, peer, w.xdgToplevel, 8)
	if binary.LittleEndian.Uint32(minimum) != 300 || binary.LittleEndian.Uint32(minimum[4:]) != 234 {
		t.Fatal("minimum hint loses content/chrome dimensions")
	}
	applyToplevelSize(w)
	if w.logicalW != 400 {
		t.Fatal("client size applies before initial configure")
	}
	w.handler.HandleToplevelConfigure(zxdg.ToplevelConfigureEvent{Width: 800, Height: 600, States: []int32{zxdg.ToplevelStateMaximized}})
	applyToplevelSize(w)
	if w.logicalW != 400 || w.toplevelStates != 0 {
		t.Fatal("partial configure applies immediately")
	}
	w.handler.HandleSurfaceConfigure(zxdg.SurfaceConfigureEvent{Serial: 10})
	applyToplevelSize(w)
	if w.logicalW != 800 || w.clientSizePending || w.minimumSizePending || !w.hasAck || w.ackSerial != 10 {
		t.Fatal("maximized configure must consume the client size request")
	}
	w.handler.HandleToplevelConfigure(zxdg.ToplevelConfigureEvent{Width: 400, Height: 334})
	applyToplevelSize(w)
	if w.logicalW != 800 {
		t.Fatal("unmaximize applies before surface configure")
	}
	w.handler.HandleSurfaceConfigure(zxdg.SurfaceConfigureEvent{Serial: 11})
	applyToplevelSize(w)
	if w.logicalW != 400 || w.logicalH != 334 || w.clientSizePending || w.ackSerial != 11 {
		t.Fatalf("discarded size reappears after restore: %dx%d", w.logicalW, w.logicalH)
	}
	c.SetSize(100, 100)
	applyWindowSizeRequests()
	applyToplevelSize(w)
	if w.logicalW != 300 || w.logicalH != 234 || w.clientSizePending {
		t.Fatalf("fresh floating request does not honor minimum size: %dx%d", w.logicalW, w.logicalH)
	}
	for _, state := range []int32{zxdg.ToplevelStateFullscreen, zxdg.ToplevelStateResizing, zxdg.ToplevelStateTiledLeft, zxdg.ToplevelStateTiledRight, zxdg.ToplevelStateTiledTop, zxdg.ToplevelStateTiledBottom} {
		w.handler.HandleToplevelConfigure(zxdg.ToplevelConfigureEvent{Width: 900, Height: 700, States: []int32{state}})
		w.handler.HandleSurfaceConfigure(zxdg.SurfaceConfigureEvent{Serial: 12})
		c.SetSize(350, 250)
		applyWindowSizeRequests()
		applyToplevelSize(w)
		if w.logicalW != 900 || w.logicalH != 700 || w.clientSizePending || w.winW != 900 {
			t.Fatalf("client size survives or overrides state %d", state)
		}
		w.handler.HandleToplevelConfigure(zxdg.ToplevelConfigureEvent{Width: 410, Height: 344})
		w.handler.HandleSurfaceConfigure(zxdg.SurfaceConfigureEvent{Serial: 13})
		applyToplevelSize(w)
		if w.logicalW != 410 || w.logicalH != 344 {
			t.Fatalf("discarded request reappears after state %d", state)
		}
	}
	w.handler.HandleToplevelConfigure(zxdg.ToplevelConfigureEvent{Width: 350, Height: 284})
	w.handler.HandleSurfaceConfigure(zxdg.SurfaceConfigureEvent{Serial: 13})
	applyToplevelSize(w)
	w.handler.HandleToplevelConfigure(zxdg.ToplevelConfigureEvent{Width: 900, Height: 700, States: []int32{zxdg.ToplevelStateMaximized}})
	w.handler.HandleSurfaceConfigure(zxdg.SurfaceConfigureEvent{Serial: 14})
	c.SetMinSize(320, 210)
	applyWindowSizeRequests()
	readControl(t, peer, w.xdgToplevel, 8)
	applyToplevelSize(w)
	w.handler.HandleToplevelConfigure(zxdg.ToplevelConfigureEvent{Width: 360, Height: 294})
	w.handler.HandleSurfaceConfigure(zxdg.SurfaceConfigureEvent{Serial: 15})
	applyToplevelSize(w)
	if w.logicalW != 360 || w.logicalH != 294 || w.clientSizePending || w.minimumSizePending || w.minW != 320 || w.minH != 210 {
		t.Fatal("minimum hint must persist without retaining a resize request")
	}
	// Configure transitions can arrive in one dispatch, without a frame between them.
	c.SetSize(500, 400)
	applyWindowSizeRequests()
	w.handler.HandleToplevelConfigure(zxdg.ToplevelConfigureEvent{Width: 900, Height: 700, States: []int32{zxdg.ToplevelStateMaximized}})
	w.handler.HandleSurfaceConfigure(zxdg.SurfaceConfigureEvent{Serial: 16})
	w.handler.HandleToplevelConfigure(zxdg.ToplevelConfigureEvent{Width: 360, Height: 294})
	w.handler.HandleSurfaceConfigure(zxdg.SurfaceConfigureEvent{Serial: 17})
	applyToplevelSize(w)
	if w.logicalW != 360 || w.logicalH != 294 || w.clientSizePending {
		t.Fatal("request survives constrained configure between frames")
	}
}

func readControl(t *testing.T, peer *net.UnixConn, object wl.Proxy, opcode uint32) []byte {
	t.Helper()
	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	header := make([]byte, 8)
	if _, err := io.ReadFull(peer, header); err != nil {
		t.Fatal(err)
	}
	id, code := binary.LittleEndian.Uint32(header), binary.LittleEndian.Uint32(header[4:])
	payload := make([]byte, int(code>>16)-8)
	if _, err := io.ReadFull(peer, payload); err != nil {
		t.Fatal(err)
	}
	if id != uint32(object.Id()) || code&65535 != opcode {
		t.Fatalf("request %d:%d, want %d:%d", id, code&65535, object.Id(), opcode)
	}
	return payload
}

func TestQueuedLayerPlacementAndBackgroundRemap(t *testing.T) {
	ctx, peer := controlWire(t)
	w := newWindowState(shirei.NewUI())
	w.surface, w.layerSurface, w.output = &wl.Surface{}, &layershell.Surface{}, &wl.Output{}
	ctx.Register(w.surface)
	ctx.Register(w.layerSurface)
	ctx.Register(w.output)
	w.csdEnabled = false
	w.winW, w.winH = 160, 100
	w.popupID = 42
	oldMain, oldCurrent, oldOutputs, oldPopups := mainWindow, currentWindow, outputs, popupWindows
	oldThread := uiThreadID.Load()
	oldRequested, oldLaunch := activationRequested.Load(), launchActivationToken
	t.Cleanup(func() {
		mainWindow, currentWindow, outputs, popupWindows = oldMain, oldCurrent, oldOutputs, oldPopups
		uiThreadID.Store(oldThread)
		activationRequested.Store(oldRequested)
		launchActivationToken = oldLaunch
	})
	mainWindow, currentWindow = w, w
	popupWindows = map[shirei.PopupWindow]*windowState{42: w}
	outputs = []*outputState{{proxy: w.output, x: -1280, y: 100, width: 1280, height: 720, scale: 2}}
	uiThreadID.Store(1)
	neighbor := &wl.Output{}
	ctx.Register(neighbor)
	outputs = append(outputs, &outputState{proxy: neighbor, x: 0, y: 100, width: 1920, height: 1080, scale: 1})
	pEdge := shirei.Vec2{0, 120}
	w.position = &pEdge
	chooseLayerOutput(w)
	if w.output != neighbor {
		t.Fatal("output selection assigns a shared edge to the wrong display")
	}
	w.output = outputs[0].proxy
	w.position = nil
	c := Context{window: w}
	p := shirei.Vec2{-50, 700}
	done := make(chan struct{})
	go func() { MovePopupWindow(42, &p); c.SetSize(320, 200); close(done) }()
	<-done
	p = shirei.Vec2{999, 999}
	applyWindowSizeRequests()
	applyPlacementRequests()
	if err := commitWindowSurface(); err != nil {
		t.Fatal(err)
	}
	size := readControl(t, peer, w.layerSurface, 0)
	if binary.LittleEndian.Uint32(size) != 320 || binary.LittleEndian.Uint32(size[4:]) != 200 {
		t.Fatal("queued size does not reach the wire")
	}
	anchor := readControl(t, peer, w.layerSurface, 1)
	if binary.LittleEndian.Uint32(anchor) != 5 {
		t.Fatal("positioned layer is not anchored top-left")
	}
	margin := readControl(t, peer, w.layerSurface, 3)
	if int32(binary.LittleEndian.Uint32(margin)) != 520 || int32(binary.LittleEndian.Uint32(margin[12:])) != 960 {
		t.Fatalf("clamped logical margins %v", margin)
	}
	zone := readControl(t, peer, w.layerSurface, 2)
	if int32(binary.LittleEndian.Uint32(zone)) != -1 {
		t.Fatal("screen placement uses the reserved work area")
	}
	readControl(t, peer, w.surface, 6)
	w.exactPosition = true
	p = shirei.Vec2{-1500, 50}
	MovePopupWindow(42, &p)
	applyPlacementRequests()
	if err := commitWindowSurface(); err != nil {
		t.Fatal(err)
	}
	readControl(t, peer, w.layerSurface, 0)
	readControl(t, peer, w.layerSurface, 1)
	margin = readControl(t, peer, w.layerSurface, 3)
	if int32(binary.LittleEndian.Uint32(margin)) != -50 || int32(binary.LittleEndian.Uint32(margin[12:])) != -220 {
		t.Fatal("exact placement loses negative logical coordinates")
	}
	readControl(t, peer, w.layerSurface, 2)
	readControl(t, peer, w.surface, 6)
	c.Center()
	applyPlacementRequests()
	if err := commitWindowSurface(); err != nil {
		t.Fatal(err)
	}
	readControl(t, peer, w.layerSurface, 0)
	anchor = readControl(t, peer, w.layerSurface, 1)
	if binary.LittleEndian.Uint32(anchor) != 0 {
		t.Fatal("centering retains the position anchor")
	}
	readControl(t, peer, w.layerSurface, 3)
	zone = readControl(t, peer, w.layerSurface, 2)
	if binary.LittleEndian.Uint32(zone) != 0 {
		t.Fatal("centering does not restore the work area")
	}
	readControl(t, peer, w.surface, 6)
	w.ui.Host.Input.Composition = "仮"
	w.gotPreedit, w.haveLastCursor = true, true
	hideMainWindow()
	if w.ui.Host.Input.Composition != "" || w.gotPreedit || w.haveLastCursor {
		t.Fatal("hide retains composition or a stale IME cursor")
	}
	attached := readControl(t, peer, w.surface, 1)
	if binary.LittleEndian.Uint32(attached) != 0 || !w.hidden {
		t.Fatal("background close does not detach its buffer")
	}
	readControl(t, peer, w.surface, 6)
	Activate()
	applyActivationRequests()
	for _, op := range []uint32{0, 1, 3, 2, 4} {
		readControl(t, peer, w.layerSurface, op)
	}
	readControl(t, peer, w.surface, 6)
	if w.hidden || !w.waitConfigure || !w.activateAfterPresent {
		t.Fatal("restore does not wait for configure before activation")
	}
	hideMainWindow()
	readControl(t, peer, w.surface, 1)
	readControl(t, peer, w.surface, 6)
	oldLayer, oldSurface, oldHandler := w.layerSurface, w.surface, w.handler
	// The compositor can close a layer after it is already hidden.
	w.surfaceNeedsCreate = true
	hideMainWindow()
	readControl(t, peer, oldLayer, 7)
	readControl(t, peer, oldSurface, 0)
	if w.surface != nil || w.layerSurface != nil || oldHandler.window != nil {
		t.Fatal("hidden compositor-closed role retains live callbacks")
	}
	*w = windowState{closed: true}
	c.Position(100, 200)
	applyPlacementRequests()
	if w.position != nil || w.surfaceStateDirty {
		t.Fatal("stale queued placement revives a closed window")
	}
}

func TestActivationTokenCompletionAndClosedTarget(t *testing.T) {
	ctx, peer := controlWire(t)
	oldManager, oldSource, oldLaunch := activationManager, activationSource, launchActivationToken
	t.Cleanup(func() { activationManager, activationSource, launchActivationToken = oldManager, oldSource, oldLaunch })
	activationManager = &activationGlobal{}
	ctx.Register(activationManager)
	activationSource = nil
	launchActivationToken = ""
	w := newWindowState(shirei.NewUI())
	w.surface = &wl.Surface{}
	ctx.Register(w.surface)
	for _, closed := range []bool{false, true} {
		w.closed = false
		requestActivation(w)
		get := readControl(t, peer, activationManager, 1)
		token := ctx.LookupProxy(wl.ProxyId(binary.LittleEndian.Uint32(get))).(*activationToken)
		readControl(t, peer, token, 1)
		readControl(t, peer, token, 3)
		w.closed = closed
		// A real done event is dispatched through the socket and object registry.
		data := make([]byte, 20)
		binary.LittleEndian.PutUint32(data, uint32(token.Id()))
		binary.LittleEndian.PutUint32(data[4:], 20<<16)
		binary.LittleEndian.PutUint32(data[8:], 6)
		copy(data[12:], "token\x00")
		if _, err := peer.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := ctx.RunTimeout(time.Second); err != nil {
			t.Fatal(err)
		}
		if !closed {
			activate := readControl(t, peer, activationManager, 2)
			if string(activate[4:9]) != "token" || binary.LittleEndian.Uint32(activate[12:]) != uint32(w.surface.Id()) {
				t.Fatal("token completion activates the wrong surface")
			}
		}
		readControl(t, peer, token, 4)
	}
}

func TestLayerMigrationRetainsUIAndRejectsOldCallbacks(t *testing.T) {
	ctx, peer := controlWire(t)
	oldPopups := popupWindows
	t.Cleanup(func() { popupWindows = oldPopups })
	oldCompositor, oldLayerShell, oldOutputs := compositor, layerShell, outputs
	t.Cleanup(func() { compositor, layerShell, outputs = oldCompositor, oldLayerShell, oldOutputs })
	compositor = &wl.Compositor{}
	ctx.Register(compositor)
	layerShell = &layershell.Manager{}
	ctx.Register(layerShell)
	a, b := &wl.Output{}, &wl.Output{}
	ctx.Register(a)
	ctx.Register(b)
	outputs = []*outputState{{proxy: a, name: 1, width: 1000, height: 800}, {proxy: b, name: 2, x: -1400, width: 1200, height: 900}}
	w := newWindowState(shirei.NewUI())
	ui := w.ui
	w.surface = &wl.Surface{}
	w.layerSurface = &layershell.Surface{}
	ctx.Register(w.surface)
	ctx.Register(w.layerSurface)
	w.output = a
	w.floating = true
	w.csdEnabled = false
	w.winW, w.winH = 300, 200
	w.ready = true
	w.popupID = 7
	w.owner = mainWindow
	w.hasAck = true
	w.ackSerial = 9
	w.havePresented = true
	oldHandler, oldSurface, oldLayer := w.handler, w.surface, w.layerSurface
	marker := &pointerOriginMarker{window: w, surface: oldSurface, origin: shirei.Vec2{999, 999}, known: true}
	popupWindows = map[shirei.PopupWindow]*windowState{7: w}
	w.pointerPrimaryDown = true
	Context{window: w}.Position(-1300, 100)
	applyPlacementRequests()
	if w.surface != oldSurface || w.output != a || !w.deferredLayerMove {
		t.Fatal("cross-output drag replaces the surface before pointer release")
	}
	w.pointerPrimaryDown = false
	applyPlacementRequests()
	if w.deferredLayerMove {
		t.Fatal("pointer release does not complete deferred migration")
	}
	if w.ui != ui || w.popupID != 7 || w.owner != mainWindow || !w.ready || w.closed {
		t.Fatal("migration changes logical window lifetime")
	}
	if w.output != b || w.surface == oldSurface || w.layerSurface == oldLayer || oldHandler.window != nil || !w.waitConfigure || w.hasAck || w.havePresented {
		t.Fatal("migration retains old native state")
	}
	readControl(t, peer, oldLayer, 7)
	readControl(t, peer, oldSurface, 0)
	readControl(t, peer, compositor, 0)
	role := readControl(t, peer, layerShell, 0)
	if binary.LittleEndian.Uint32(role[8:]) != uint32(b.Id()) {
		t.Fatal("replacement layer targets wrong output")
	}
	for _, op := range []uint32{0, 1, 3, 2, 4} {
		readControl(t, peer, w.layerSurface, op)
	}
	if compositorVer >= 3 {
		readControl(t, peer, w.surface, 8)
	}
	readControl(t, peer, w.surface, 6)
	origin := w.pointerOrigin
	oldHandler.HandleLayerConfigure(layershell.ConfigureEvent{Serial: 88, Width: 999, Height: 999})
	oldHandler.HandleLayerClosed()
	oldHandler.HandleCallbackDone(wl.CallbackDoneEvent{})
	marker.HandleCallbackDone(wl.CallbackDoneEvent{})
	if w.hasAck || w.logicalW != 300 || w.pointerOrigin != origin {
		t.Fatal("old events alter replacement surface")
	}
	oldDisplay, oldMain, oldHide := disp, mainWindow, mainHideRequested
	t.Cleanup(func() { disp, mainWindow, mainHideRequested = oldDisplay, oldMain, oldHide })
	disp = ctx.LookupProxy(1).(*wl.Display)
	popupWindows = oldPopups
	mainWindow = w
	closingHandler := w.handler
	closingHandler.HandleLayerClosed()
	request := readControl(t, peer, disp, 0)
	confirmation := ctx.LookupProxy(wl.ProxyId(binary.LittleEndian.Uint32(request))).(*wl.Callback)
	if mainHideRequested != oldHide {
		t.Fatal("layer closes before output teardown can settle")
	}
	w.pointerPrimaryDown = true
	h.HandleRegistryGlobalRemove(wl.RegistryGlobalRemoveEvent{Name: 2})
	applyPlacementRequests()
	confirmation.Dispatch(&wl.Event{Opcode: 0, Data: make([]byte, 4)})
	if w.output != a || closingHandler.window != nil || mainHideRequested != oldHide || w.closed || w.pointerPrimaryDown {
		t.Fatal("output removal closes the migrated window")
	}
	delete(windowsBySurface, w.surface)
}

func TestSurfaceScaleOutputMembershipAndFractionalViewport(t *testing.T) {
	ctx, peer := controlWire(t)
	oldOutputs, oldVersion := outputs, compositorVer
	t.Cleanup(func() { outputs, compositorVer = oldOutputs, oldVersion })
	compositorVer = 6
	a, b := &wl.Output{}, &wl.Output{}
	ctx.Register(a)
	ctx.Register(b)
	outputs = []*outputState{{proxy: a, scale: 2}, {proxy: b, scale: 1}}
	w := newWindowState(shirei.NewUI())
	other := newWindowState(shirei.NewUI())
	w.surface = &wl.Surface{}
	ctx.Register(w.surface)
	w.logicalW, w.logicalH = 513, 299
	w.handler.HandleSurfaceEnter(wl.SurfaceEnterEvent{Output: a})
	other.handler.HandleSurfaceEnter(wl.SurfaceEnterEvent{Output: b})
	if w.windowScale != 2 || other.windowScale != 1 {
		t.Fatal("output scales leak between windows")
	}
	w.handler.HandleSurfaceEnter(wl.SurfaceEnterEvent{Output: b})
	if w.windowScale != 2 {
		t.Fatal("overlapping outputs do not retain highest entered scale")
	}
	w.handler.HandleSurfaceLeave(wl.SurfaceLeaveEvent{Output: a})
	if w.windowScale != 1 || w.logicalW != 513 || w.curW != 513 {
		t.Fatal("leaving output retains stale scale or changes logical size")
	}
	w.handler.HandleSurfacePreferredBufferScale(wl.SurfacePreferredBufferScaleEvent{Factor: 2})
	if w.windowScale != 2 {
		t.Fatal("preferred integer scale ignored")
	}
	w.viewport = &surfaceViewport{}
	ctx.Register(w.viewport)
	w.fractionalScale = &surfaceFractionalScale{handler: w.handler}
	ctx.Register(w.fractionalScale)
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, 150)
	w.fractionalScale.Dispatch(&wl.Event{Opcode: 0, Data: data})
	if w.windowScale != 1.25 || w.curW != 641 || w.curH != 374 || other.windowScale != 1 {
		t.Fatal("fractional scale changes logical size or another window")
	}
	leave := enterWindow(w)
	defer leave()
	if err := commitWindowSurface(); err != nil {
		t.Fatal(err)
	}
	scale := readControl(t, peer, w.surface, 8)
	if binary.LittleEndian.Uint32(scale) != 1 {
		t.Fatal("fractional surface uses integer buffer scale")
	}
	destination := readControl(t, peer, w.viewport, 2)
	if binary.LittleEndian.Uint32(destination) != 513 || binary.LittleEndian.Uint32(destination[4:]) != 299 {
		t.Fatal("viewport destination differs from logical size")
	}
	readControl(t, peer, w.surface, 6)
	w.handler.window = nil
	binary.LittleEndian.PutUint32(data, 240)
	w.fractionalScale.Dispatch(&wl.Event{Opcode: 0, Data: data})
	if w.windowScale != 1.25 {
		t.Fatal("stale fractional listener changes replacement window")
	}
}
