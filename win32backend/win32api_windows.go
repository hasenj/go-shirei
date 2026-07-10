// Win32 API surface used by the backend: lazily-bound user32/gdi32/kernel32
// procedures plus the constants and structs they need. Pure Go via the standard
// syscall package (no cgo), so the backend cross-compiles from any OS with
// GOOS=windows. Only what win32backend actually calls is declared here.
package win32backend

import "syscall"

var (
	// kernel32/user32/gdi32/imm32 are KnownDLLs (always mapped from System32),
	// so NewLazyDLL's default search is not a planting risk for these DLLs.
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	imm32    = syscall.NewLazyDLL("imm32.dll")

	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	procGlobalAlloc      = kernel32.NewProc("GlobalAlloc")
	procGlobalLock       = kernel32.NewProc("GlobalLock")
	procGlobalUnlock     = kernel32.NewProc("GlobalUnlock")

	procRegisterClassExW         = user32.NewProc("RegisterClassExW")
	procCreateWindowExW          = user32.NewProc("CreateWindowExW")
	procDefWindowProcW           = user32.NewProc("DefWindowProcW")
	procDestroyWindow            = user32.NewProc("DestroyWindow")
	procPostQuitMessage          = user32.NewProc("PostQuitMessage")
	procGetMessageW              = user32.NewProc("GetMessageW")
	procTranslateMessage         = user32.NewProc("TranslateMessage")
	procDispatchMessageW         = user32.NewProc("DispatchMessageW")
	procLoadCursorW              = user32.NewProc("LoadCursorW")
	procGetClientRect            = user32.NewProc("GetClientRect")
	procInvalidateRect           = user32.NewProc("InvalidateRect")
	procBeginPaint               = user32.NewProc("BeginPaint")
	procEndPaint                 = user32.NewProc("EndPaint")
	procShowWindow               = user32.NewProc("ShowWindow")
	procUpdateWindow             = user32.NewProc("UpdateWindow")
	procSetWindowPos             = user32.NewProc("SetWindowPos")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procBringWindowToTop         = user32.NewProc("BringWindowToTop")
	procSetFocus                 = user32.NewProc("SetFocus")
	procSetTimer                 = user32.NewProc("SetTimer")
	procKillTimer                = user32.NewProc("KillTimer")
	procGetKeyState              = user32.NewProc("GetKeyState")
	procSetCapture               = user32.NewProc("SetCapture")
	procReleaseCapture           = user32.NewProc("ReleaseCapture")
	procAdjustWindowRect         = user32.NewProc("AdjustWindowRect")
	procAdjustWindowRectExForDpi = user32.NewProc("AdjustWindowRectExForDpi")
	procGetDpiForWindow          = user32.NewProc("GetDpiForWindow")
	procGetDpiForSystem          = user32.NewProc("GetDpiForSystem")
	procGetDC                    = user32.NewProc("GetDC")
	procReleaseDC                = user32.NewProc("ReleaseDC")
	procSetProcDpiCtx            = user32.NewProc("SetProcessDpiAwarenessContext")
	procSetProcDPIAware          = user32.NewProc("SetProcessDPIAware")
	procSendMessageW             = user32.NewProc("SendMessageW")
	procCreateIconIndirect       = user32.NewProc("CreateIconIndirect")
	procOpenClipboard            = user32.NewProc("OpenClipboard")
	procCloseClipboard           = user32.NewProc("CloseClipboard")
	procEmptyClipboard           = user32.NewProc("EmptyClipboard")
	procGetClipboardData         = user32.NewProc("GetClipboardData")
	procSetClipboardData         = user32.NewProc("SetClipboardData")

	procGetDeviceCaps      = gdi32.NewProc("GetDeviceCaps")
	procCreateBitmap       = gdi32.NewProc("CreateBitmap")
	procCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procSelectObject       = gdi32.NewProc("SelectObject")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procDeleteDC           = gdi32.NewProc("DeleteDC")
	procBitBlt             = gdi32.NewProc("BitBlt")

	procImmGetContext           = imm32.NewProc("ImmGetContext")
	procImmReleaseContext       = imm32.NewProc("ImmReleaseContext")
	procImmGetCompositionString = imm32.NewProc("ImmGetCompositionStringW")
	procImmSetCandidateWindow   = imm32.NewProc("ImmSetCandidateWindow")
	procImmNotifyIME            = imm32.NewProc("ImmNotifyIME")
)

// procAvailable reports whether a lazily-bound proc exists in its DLL, so the
// backend can degrade gracefully when an OS (or Wine) lacks a newer entry point
// such as SetProcessDpiAwarenessContext / GetDpiForWindow.
func procAvailable(p *syscall.LazyProc) bool { return p.Find() == nil }

