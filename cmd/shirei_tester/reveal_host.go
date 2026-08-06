package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
)

func revealFile(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", "-R", abs).Run()
	case "windows":
		return exec.Command("explorer", "/select,", abs).Start()
	default:
		return openDir(filepath.Dir(abs))
	}
}

func openDir(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", abs).Run()
	case "windows":
		return exec.Command("explorer", abs).Start()
	case "linux":
		return exec.Command("xdg-open", abs).Start()
	default:
		return fmt.Errorf("unsupported OS")
	}
}
