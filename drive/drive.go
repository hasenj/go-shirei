// Package drive is a loopback client for AcceptInputCommands. It sends
// kernel commands (move, key_down, show, …) and composes hover/click/type
// by sleeping between beats while the app produces frames. Start builds
// and launches a main package with SHIREI_DRIVE_PORT for windowed tests.
//
// Commands apply to the listener's selected window: the main window by
// default. Windows lists the app's windows and SelectWindow targets an
// auxiliary window for later commands.
package drive

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	g "go.hasen.dev/generic"
	"go.hasen.dev/shirei"
	"go.hasen.dev/udplib"
)

const (
	// Pause is the wait after pointer/key injects.
	Pause = 150 * time.Millisecond
	// Timeout is the UDP read deadline.
	Timeout = 2 * time.Second
)

// FreePort returns an unused loopback UDP port. The port is released before
// return; a later bind can still fail if something else takes it.
func FreePort() (int, error) {
	return udplib.FreePort()
}

func traceUDP(sent, recv string) {
	if !testing.Verbose() || !g.EnvTruthy(EnvCmdLog) || sent == "ping" {
		return
	}
	var dim, reset = "\x1b[2m", "\x1b[0m"
	stamp := time.Now().Format("15:04:05.000")
	fmt.Fprintf(os.Stderr, "%s%s > %s%s\n", dim, stamp, reset, sent)
	recv = strings.TrimSpace(recv)
	if recv != "" && recv != "ok" {
		for _, line := range strings.Split(recv, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			fmt.Fprintf(os.Stderr, "%s<%s %s\n", dim, reset, line)
		}
	}
	fmt.Fprintln(os.Stderr)
}

// Cmd sends one kernel line and returns the reply. Injects reply empty
// (UDP delivery is the success flag). Replies that start with "error:"
// become an error. With go test -v and SHIREI_CMD_LOG set, each command
// is printed (except ping); empty inject replies are omitted.
func Cmd(port int, line string) (string, error) {
	b, ok := udplib.SendUDP(port, []byte(line), nil, Timeout)
	if !ok {
		traceUDP(line, "(no udp response)")
		recordCmd(line, "(no udp response)")
		return "", errors.New("no udp response")
	}
	resp := string(b)
	traceUDP(line, resp)
	recordCmd(line, resp)
	if strings.HasPrefix(resp, "error:") {
		return "", errors.New(strings.TrimSpace(strings.TrimPrefix(resp, "error:")))
	}
	return resp, nil
}

func inject(port int, line string) error {
	resp, err := Cmd(port, line)
	if err != nil {
		return err
	}
	if strings.TrimSpace(resp) != "" {
		return fmt.Errorf("%s: %s", line, resp)
	}
	time.Sleep(Pause)
	return nil
}

func Move(port int, x, y float32) error {
	return inject(port, fmt.Sprintf("move %s %s", fmtF32(x), fmtF32(y)))
}

func Down(port int, button ...string) error {
	return inject(port, mouseLine("mouse_down", button))
}

func Up(port int, button ...string) error {
	return inject(port, mouseLine("mouse_up", button))
}

func mouseLine(cmd string, button []string) string {
	if len(button) > 0 && button[0] != "" {
		return cmd + " " + button[0]
	}
	return cmd
}

func Wheel(port int, dy float32) error {
	return inject(port, fmt.Sprintf("wheel %s", fmtF32(dy)))
}

func keyLine(cmd, name string, mods []string) string {
	line := cmd + " " + name
	if len(mods) > 0 {
		line += " " + strings.Join(mods, " ")
	}
	return line
}

func KeyDown(port int, name string, mods ...string) error {
	return inject(port, keyLine("key_down", name, mods))
}

func KeyUp(port int, name string, mods ...string) error {
	return inject(port, keyLine("key_up", name, mods))
}

// Key is a tap: key_down (Pause), Tick, key_up (Pause).
func Key(port int, name string, mods ...string) error {
	if err := KeyDown(port, name, mods...); err != nil {
		return err
	}
	Tick()
	return KeyUp(port, name, mods...)
}

func Text(port int, s string) error {
	return inject(port, "text "+strconv.Quote(s))
}

// Tick sleeps one display frame (~1/60s). Input already requests the
// follow-up produce in core; this does not send a kernel command.
func Tick() {
	time.Sleep(time.Second / 60)
}

// Frames sleeps n display frames.
func Frames(n int) {
	if n <= 0 {
		return
	}
	time.Sleep(time.Duration(n) * time.Second / 60)
}

// Quit asks the app to exit via generic.ExitWithCleanup.
func Quit(port int) error {
	_, err := Cmd(port, "quit")
	return err
}

