package shirei

import (
	"bytes"
	"fmt"
	"image/png"
	"os"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	g "go.hasen.dev/generic"
	"go.hasen.dev/udplib"
)

// maxUDPReply matches udplib's datagram buffer. Replies are complete
// records only (cut at \n\n), never a torn last line.
const maxUDPReply = 1024

var inputKeyNames = map[string]KeyCode{
	"tab":       KeyTab,
	"space":     KeySpace,
	"enter":     KeyEnter,
	"return":    KeyEnter,
	"escape":    KeyEscape,
	"esc":       KeyEscape,
	"left":      KeyLeft,
	"right":     KeyRight,
	"up":        KeyUp,
	"down":      KeyDown,
	"home":      KeyHome,
	"end":       KeyEnd,
	"backspace": KeyDeleteBackward,
	"delete":    KeyDeleteForward,
	"pageup":    KeyPageUp,
	"pagedown":  KeyPageDown,
	"f1":        KeyF1,
	"f2":        KeyF2,
	"f3":        KeyF3,
	"f4":        KeyF4,
	"f5":        KeyF5,
	"f6":        KeyF6,
	"f7":        KeyF7,
	"f8":        KeyF8,
	"f9":        KeyF9,
	"f10":       KeyF10,
	"f11":       KeyF11,
	"f12":       KeyF12,
}

// HandleInputCommand applies one kernel command to the active UI. Pointer and
// key commands write Host.Input / FrameInput for the next produce; query /
// count / show / focused / hovered / screenshot read the last completed
// frame. The caller holds the frame lock (AcceptInputCommands does; tests
// call this between RunFrameFn on the UI goroutine). It does not wait for
// another produce.
func HandleInputCommand(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	cmd, rest, _ := strings.Cut(line, " ")
	rest = strings.TrimSpace(rest)
	switch strings.ToLower(cmd) {
	case "ping":
		return "pong"
	case "move":
		return inputCmdMove(rest)
	case "mouse_down":
		return inputCmdMouse(MouseClick, rest)
	case "mouse_up":
		return inputCmdMouse(MouseRelease, rest)
	case "wheel":
		return inputCmdWheel(rest)
	case "key_down":
		return inputCmdKey(rest, true)
	case "key_up":
		return inputCmdKey(rest, false)
	case "text":
		return inputCmdText(rest)
	case "query":
		return inputCmdQuery(rest)
	case "count":
		return inputCmdCount(rest)
	case "show":
		return inputCmdShow(rest)
	case "focused":
		if rest != "" {
			return "error: usage: focused"
		}
		return inputCmdFocused()
	case "hovered":
		if rest != "" {
			return "error: usage: hovered"
		}
		return inputCmdHovered()
	case "profile_start":
		return inputCmdProfileStart(rest)
	case "profile_stop":
		if rest != "" {
			return "error: usage: profile_stop"
		}
		if err := stopInputCPUProfile(); err != nil {
			return "error: " + err.Error()
		}
		return "ok"
	case "screenshot":
		return inputCmdScreenshot(rest)
	case "quit", "exit":
		go g.ExitWithCleanup(0)
		return ""
	default:
		return "error: unknown command: " + cmd
	}
}

func inputCmdBeginInject() {
	g.Reset(&ui.Host.FrameInput)
	ui.Host.Input.Modifiers = 0
	RequestNextFrame()
	wakeBackend()
}

func inputCmdMove(rest string) string {
	fields := strings.Fields(rest)
	if len(fields) != 2 {
		return "error: usage: move <x> <y>"
	}
	x, err1 := strconv.ParseFloat(fields[0], 32)
	y, err2 := strconv.ParseFloat(fields[1], 32)
	if err1 != nil || err2 != nil {
		return "error: usage: move <x> <y>"
	}
	return inputCmdMovePoint(Vec2{float32(x), float32(y)})
}

func inputCmdMovePoint(p Vec2) string {
	inputCmdBeginInject()
	prev := ui.Host.Input.MousePoint
	ui.Host.FrameInput.Motion = Vec2Sub(p, prev)
	ui.Host.Input.MousePoint = p
	return ""
}

func inputCmdMouse(a MouseAction, rest string) string {
	btn := MousePrimary
	if s := strings.TrimSpace(rest); s != "" {
		fields := strings.Fields(s)
		if len(fields) != 1 {
			return "error: usage: mouse_down|mouse_up [primary|secondary|tertiary]"
		}
		parsed, err := parseMouseButton(fields[0])
		if err != "" {
			return err
		}
		btn = parsed
	}
	inputCmdBeginInject()
	ui.Host.FrameInput.Mouse = a
	ui.Host.Input.MouseButton = btn
	return ""
}

