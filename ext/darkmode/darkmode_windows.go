//go:build windows

package darkmode

import (
	"syscall"
	"unsafe"
)

const (
	hkeyCurrentUser        = 0x80000001
	keyQueryValue          = 0x0001
	keyNotify              = 0x0010
	keyRead                = 0x20019
	regNotifyChangeLastSet = 0x00000004
)

var (
	advapi32                    = syscall.NewLazyDLL("advapi32.dll")
	procRegOpenKeyExW           = advapi32.NewProc("RegOpenKeyExW")
	procRegQueryValueExW        = advapi32.NewProc("RegQueryValueExW")
	procRegCloseKey             = advapi32.NewProc("RegCloseKey")
	procRegNotifyChangeKeyValue = advapi32.NewProc("RegNotifyChangeKeyValue")
)

func initPlatform() {
	readInitialAndStartWatcher()
}

func queryAppsUseLightTheme(hKey syscall.Handle) bool {
	var valType uint32
	var data uint32
	dataSize := uint32(unsafe.Sizeof(data))
	valName, err := syscall.UTF16PtrFromString("AppsUseLightTheme")
	if err != nil {
		return false
	}
	ret, _, _ := procRegQueryValueExW.Call(
		uintptr(hKey),
		uintptr(unsafe.Pointer(valName)),
		0,
		uintptr(unsafe.Pointer(&valType)),
		uintptr(unsafe.Pointer(&data)),
		uintptr(unsafe.Pointer(&dataSize)),
	)
	if ret == 0 {
		// AppsUseLightTheme: 0 = Dark, 1 = Light
		return data == 0
	}
	return false
}

func readInitialAndStartWatcher() {
	subKey, err := syscall.UTF16PtrFromString(`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`)
	if err != nil {
		return
	}

	var hKey syscall.Handle
	ret, _, _ := procRegOpenKeyExW.Call(
		uintptr(hkeyCurrentUser),
		uintptr(unsafe.Pointer(subKey)),
		0,
		uintptr(keyRead),
		uintptr(unsafe.Pointer(&hKey)),
	)
	if ret != 0 {
		return
	}

	// Read initial value synchronously so first OSDarkMode() call is accurate
	setDarkMode(queryAppsUseLightTheme(hKey))

	// Run blocking watcher in a background goroutine
	go func() {
		defer procRegCloseKey.Call(uintptr(hKey))
		for {
			r, _, _ := procRegNotifyChangeKeyValue.Call(
				uintptr(hKey),
				0, // bWatchSubtree = FALSE
				uintptr(regNotifyChangeLastSet),
				0, // hEvent = NULL (blocking)
				0, // fAsynchronous = FALSE
			)
			if r != 0 {
				return
			}
			setDarkMode(queryAppsUseLightTheme(hKey))
		}
	}()
}
