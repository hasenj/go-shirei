package shirei

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// inputCmdServer is one drive listener's state. Each listener selects its own
// target window with the `window` command; an empty target means the default
// (the main window, or the active UI when no window is registered).
type inputCmdServer struct {
	target windowHandle
}

func handleInputUDP(server *inputCmdServer, msg []byte) []byte {
	var resp string
	WithFrameLock(func() {
		resp = server.handleInput(string(msg))
	})
	return clipUDPReply(resp)
}

// handleInput dispatches one wire line for this listener. Call with the frame
// lock held.
func (s *inputCmdServer) handleInput(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	cmd, rest, _ := strings.Cut(line, " ")
	rest = strings.TrimSpace(rest)
	switch strings.ToLower(cmd) {
	case "windows":
		if rest != "" {
			return "error: usage: windows"
		}
		_, target := s.targetWindow()
		return formatWindowList(target)
	case "window":
		return s.inputCmdWindow(rest)
	}

	u, _ := s.targetWindow()
	if u == nil {
		// No registered windows: keep the legacy behavior and apply the command
		// to the active UI.
		return HandleInputCommand(line)
	}
	restore := EnterUILocked(u)
	defer restore()
	return HandleInputCommand(line)
}

// targetWindow resolves the listener's selected window and its handle. A target
// that has closed reverts to the main window. With no registered windows it
// returns nil and the caller falls back to the active UI. Call with the frame
// lock held.
func (s *inputCmdServer) targetWindow() (*UI, windowHandle) {
	windowRegistry.RLock()
	if s.target != "" {
		if record := windowRegistry.byHandle[s.target]; record != nil {
			u, handle := record.ui, record.handle
			windowRegistry.RUnlock()
			return u, handle
		}
	}
	var main *windowRecord
	for _, record := range windowRegistry.list {
		if record.kind == windowMain {
			main = record
			break
		}
	}
	windowRegistry.RUnlock()
	if main == nil {
		s.target = ""
		return nil, ""
	}
	s.target = main.handle
	return main.ui, main.handle
}

// inputCmdWindow reads or selects the listener's target window.
func (s *inputCmdServer) inputCmdWindow(rest string) string {
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		_, handle := s.targetWindow()
		return string(handle)
	}
	if len(fields) > 1 {
		return "error: usage: window [handle]"
	}
	handle := windowHandle(fields[0])
	windowRegistry.RLock()
	record := windowRegistry.byHandle[handle]
	windowRegistry.RUnlock()
	if record == nil {
		return "error: no such window: " + fields[0]
	}
	s.target = handle
	return "ok"
}

func formatWindowList(target windowHandle) string {
	snapshots := windowSnapshots()
	head := fmt.Sprintf("count: %d", len(snapshots))
	if len(snapshots) == 0 {
		return head
	}
	const sep = "\n\n"
	budget := maxUDPReply - len(head) - len(sep)
	body := joinWindowRecords(snapshots, target, budget)
	if body == "" {
		return head
	}
	return head + sep + body
}

// joinWindowRecords packs complete window records into budget bytes. Stops
// before the next record that would not fit; never emits a torn record.
func joinWindowRecords(snapshots []windowSnapshot, target windowHandle, budget int) string {
	if budget <= 0 {
		return ""
	}
	const sep = "\n\n"
	var b strings.Builder
	for i, snapshot := range snapshots {
		record := formatWindowRecord(snapshot, target)
		if i == 0 {
			b.WriteString(record)
			continue
		}
		if b.Len()+len(sep)+len(record) > budget {
			break
		}
		b.WriteString(sep)
		b.WriteString(record)
	}
	return b.String()
}

func formatWindowRecord(snapshot windowSnapshot, target windowHandle) string {
	var b strings.Builder
	fmt.Fprintf(&b, "id: %s\n", snapshot.handle)
	kind := "popup"
	if snapshot.kind == windowMain {
		kind = "main"
	}
	fmt.Fprintf(&b, "kind: %s\n", kind)
	if snapshot.title != "" {
		fmt.Fprintf(&b, "title: %s\n", clipOneLine(snapshot.title, 160))
	}
	fmt.Fprintf(&b, "size: %s %s\n", fmtF32(snapshot.size[0]), fmtF32(snapshot.size[1]))
	if snapshot.hasPos {
		fmt.Fprintf(&b, "pos: %s %s\n", fmtF32(snapshot.pos[0]), fmtF32(snapshot.pos[1]))
	}
	if snapshot.handle == target {
		b.WriteString("target: true\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// clipOneLine flattens s to one line and caps it at max bytes without splitting
// a UTF-8 rune.
func clipOneLine(s string, max int) string {
	s = oneLine(s)
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}
