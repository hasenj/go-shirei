package main

import (
	"os/exec"
	"strings"
	"sync"
)

// ADBDevice is one row from `adb devices -l`.
type ADBDevice struct {
	Serial string
	Model  string // model:X from -l output; may be empty
	State  string // "device", "unauthorized", "offline", …
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

var devCache struct {
	mu       sync.Mutex
	list     []ADBDevice
	ready    bool
	scanning bool
	err      error
}

// ensureDevices returns the cached device list, kicking off a scan on first
// call. GUI-safe: never blocks.
func ensureDevices() (list []ADBDevice, ready bool, err error) {
	devCache.mu.Lock()
	defer devCache.mu.Unlock()
	if !devCache.ready && !devCache.scanning {
		devCache.scanning = true
		go scanDevices()
	}
	return append([]ADBDevice(nil), devCache.list...), devCache.ready, devCache.err
}

func rescanDevices() {
	devCache.mu.Lock()
	devCache.ready = false
	devCache.scanning = true
	devCache.mu.Unlock()
	go scanDevices()
}

func scanDevices() {
	list, err := listADBDevices()
	devCache.mu.Lock()
	devCache.list = list
	devCache.err = err
	devCache.ready = true
	devCache.scanning = false
	devCache.mu.Unlock()
	wakeUI()
}

func listADBDevices() ([]ADBDevice, error) {
	sdk, err := findSDK()
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