func parseMouseButton(s string) (MouseButton, string) {
	switch strings.ToLower(s) {
	case "primary", "1", "left":
		return MousePrimary, ""
	case "secondary", "2", "right":
		return MouseSecondary, ""
	case "tertiary", "3", "middle":
		return MouseTertiary, ""
	default:
		return 0, "error: unknown mouse button: " + s
	}
}

func inputCmdWheel(rest string) string {
	fields := strings.Fields(rest)
	if len(fields) != 1 {
		return "error: usage: wheel <dy>"
	}
	dy, err := strconv.ParseFloat(fields[0], 32)
	if err != nil {
		return "error: usage: wheel <dy>"
	}
	inputCmdBeginInject()
	ui.Host.FrameInput.Scroll = Vec2{0, float32(dy)}
	return ""
}

func inputCmdKey(rest string, down bool) string {
	usage := "error: usage: key_down|key_up <Name> [mods]"
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return usage
	}
	code, ok := lookupInputKey(fields[0])
	if !ok {
		return "error: unknown key: " + fields[0]
	}
	var mods Modifiers
	for _, m := range fields[1:] {
		switch strings.ToLower(m) {
		case "shift":
			mods |= ModShift
		case "alt":
			mods |= ModAlt
		case "ctrl", "control":
			mods |= ModCtrl
		case "cmd", "command":
			mods |= ModCmd
		case "primary":
			mods |= PrimaryMod()
		default:
			return "error: unknown mod: " + m
		}
	}
	inputCmdBeginInject()
	ui.Host.Input.Modifiers = mods
	if down {
		ui.Host.FrameInput.Key = code
		if !slices.Contains(ui.Host.Input.DownKeys, code) {
			ui.Host.Input.DownKeys = append(ui.Host.Input.DownKeys, code)
		}
	} else {
		ui.Host.Input.DownKeys = slices.DeleteFunc(ui.Host.Input.DownKeys, func(k KeyCode) bool {
			return k == code
		})
	}
	return ""
}

func lookupInputKey(name string) (KeyCode, bool) {
	if code, ok := inputKeyNames[strings.ToLower(name)]; ok {
		return code, true
	}
	if utf8.RuneCountInString(name) != 1 {
		return 0, false
	}
	r, _ := utf8.DecodeRuneInString(name)
	switch {
	case r >= 'a' && r <= 'z':
		return KeyCode('A' + r - 'a'), true
	case r >= 'A' && r <= 'Z':
		return KeyCode(r), true
	case r >= '0' && r <= '9':
		return KeyCode(r), true
	case strings.ContainsRune("-=[]\\;'`,./", r):
		return KeyCode(r), true
	}
	return 0, false
}

func inputCmdText(rest string) string {
	s := unquoteArg(rest)
	if s == "" {
		return "error: usage: text <s>"
	}
	inputCmdBeginInject()
	ui.Host.FrameInput.Text = s
	return ""
}

func inputCmdQuery(rest string) string {
	return formatQueryReply(queryAccess(rest))
}

func inputCmdCount(rest string) string {
	return strconv.Itoa(len(queryAccess(rest)))
}

func queryAccess(rest string) []AccessNode {
	q := unquoteArg(rest)
	if q == "" {
		return append([]AccessNode(nil), ui.access...)
	}
	return QueryContainers(q)
}

func formatQueryReply(nodes []AccessNode) string {
	head := fmt.Sprintf("count: %d", len(nodes))
	if len(nodes) == 0 {
		return head
	}
	const sep = "\n\n"
	budget := maxUDPReply - len(head) - len(sep)
	body := joinAccessNodes(nodes, budget)
	if body == "" {
		return head
	}
	return head + sep + body
}

// joinAccessNodes packs complete records into budget bytes. Stops before
// the next record that would not fit; never emits a torn record.
func joinAccessNodes(nodes []AccessNode, budget int) string {
	if budget <= 0 {
		return ""
	}
	const sep = "\n\n"
	var b strings.Builder
	for i, n := range nodes {
		rec := formatAccessNode(n)
		if i == 0 {
			b.WriteString(rec)
			continue
		}
		if b.Len()+len(sep)+len(rec) > budget {
			break
		}
		b.WriteString(sep)
		b.WriteString(rec)
	}
	return b.String()
}

func clipUDPReply(s string) []byte {
	out := []byte(s)
	if len(out) <= maxUDPReply {
		return out
	}
	out = out[:maxUDPReply]
	if i := bytes.LastIndex(out, []byte("\n\n")); i >= 0 {
		return out[:i]
	}
	if i := bytes.LastIndexByte(out, '\n'); i > 0 {
		return out[:i]
	}
	return []byte("error: reply too large")
}