// Screenshot writes a PNG of the last completed frame on the app side
// (software-rendered, same oracle as RenderToPNG). Path is in the app
// process; use an absolute path from tests. Reply is ok.
func Screenshot(port int, path string) error {
	if path == "" {
		return errors.New("screenshot: empty path")
	}
	resp, err := Cmd(port, "screenshot "+path)
	if err != nil {
		return err
	}
	if strings.TrimSpace(resp) != "ok" {
		return fmt.Errorf("screenshot: %s", resp)
	}
	return nil
}

func Ping(port int) error {
	resp, err := Cmd(port, "ping")
	if err != nil {
		return err
	}
	if resp != "pong" {
		return fmt.Errorf("ping: %s", resp)
	}
	return nil
}

func Show(port int, q string) (shirei.AccessNode, error) {
	resp, err := Cmd(port, "show "+q)
	if err != nil {
		return shirei.AccessNode{}, err
	}
	return parseRecord(resp)
}

func Focused(port int) ([]uint64, error) {
	resp, err := Cmd(port, "focused")
	if err != nil {
		return nil, err
	}
	return parseIDList(resp)
}

func Hovered(port int) ([]uint64, error) {
	resp, err := Cmd(port, "hovered")
	if err != nil {
		return nil, err
	}
	return parseIDList(resp)
}

func parseIDList(s string) ([]uint64, error) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return nil, nil
	}
	out := make([]uint64, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimPrefix(f, "#")
		id, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("id list: %w", err)
		}
		out = append(out, id)
	}
	return out, nil
}

// QueryResult is a query reply: Count is every match this frame, Nodes
// are the records that fit in the UDP datagram (whole records only).
type QueryResult struct {
	Count int
	Nodes []shirei.AccessNode
}

func Query(port int, q string) (QueryResult, error) {
	line := "query"
	if q != "" {
		line = "query " + q
	}
	resp, err := Cmd(port, line)
	if err != nil {
		return QueryResult{}, err
	}
	return parseQueryReply(resp)
}

// Count is the number of query matches this frame. No records.
func Count(port int, q string) (int, error) {
	line := "count"
	if q != "" {
		line = "count " + q
	}
	resp, err := Cmd(port, line)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(resp))
	if err != nil {
		return 0, fmt.Errorf("count: %w", err)
	}
	return n, nil
}

const (
	waitCountInterval = 100 * time.Millisecond
	waitCountDeadline = 3 * time.Second
)

// WaitCount polls Count until it equals want. Interval 100ms, up to 3s.
func WaitCount(port int, q string, want int) error {
	deadline := time.Now().Add(waitCountDeadline)
	var last int
	var lastErr error
	for {
		n, err := Count(port, q)
		last, lastErr = n, err
		if err == nil && n == want {
			return nil
		}
		if time.Now().After(deadline) {
			if lastErr != nil {
				return fmt.Errorf("count %q: %w", q, lastErr)
			}
			return fmt.Errorf("count %q: got %d want %d", q, last, want)
		}
		time.Sleep(waitCountInterval)
	}
}

func parseQueryReply(s string) (QueryResult, error) {
	var r QueryResult
	s = strings.TrimSpace(s)
	if s == "" {
		return r, nil
	}
	rest := s
	if strings.HasPrefix(s, "count:") {
		line, after, found := strings.Cut(s, "\n")
		n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "count:")))
		if err != nil {
			return r, fmt.Errorf("count: %w", err)
		}
		r.Count = n
		if !found {
			return r, nil
		}
		rest = strings.TrimSpace(after)
	}
	if rest == "" {
		return r, nil
	}
	chunks := strings.Split(rest, "\n\n")
	r.Nodes = make([]shirei.AccessNode, 0, len(chunks))
	for _, c := range chunks {
		if strings.TrimSpace(c) == "" {
			continue
		}
		n, err := parseRecord(c)
		if err != nil {
			return QueryResult{}, err
		}
		r.Nodes = append(r.Nodes, n)
	}
	return r, nil
}

func rectCenter(r shirei.Rect) shirei.Vec2 {
	return shirei.Vec2{r.Origin[0] + r.Size[0]/2, r.Origin[1] + r.Size[1]/2}
}

func hoverNode(port int, n shirei.AccessNode) (shirei.AccessNode, error) {
	c := rectCenter(n.Rect)
	if err := Move(port, c[0], c[1]); err != nil {
		return shirei.AccessNode{}, err
	}
	return n, nil
}

func clickNode(port int, n shirei.AccessNode) (shirei.AccessNode, error) {
	if _, err := hoverNode(port, n); err != nil {
		return shirei.AccessNode{}, err
	}
	if err := Down(port); err != nil {
		return shirei.AccessNode{}, err
	}
	if err := Up(port); err != nil {
		return shirei.AccessNode{}, err
	}
	return n, nil
}

// Hover moves to the center of the first show match.
func Hover(port int, q string) (shirei.AccessNode, error) {
	n, err := Show(port, q)
	if err != nil {
		return shirei.AccessNode{}, err
	}
	return hoverNode(port, n)
}

