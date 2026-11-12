// Package hyprprobe inspects native Hyprland state for windowed behavior checks.
package hyprprobe

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Client struct {
	Address                  string
	Title                    string
	Pid                      int
	Mapped, Hidden, Floating bool
	Size, At                 [2]int
	Fullscreen               int
}

type Layer struct {
	Address         string
	X, Y, W, H, Pid int
	Alpha           float64
	Output          string
}

func Layers() ([]Layer, error) {
	data, err := Run("layers", "-j")
	if err != nil {
		return nil, err
	}
	var monitors map[string]struct{ Levels map[string][]Layer }
	if err := json.Unmarshal(data, &monitors); err != nil {
		return nil, err
	}
	var result []Layer
	for name, m := range monitors {
		for _, layers := range m.Levels {
			for _, l := range layers {
				if l.Pid == os.Getpid() && l.Alpha > 0.99 {
					l.Output = name
					result = append(result, l)
				}
			}
		}
	}
	return result, nil
}

type Monitor struct {
	Name                           string
	X, Y, Width, Height, Transform int
	Scale                          float64
}

func Monitors() ([]Monitor, error) {
	data, err := Run("monitors", "-j")
	if err != nil {
		return nil, err
	}
	var result []Monitor
	err = json.Unmarshal(data, &result)
	for i := range result {
		m := &result[i]
		if m.Transform%2 != 0 {
			m.Width, m.Height = m.Height, m.Width
		}
		m.Width, m.Height = int(float64(m.Width)/m.Scale), int(float64(m.Height)/m.Scale)
	}
	return result, err
}

func Run(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, "hyprctl", args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("hyprctl %v: %w: %s", args, err, data)
	}
	return data, nil
}

func Clients() (map[string]Client, error) {
	data, err := Run("clients", "-j")
	if err != nil {
		return nil, err
	}
	var clients []Client
	if err := json.Unmarshal(data, &clients); err != nil {
		return nil, err
	}
	result := map[string]Client{}
	for _, c := range clients {
		if c.Pid == os.Getpid() {
			result[c.Title] = c
		}
	}
	return result, nil
}

// Dispatch targets a window that belongs to this test process.
func Dispatch(action string, c Client) error {
	if c.Pid != os.Getpid() || c.Address == "" {
		return fmt.Errorf("missing test window")
	}
	float := "on"
	if action == "settiled" {
		float = "off"
	} else if action != "setfloating" {
		return fmt.Errorf("unsupported action %q", action)
	}
	data, err := Run("eval", fmt.Sprintf("hl.dispatch(hl.dsp.window.float({action=%q, window=%q}))", float, "address:"+c.Address))
	if err == nil && strings.TrimSpace(string(data)) != "ok" {
		err = fmt.Errorf("dispatch: %s", data)
	}
	return err
}
