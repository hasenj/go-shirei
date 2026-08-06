package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// ADBDevice is one row from `adb devices -l`.
type ADBDevice struct {
	Serial string
	Model  string
	State  string // device | unauthorized | offline | …
}

func (d ADBDevice) Display() string {
	name := d.Model
	if name == "" {
		name = d.Serial
	} else {
		name = name + " (" + d.Serial + ")"
	}
	if d.State != "device" {
		name += " — " + d.State
	}
	return name
}

var adbDevCache struct {
	mu       sync.Mutex
	list     []ADBDevice
	ready    bool
	scanning bool
	err      error
}

func ensureADBDevices() (list []ADBDevice, ready bool, err error) {
	adbDevCache.mu.Lock()
	defer adbDevCache.mu.Unlock()
	if !adbDevCache.ready && !adbDevCache.scanning {
		adbDevCache.scanning = true
		go scanADBDevices()
	}
	return append([]ADBDevice(nil), adbDevCache.list...), adbDevCache.ready, adbDevCache.err
}

func rescanADBDevices() {
	adbDevCache.mu.Lock()
	adbDevCache.ready = false
	adbDevCache.scanning = true
	adbDevCache.mu.Unlock()
	go scanADBDevices()
}

func scanADBDevices() {
	list, err := listADBDevices()
	adbDevCache.mu.Lock()
	adbDevCache.list = list
	adbDevCache.err = err
	adbDevCache.ready = true
	adbDevCache.scanning = false
	adbDevCache.mu.Unlock()
	wakeUI()
}

func listADBDevices() ([]ADBDevice, error) {
	sdk, err := findAndroidSDK()
	if err != nil {
		return nil, err
	}
	adb, err := findADB(sdk)
	if err != nil {
		return nil, err
	}
	out, err := exec.Command(adb, "devices", "-l").Output()
	if err != nil {
		return nil, err
	}
	var list []ADBDevice
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "List of devices") || strings.HasPrefix(line, "*") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		d := ADBDevice{Serial: fields[0], State: fields[1]}
		for _, f := range fields[2:] {
			if m, ok := strings.CutPrefix(f, "model:"); ok {
				d.Model = strings.ReplaceAll(m, "_", " ")
			}
		}
		list = append(list, d)
	}
	return list, nil
}

func findADB(sdk string) (string, error) {
	if p, err := exec.LookPath("adb"); err == nil {
		return p, nil
	}
	p := filepath.Join(sdk, "platform-tools", "adb")
	if runtime.GOOS == "windows" {
		p += ".exe"
	}
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("adb not found")
}

func adbCmd(adb, serial string, args ...string) *exec.Cmd {
	if serial != "" {
		args = append([]string{"-s", serial}, args...)
	}
	return exec.Command(adb, args...)
}

// installAndroidAPK installs apkPath with adb install -r and optionally launches it.
func installAndroidAPK(apkPath, packageID, serial string, logf func(string, ...any)) error {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if st, err := os.Stat(apkPath); err != nil || st.IsDir() {
		return fmt.Errorf("APK not found: %s", apkPath)
	}
	sdk, err := findAndroidSDK()
	if err != nil {
		return err
	}
	adb, err := findADB(sdk)
	if err != nil {
		return err
	}
	logf("— adb install -r %s", apkPath)
	cmd := adbCmd(adb, serial, "install", "-r", apkPath)
	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			logf("%s", line)
		}
	}
	if err != nil {
		return fmt.Errorf("adb install: %w", err)
	}
	if packageID != "" {
		comp := packageID + "/dev.shirei.host.ShireiActivity"
		logf("— am start %s", comp)
		cmd = adbCmd(adb, serial, "shell", "am", "start", "-W", "-n", comp)
		out, err = cmd.CombinedOutput()
		if len(out) > 0 {
			for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				logf("%s", line)
			}
		}
		if err != nil {
			return fmt.Errorf("am start: %w", err)
		}
	}
	return nil
}
