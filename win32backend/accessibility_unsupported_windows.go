//go:build windows && !amd64 && !arm64

package win32backend

import "go.hasen.dev/shirei"

const accessWake = 0x8000 + 73

func initAccess()                                     {}
func closeAccess()                                    {}
func flushAccessAction()                              {}
func updateAccess([]shirei.AccessNode, bool)          {}
func accessGetObject(h, w, l uintptr) (uintptr, bool) { return 0, false }

func refreshAccess() {}

type windowAccess struct{}

func newWindowAccess() *windowAccess { return &windowAccess{} }
