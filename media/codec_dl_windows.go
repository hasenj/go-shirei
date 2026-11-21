//go:build windows

package media

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/windows"
)

func openCodecLib(names []string) (uintptr, error) {
	var dirs []string
	if env := strings.TrimSpace(os.Getenv("SHIREI_CODEC_DIR")); env != "" {
		dirs = append(dirs, env)
	}
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}

	var lastErr error
	for _, dir := range dirs {
		for _, name := range names {
			full := filepath.Join(dir, name)
			if _, err := os.Stat(full); err != nil {
				continue
			}
			h, err := windows.LoadLibrary(full)
			if err == nil && h != 0 {
				return uintptr(h), nil
			}
			lastErr = err
		}
	}
	for _, name := range names {
		h, err := windows.LoadLibrary(name)
		if err == nil && h != 0 {
			return uintptr(h), nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("library not found: %v", names)
	}
	return 0, lastErr
}

func registerCodecFunc(fn any, handle uintptr, sym string) error {
	addr, err := windows.GetProcAddress(windows.Handle(handle), sym)
	if err != nil {
		return err
	}
	purego.RegisterFunc(fn, addr)
	return nil
}
