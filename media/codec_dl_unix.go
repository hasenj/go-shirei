//go:build (darwin && !ios) || (linux && !android)

package media

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ebitengine/purego"
)

func openCodecLib(names []string) (uintptr, error) {
	var dirs []string
	if env := strings.TrimSpace(os.Getenv("SHIREI_CODEC_DIR")); env != "" {
		dirs = append(dirs, env)
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		dirs = append(dirs, dir)
		if runtime.GOOS == "darwin" {
			dirs = append(dirs, filepath.Join(dir, "..", "Frameworks"))
		}
	}
	if runtime.GOOS == "darwin" {
		dirs = append(dirs, "/opt/homebrew/lib", "/usr/local/lib")
	}

	var lastErr error
	for _, dir := range dirs {
		for _, name := range names {
			full := filepath.Join(dir, name)
			if _, err := os.Stat(full); err != nil {
				continue
			}
			h, err := purego.Dlopen(full, purego.RTLD_NOW|purego.RTLD_LOCAL)
			if err == nil && h != 0 {
				return h, nil
			}
			lastErr = err
		}
	}
	for _, name := range names {
		h, err := purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_LOCAL)
		if err == nil && h != 0 {
			return h, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("library not found: %v", names)
	}
	return 0, lastErr
}

func registerCodecFunc(fn any, handle uintptr, sym string) error {
	addr, err := purego.Dlsym(handle, sym)
	if err != nil {
		return err
	}
	purego.RegisterFunc(fn, addr)
	return nil
}
