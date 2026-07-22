package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
)

var nonAlnumID = regexp.MustCompile(`[^a-z0-9]+`)

// sanitizeAppIDComponent turns a folder name into a reverse-DNS label segment.
func sanitizeAppIDComponent(s string) string {
	s = nonAlnumID.ReplaceAllString(strings.ToLower(s), "")
	if s == "" || s[0] >= '0' && s[0] <= '9' {
		s = "a" + s
	}
	return s
}

// startIOS runs the embedded ios-run.sh synchronously, streaming logs via logf.
func startIOS(pkgDir string, cfg Config, logf logFn) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("iOS builds require macOS + Xcode (this host is %s)", runtime.GOOS)
	}

	rt, err := ensureRuntime()
	if err != nil {
		return fmt.Errorf("extract ios runtime: %w", err)
	}

	args := []string{rt.Script}
	if cfg.IOSTarget == "device" {
		args = append(args, "--device")
	} else {
		args = append(args, "--sim")
	}
	args = append(args, pkgDir)

	bundleID := strings.TrimSpace(cfg.AppID)
	if bundleID == "" {
		bundleID = cfg.effectiveAppIDPrefix() + "." + sanitizeAppIDComponent(filepath.Base(pkgDir))
	}
	appName := strings.TrimSpace(cfg.AppName)
	if appName == "" {
		appName = filepath.Base(pkgDir)
	}

	caps, err := collectCapabilities(pkgDir, logf)
	if err != nil {
		return fmt.Errorf("capabilities: %w", err)
	}
	usageFile := filepath.Join(rt.BuildDir, "ios-usage.txt")
	if err := os.MkdirAll(rt.BuildDir, 0o755); err != nil {
		return err
	}
	if err := writeIOSUsageFile(usageFile, caps.IOS); err != nil {
		return fmt.Errorf("write ios usage file: %w", err)
	}

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = pkgDir
	env := append(os.Environ(),
		"SHIREI_IOS_HOST_DIR="+rt.HostDir,
		"SHIREI_IOS_BUILD_DIR="+rt.BuildDir,
		"SHIREI_IOS_BUNDLE_ID="+bundleID,
		"SHIREI_IOS_APP_NAME="+appName,
		"SHIREI_IOS_TEAM="+cfg.TeamID,
		"SHIREI_IOS_DEVICE_NAME="+cfg.DeviceName,
		"SHIREI_IOS_RUNTIME="+cfg.Runtime,
		"SHIREI_IOS_USAGE_FILE="+usageFile,
	)
	if caps.Orientation != "" {
		env = append(env, "SHIREI_IOS_ORIENTATION="+caps.Orientation)
		logf("— orientation %s (Info.plist)", caps.Orientation)
	}
	icon := resolveIconPath(pkgDir, cfg.IconPath)
	if icon != "" {
		env = append(env, "SHIREI_IOS_ICON_PATH="+icon)
	}
	if mod := findShireiModule(pkgDir); mod != "" {
		env = append(env, "SHIREI_MODULE="+mod)
	} else if mod := findShireiModule(wd); mod != "" {
		env = append(env, "SHIREI_MODULE="+mod)
	}
	cmd.Env = env

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	logf("— ios %v —", args[1:])
	logf("  host=%s", rt.HostDir)
	logf("  build=%s", rt.BuildDir)
	logf("  team=%s bundle=%s app=%s target=%s",
		cfg.TeamID, bundleID, appName, cfg.IOSTarget)

	var wg sync.WaitGroup
	pump := func(rd io.Reader) {
		defer wg.Done()
		sc := bufio.NewScanner(rd)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			logf("%s", sc.Text())
		}
	}
	wg.Add(2)
	go pump(stdout)
	go pump(stderr)
	wg.Wait()
	return cmd.Wait()
}
