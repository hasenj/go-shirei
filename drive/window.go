// Window targeting for the drive protocol. The kernel commands `windows` and
// `window` list the app's native windows and select the one that later
// commands operate on.
package drive

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.hasen.dev/shirei"
)

// Window is one entry from the `windows` listing.
type Window struct {
	// ID is the handle used by SelectWindow.
	ID string
	// Kind is "main" or "popup".
	Kind string
	// Title is the native window title; may be empty.
	Title string
	// Size is the window's content size in logical points.
	Size shirei.Vec2
	// Pos is the window's top-left corner in screen logical points. Valid
	// only when HasPos is true; platforms without a global position (such as
	// ordinary Wayland toplevels) omit it.
	Pos    shirei.Vec2
	HasPos bool
	// Target is true for the listener's currently selected window.
	Target bool
}

// WindowList is the reply to `windows`. Count is every registered window;
// Windows holds the records that fit in one datagram.
type WindowList struct {
	Count   int
	Windows []Window
}

// Windows lists the app's windows. The main window is first, then auxiliary
// windows in creation order.
func Windows(port int) (WindowList, error) {
	resp, err := Cmd(port, "windows")
	if err != nil {
		return WindowList{}, err
	}
	return parseWindowList(resp)
}

// SelectWindow selects the window that later commands on this port operate on.
// Passing "main" returns to the main window. Selection sticks until the next
// SelectWindow, or until the selected window closes (then commands fall back to
// the main window).
func SelectWindow(port int, handle string) error {
	if handle == "" {
		return fmt.Errorf("select window: empty handle")
	}
	resp, err := Cmd(port, "window "+handle)
	if err != nil {
		return err
	}
	if strings.TrimSpace(resp) != "ok" {
		return fmt.Errorf("select window %s: %s", handle, resp)
	}
	return nil
}

// CurrentWindow returns the handle of the window that commands on this port
// operate on. It is empty when the app has no registered windows yet.
func CurrentWindow(port int) (string, error) {
	resp, err := Cmd(port, "window")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp), nil
}

// WaitWindow polls the window list until match returns true. Interval 100ms,
// up to 3s.
func WaitWindow(port int, match func(Window) bool) (Window, error) {
	if match == nil {
		return Window{}, fmt.Errorf("wait window: nil match")
	}
	deadline := time.Now().Add(waitCountDeadline)
	var last WindowList
	var lastErr error
	for {
		list, err := Windows(port)
		last, lastErr = list, err
		if err == nil {
			for _, window := range list.Windows {
				if match(window) {
					return window, nil
				}
			}
		}
		if time.Now().After(deadline) {
			if lastErr != nil {
				return Window{}, fmt.Errorf("wait window: %w", lastErr)
			}
			return Window{}, fmt.Errorf("wait window: no match among %d windows", last.Count)
		}
		time.Sleep(waitCountInterval)
	}
}

func parseWindowList(reply string) (WindowList, error) {
	var result WindowList
	rest := strings.TrimSpace(reply)
	if rest == "" {
		return result, nil
	}
	if strings.HasPrefix(rest, "count:") {
		line, after, found := strings.Cut(rest, "\n")
		count, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "count:")))
		if err != nil {
			return WindowList{}, fmt.Errorf("windows count: %w", err)
		}
		result.Count = count
		if !found {
			return result, nil
		}
		rest = strings.TrimSpace(after)
	}
	if rest == "" {
		return result, nil
	}
	for _, block := range strings.Split(rest, "\n\n") {
		if strings.TrimSpace(block) == "" {
			continue
		}
		window, err := parseWindowRecord(block)
		if err != nil {
			return WindowList{}, err
		}
		result.Windows = append(result.Windows, window)
	}
	return result, nil
}

func parseWindowRecord(record string) (Window, error) {
	var window Window
	for _, line := range strings.Split(record, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		switch key {
		case "id":
			window.ID = val
		case "kind":
			window.Kind = val
		case "title":
			window.Title = val
		case "size":
			var x, y float32
			if _, err := fmt.Sscanf(val, "%f %f", &x, &y); err != nil {
				return Window{}, fmt.Errorf("window size: %w", err)
			}
			window.Size = shirei.Vec2{x, y}
		case "pos":
			var x, y float32
			if _, err := fmt.Sscanf(val, "%f %f", &x, &y); err != nil {
				return Window{}, fmt.Errorf("window pos: %w", err)
			}
			window.Pos = shirei.Vec2{x, y}
			window.HasPos = true
		case "target":
			window.Target = val == "true"
		}
	}
	return window, nil
}
