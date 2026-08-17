package window

import (
	"testing"
	"time"

	"go.hasen.dev/shirei"
)

type dummyContext struct {
	platform string
}

func (d dummyContext) Platform() string { return d.platform }

func TestSetMinSizeDeferredWhenWindowNotReady(t *testing.T) {
	orig := shirei.GetHost().EscapeHatchBackendContext
	origMinSize := setPlatformMinSize
	defer func() {
		shirei.GetHost().EscapeHatchBackendContext = orig
		setPlatformMinSize = origMinSize
		mu.Lock()
		lastCtx = nil
		lastAppliedW = 0
		lastAppliedH = 0
		hasPendingMinSize = false
		pendingMinW = 0
		pendingMinH = 0
		pendingPlacement = placeNone
		waiterActive = false
		mu.Unlock()
	}()

	var callCount int
	var recordedW, recordedH float32
	setPlatformMinSize = func(ctx shirei.BackendContext, minW, minH float32) {
		callCount++
		recordedW = minW
		recordedH = minH
	}

	shirei.GetHost().EscapeHatchBackendContext = nil

	// Call SetMinSize before window is ready (like right after SetupWindow)
	SetMinSize(450, 350)
	if callCount != 0 {
		t.Fatalf("expected 0 immediate calls before window is ready, got %d", callCount)
	}

	// Simulate window becoming ready (app.Run starts)
	dummy := dummyContext{platform: "test"}
	shirei.GetHost().EscapeHatchBackendContext = dummy

	// Wait for the background waiter to apply the deferred min size
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	calls := callCount
	w, h := recordedW, recordedH
	mu.Unlock()

	if calls != 1 {
		t.Fatalf("expected 1 deferred call after window ready, got %d", calls)
	}
	if w != 450 || h != 350 {
		t.Fatalf("expected 450, 350, got %v, %v", w, h)
	}
}

func TestPlacementDeferredWhenWindowNotReady(t *testing.T) {
	orig := shirei.GetHost().EscapeHatchBackendContext
	origCenter := setPlatformCenter
	origPosition := setPlatformPosition
	defer func() {
		shirei.GetHost().EscapeHatchBackendContext = orig
		setPlatformCenter = origCenter
		setPlatformPosition = origPosition
		mu.Lock()
		lastCtx = nil
		lastAppliedW = 0
		lastAppliedH = 0
		hasPendingMinSize = false
		pendingPlacement = placeNone
		waiterActive = false
		mu.Unlock()
	}()

	var centerCalled int
	var posCalled int
	var recordedX, recordedY int

	setPlatformCenter = func(ctx shirei.BackendContext) {
		centerCalled++
	}
	setPlatformPosition = func(ctx shirei.BackendContext, x, y int) {
		posCalled++
		recordedX = x
		recordedY = y
	}

	shirei.GetHost().EscapeHatchBackendContext = nil

	// Call Position before window is ready
	Position(120, 80)
	if posCalled != 0 {
		t.Fatalf("expected 0 immediate calls before window is ready, got %d", posCalled)
	}

	// Window becomes ready
	dummy := dummyContext{platform: "test"}
	shirei.GetHost().EscapeHatchBackendContext = dummy

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	calls := posCalled
	x, y := recordedX, recordedY
	mu.Unlock()

	if calls != 1 {
		t.Fatalf("expected 1 deferred Position call, got %d", calls)
	}
	if x != 120 || y != 80 {
		t.Fatalf("expected 120, 80, got %d, %d", x, y)
	}
}

func TestSetMinSizeNegativeAndCaching(t *testing.T) {
	orig := shirei.GetHost().EscapeHatchBackendContext
	origMinSize := setPlatformMinSize
	defer func() {
		shirei.GetHost().EscapeHatchBackendContext = orig
		setPlatformMinSize = origMinSize
		mu.Lock()
		lastCtx = nil
		lastAppliedW = 0
		lastAppliedH = 0
		hasPendingMinSize = false
		pendingPlacement = placeNone
		waiterActive = false
		mu.Unlock()
	}()

	var callCount int
	var recordedW, recordedH float32
	setPlatformMinSize = func(ctx shirei.BackendContext, minW, minH float32) {
		callCount++
		recordedW = minW
		recordedH = minH
	}

	dummy := dummyContext{platform: "test"}
	shirei.GetHost().EscapeHatchBackendContext = dummy

	// First call with negative coordinates should clamp to 0.
	SetMinSize(-50, -20)
	if callCount != 1 {
		t.Fatalf("expected 1 call, got %d", callCount)
	}
	if recordedW != 0 || recordedH != 0 {
		t.Fatalf("expected clamped 0,0, got %v,%v", recordedW, recordedH)
	}

	// Repeated call with same clamped values should hit the cache.
	SetMinSize(-50, -20)
	if callCount != 1 {
		t.Fatalf("expected cache hit (1 call), got %d calls", callCount)
	}

	// Call with new dimensions.
	SetMinSize(500, 400)
	if callCount != 2 {
		t.Fatalf("expected 2 calls, got %d", callCount)
	}
	if recordedW != 500 || recordedH != 400 {
		t.Fatalf("expected 500,400, got %v,%v", recordedW, recordedH)
	}
}