// Click shows q (first match) and clicks its rect center.
func Click(port int, q string) (shirei.AccessNode, error) {
	n, err := Show(port, q)
	if err != nil {
		return shirei.AccessNode{}, err
	}
	return clickNode(port, n)
}

// ClickOne waits until Count(q) == 1, then clicks that record.
func ClickOne(port int, q string) (shirei.AccessNode, error) {
	if err := WaitCount(port, q, 1); err != nil {
		return shirei.AccessNode{}, err
	}
	res, err := Query(port, q)
	if err != nil {
		return shirei.AccessNode{}, err
	}
	if res.Count != 1 {
		return shirei.AccessNode{}, fmt.Errorf("query %q: count %d want 1", q, res.Count)
	}
	if len(res.Nodes) != 1 {
		return shirei.AccessNode{}, fmt.Errorf("query %q: count 1 but %d records in datagram", q, len(res.Nodes))
	}
	return clickNode(port, res.Nodes[0])
}

// Drag hovers from, presses, moves to to, releases.
func Drag(port int, from, to string) error {
	if _, err := Hover(port, from); err != nil {
		return err
	}
	if err := Down(port); err != nil {
		return err
	}
	if _, err := Hover(port, to); err != nil {
		return err
	}
	return Up(port)
}

// Type clicks q with ClickOne, then sends each rune as its own text command.
func Type(port int, q, text string) error {
	if _, err := ClickOne(port, q); err != nil {
		return err
	}
	for _, r := range text {
		if err := Text(port, string(r)); err != nil {
			return err
		}
	}
	return nil
}

// TabUntil waits until q is unique, snapshots that record's identity,
// then taps Tab until the focused leaf is that id. After each Tab it
// waits two frames, then reads focused once. Wrap-around (back to the
// starting leaf without hitting the target) is a failure.
func TabUntil(port int, q string) error {
	if err := WaitCount(port, q, 1); err != nil {
		return err
	}
	target, err := Show(port, q)
	if err != nil {
		return err
	}
	if target.ID == 0 {
		return fmt.Errorf("tab until %q: no id", q)
	}
	orig, err := Focused(port)
	if err != nil {
		return err
	}
	var start uint64
	if len(orig) > 0 {
		start = orig[0]
	}
	if start == target.ID {
		return nil
	}
	left := false
	for range 64 {
		if err := Key(port, "Tab"); err != nil {
			return err
		}
		Frames(2)
		ids, err := Focused(port)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			continue
		}
		leaf := ids[0]
		if leaf == target.ID {
			return nil
		}
		if start != 0 && leaf == start {
			if left {
				return fmt.Errorf("tab until %q: wrapped around", q)
			}
			continue
		}
		left = true
	}
	return fmt.Errorf("tab until %q: cap 64", q)
}

func parseRecord(s string) (shirei.AccessNode, error) {
	var n shirei.AccessNode
	for _, line := range strings.Split(s, "\n") {
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
			val = strings.TrimPrefix(val, "#")
			id, err := strconv.ParseUint(val, 10, 64)
			if err != nil {
				return shirei.AccessNode{}, fmt.Errorf("id: %w", err)
			}
			n.ID = id
		case "path":
			n.Path = val
			toks := strings.Fields(val)
			if len(toks) > 0 && toks[len(toks)-1] != "-" {
				n.Name = toks[len(toks)-1]
			}
		case "rect":
			var x, y, w, h float32
			if _, err := fmt.Sscanf(val, "%f %f %f %f", &x, &y, &w, &h); err != nil {
				return shirei.AccessNode{}, fmt.Errorf("rect: %w", err)
			}
			n.Rect = shirei.Rect{Origin: shirei.Vec2{x, y}, Size: shirei.Vec2{w, h}}
		case "focused":
			n.Focused = val == "true"
		case "hovered":
			n.Hovered = val == "true"
		case "z":
			z, err := strconv.ParseFloat(val, 32)
			if err != nil {
				return shirei.AccessNode{}, fmt.Errorf("z: %w", err)
			}
			n.Z = float32(z)
		case "role":
			n.Role = val
		case "checked":
			n.Checked = val == "true"
		case "value":
			n.Value = val
		case "scroll":
			n.ScrollPort = true
			val = strings.Replace(val, "max:", "", 1)
			var ox, oy, mx, my float32
			if _, err := fmt.Sscanf(val, "%f %f %f %f", &ox, &oy, &mx, &my); err != nil {
				return shirei.AccessNode{}, fmt.Errorf("scroll: %w", err)
			}
			n.Scroll = shirei.Vec2{ox, oy}
			n.ScrollMax = shirei.Vec2{mx, my}
		}
	}
	return n, nil
}

func fmtF32(v float32) string {
	return strconv.FormatFloat(float64(v), 'g', 6, 32)
}