// Window messages.
const (
	wmDestroy             = 0x0002
	wmSize                = 0x0005
	wmKillfocus           = 0x0008
	wmPaint               = 0x000F
	wmClose               = 0x0010
	wmQuit                = 0x0012
	wmErasebkgnd          = 0x0014
	wmInputlangchange     = 0x0051
	wmKeydown             = 0x0100
	wmKeyup               = 0x0101
	wmChar                = 0x0102
	wmSyskeydown          = 0x0104
	wmSyskeyup            = 0x0105
	wmImeStartcomposition = 0x010D
	wmImeEndcomposition   = 0x010E
	wmImeComposition      = 0x010F
	wmTimer               = 0x0113
	wmMousemove           = 0x0200
	wmLbuttondown         = 0x0201
	wmLbuttonup           = 0x0202
	wmRbuttondown         = 0x0204
	wmRbuttonup           = 0x0205
	wmMbuttondown         = 0x0207
	wmMbuttonup           = 0x0208
	wmMousewheel          = 0x020A
	wmMousehwheel         = 0x020E
	wmImeSetcontext       = 0x0281
	wmImeNotify           = 0x0282
	wmImeChar             = 0x0286
	wmDpichanged          = 0x02E0
	wmSeticon             = 0x0080
)

// WM_SETICON wparam values.
const (
	iconSmall = 0
	iconBig   = 1
)

// Window styles / show / SetWindowPos / clipboard / GDI / DIB constants.
const (
	wsOverlappedWindow = 0x00CF0000
	wsVisible          = 0x10000000
	cwUseDefault       = 0x80000000

	swShow = 5

	swpNozorder   = 0x0004
	swpNoactivate = 0x0010

	idcArrow = 32512

	cfUnicodeText = 13
	gmemMoveable  = 0x0002

	dibRGBColors = 0
	biRGB        = 0
	srccopy      = 0x00CC0020

	// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 == (HANDLE)-4
	dpiPerMonitorAwareV2 = ^uintptr(3)

	logPixelsX = 88 // GetDeviceCaps index: horizontal DPI
)

// IMM32 composition flags and command constants.
const (
	gcsCompstr    = 0x0008
	gcsCompattr   = 0x0010
	gcsCompclause = 0x0020
	gcsCursorpos  = 0x0080
	gcsResultstr  = 0x0800

	iscShowUICompositionWindow = 0x80000000

	imnOpencandidate = 0x0005

	cfsCandidatepos = 0x0040

	niCompositionstr = 0x0015
	cpsComplete      = 0x0001
	cpsCancel        = 0x0004
)

// Virtual-key codes (only the ones the backend maps).
const (
	vkBack       = 0x08
	vkTab        = 0x09
	vkReturn     = 0x0D
	vkShift      = 0x10
	vkControl    = 0x11
	vkMenu       = 0x12 // Alt
	vkEscape     = 0x1B
	vkSpace      = 0x20
	vkPrior      = 0x21 // Page Up
	vkNext       = 0x22 // Page Down
	vkEnd        = 0x23
	vkHome       = 0x24
	vkLeft       = 0x25
	vkUp         = 0x26
	vkRight      = 0x27
	vkDown       = 0x28
	vkDelete     = 0x2E
	vkLwin       = 0x5B
	vkRwin       = 0x5C
	vkF1         = 0x70
	vkF12        = 0x7B
	vkProcesskey = 0xE5
)

type win32Point struct{ X, Y int32 }
type win32Rect struct{ Left, Top, Right, Bottom int32 }

type candidateForm struct {
	Index      uint32
	Style      uint32
	CurrentPos win32Point
	Area       win32Rect
}

type wndClassExW struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   syscall.Handle
	Icon       syscall.Handle
	Cursor     syscall.Handle
	Background syscall.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     syscall.Handle
}

type win32Msg struct {
	Hwnd    syscall.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      win32Point
}

type paintStruct struct {
	Hdc         syscall.Handle
	Erase       int32
	RcPaint     win32Rect
	Restore     int32
	IncUpdate   int32
	RgbReserved [32]byte
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

// bitmapInfo is a BITMAPINFOHEADER plus a one-entry color table. For a 32bpp
// BI_RGB DIB the color table is unused, but keeping a slot makes the layout a
// valid BITMAPINFO.
type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]uint32
}

// iconInfo mirrors ICONINFO (Go inserts the same pre-handle padding the C
// layout has on 64-bit).
type iconInfo struct {
	FIcon    int32
	XHotspot uint32
	YHotspot uint32
	HbmMask  syscall.Handle
	HbmColor syscall.Handle
}
