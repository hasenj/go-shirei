package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

var peerHWND uintptr
var stopOverlayPeer = func() {}

// The helper owns an ordinary window in another process, with no Shirei owner
// relationship that could keep it below the overlay independently of topmost.
func startOverlayPeer() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "--overlay-peer")
	cmd.Stderr = os.Stderr
	input, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		input.Close()
		return err
	}
	if err := cmd.Start(); err != nil {
		input.Close()
		output.Close()
		return err
	}
	var once sync.Once
	stopOverlayPeer = func() { once.Do(func() { input.Close(); cmd.Wait() }) }
	if _, err := fmt.Fscan(output, &peerHWND); err != nil {
		stopOverlayPeer()
		return fmt.Errorf("read helper window: %w", err)
	}
	output.Close()
	return nil
}

func runOverlayPeer() {
	runtime.LockOSThread()
	// Closing the parent's pipe also cleans up the helper after an interrupted run.
	go func() { io.Copy(io.Discard, os.Stdin); os.Exit(0) }()
	class, _ := syscall.UTF16PtrFromString("STATIC")
	title, _ := syscall.UTF16PtrFromString("Overlay check: another application")
	hwnd, _, err := user32.NewProc("CreateWindowExW").Call(0,
		uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)), 0x00cf0000,
		80, 80, 520, 350, 0, 0, 0, 0) // WS_OVERLAPPEDWINDOW, no owner or topmost style
	if hwnd == 0 {
		fmt.Fprintln(os.Stderr, "helper CreateWindowExW:", err)
		os.Exit(1)
	}
	showWindow.Call(hwnd, 5)
	user32.NewProc("SetForegroundWindow").Call(hwnd)
	fmt.Println(hwnd)
	var msg struct {
		Window         uintptr
		Message        uint32
		WParam, LParam uintptr
		Time           uint32
		X, Y           int32
		Private        uint32
	}
	for {
		got, _, _ := user32.NewProc("GetMessageW").Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(got) <= 0 {
			return
		}
		user32.NewProc("TranslateMessage").Call(uintptr(unsafe.Pointer(&msg)))
		user32.NewProc("DispatchMessageW").Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func checkOverlay() error {
	getStyle := user32.NewProc("GetWindowLongPtrW")
	style, _, _ := getStyle.Call(mainHWND, ^uintptr(15))   // GWL_STYLE
	exStyle, _, _ := getStyle.Call(mainHWND, ^uintptr(19)) // GWL_EXSTYLE
	if exStyle&0x8 == 0 {
		return fmt.Errorf("overlay main window is not topmost")
	}
	if exStyle&0x80000 == 0 || style&0x00c00000 != 0 {
		return fmt.Errorf("overlay main window is not transparent and borderless")
	}
	// Walk actual native stacking order, not just the requested window style.
	for hwnd, count := peerHWND, 0; hwnd != 0 && count < 10000; count++ {
		hwnd, _, _ = user32.NewProc("GetWindow").Call(hwnd, 3) // GW_HWNDPREV
		if hwnd == mainHWND {
			return nil
		}
	}
	return fmt.Errorf("overlay is below the other application's window")
}