func inputCmdShow(rest string) string {
	q := unquoteArg(rest)
	if q == "" {
		return "error: usage: show <q>"
	}
	n, ok := QueryContainer(q)
	if !ok {
		return "error: no match"
	}
	return formatAccessNode(n)
}

func inputCmdFocused() string {
	return formatIDList(FocusIDs())
}

func inputCmdHovered() string {
	return formatIDList(HoverIDs())
}

func formatIDList(ids []uint64) string {
	if len(ids) == 0 {
		return ""
	}
	var b strings.Builder
	for i, id := range ids {
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "#%d", id)
	}
	return b.String()
}

func unquoteArg(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' {
		if u, err := strconv.Unquote(s); err == nil {
			return u
		}
	}
	return s
}

func formatAccessNode(n AccessNode) string {
	var b strings.Builder
	fmt.Fprintf(&b, "id: #%d\n", n.ID)
	fmt.Fprintf(&b, "path: %s\n", n.Path)
	fmt.Fprintf(&b, "rect: %s %s %s %s\n",
		fmtF32(n.Rect.Origin[0]), fmtF32(n.Rect.Origin[1]),
		fmtF32(n.Rect.Size[0]), fmtF32(n.Rect.Size[1]))
	if n.Focused {
		b.WriteString("focused: true\n")
	}
	if n.Hovered {
		b.WriteString("hovered: true\n")
	}
	if n.Z != 0 {
		fmt.Fprintf(&b, "z: %s\n", fmtF32(n.Z))
	}
	if n.Role != "" {
		fmt.Fprintf(&b, "role: %s\n", n.Role)
	}
	if n.Checked {
		b.WriteString("checked: true\n")
	}
	if n.Value != "" {
		fmt.Fprintf(&b, "value: %s\n", oneLine(n.Value))
	}
	if n.ScrollPort {
		fmt.Fprintf(&b, "scroll: %s %s max: %s %s\n",
			fmtF32(n.Scroll[0]), fmtF32(n.Scroll[1]),
			fmtF32(n.ScrollMax[0]), fmtF32(n.ScrollMax[1]))
	}
	direct := ui.focused != nil && n.ID == ui.focused.serial
	if direct && ui.Host.WantsKeyboard && ui.Host.CaretHeight > 0 {
		fmt.Fprintf(&b, "caret: %s %s\n", fmtF32(ui.Host.CaretPos[0]), fmtF32(ui.Host.CaretPos[1]))
		fmt.Fprintf(&b, "caret_h: %s\n", fmtF32(ui.Host.CaretHeight))
	}
	if direct {
		if c := lastFrameCopy(); c != "" {
			fmt.Fprintf(&b, "copy: %s\n", oneLine(c))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func fmtF32(v float32) string {
	return strconv.FormatFloat(float64(v), 'g', 6, 32)
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

// lastFrameCopy is the clipboard-copy request from the last completed frame.
// Callers already hold the frame lock (input commands run under WithFrameLock).
func lastFrameCopy() string {
	return ui.lastCopy
}

func inputCmdScreenshot(path string) string {
	if path == "" {
		return "error: usage: screenshot <path>"
	}
	if ui.FrameNumber == 0 {
		return "error: no frame"
	}
	scale := ui.Host.WindowScale
	if scale <= 0 {
		scale = 1
	}
	devW := int(Roundf32(ui.Host.WindowSize[0] * scale))
	devH := int(Roundf32(ui.Host.WindowSize[1] * scale))
	if devW < 1 || devH < 1 {
		return "error: no frame"
	}
	var rend SoftRenderer
	fb := rend.Render(ui.surfaces, ui.glyphRuns, devW, devH, scale)
	f, err := os.Create(path)
	if err != nil {
		return "error: " + err.Error()
	}
	defer f.Close()
	if err := png.Encode(f, fb.ToRGBA()); err != nil {
		return "error: " + err.Error()
	}
	return "ok"
}

// AcceptInputCommands starts a loopback UDP request/response server on port
// (127.0.0.1 only; non-loopback senders are dropped). Each datagram is one
// kernel command; the reply is the handler return value. Opt-in: core does
// not open a port unless something calls this. Compatible with msg_udp.
func AcceptInputCommands(port int) {
	conn := udplib.Listen(port)
	go udplib.Serve(conn, new(struct{}), handleInputUDP, nil)
}

func handleInputUDP(_ *struct{}, msg []byte) []byte {
	var resp string
	WithFrameLock(func() {
		resp = HandleInputCommand(string(msg))
	})
	return clipUDPReply(resp)
}
