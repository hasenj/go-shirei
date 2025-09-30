package widgets

import (
	"bytes"
	"encoding/json"
	"fmt"

	. "go.hasen.dev/slay"
	. "go.hasen.dev/slay/tw"
)

type _DebugPanel struct {
	messages []string
	position Vec2
}

var _panel = _DebugPanel{position: Vec2{10, 10}}

func DebugPanel(show bool) {
	if len(_panel.messages) == 0 {
		return
	}

	if show {
		LayoutId(&_panel, TW(FloatV(_panel.position), BG(0, 0, 0, 0.8), BR(4), Pad(4), Gap(4), NoAnimate), func() {
			PressAction()
			if IsActive() {
				_panel.position = Vec2Add(_panel.position, FrameInput.Motion)
			}
			var sz = GetResolvedSize()
			var br = Vec2Add(_panel.position, sz)
			if br[0] > WindowSize[0] {
				_panel.position[0] = WindowSize[0] - sz[0]
			}
			if br[1] > WindowSize[1] {
				_panel.position[1] = WindowSize[1] - sz[1]
			}
			for _, msg := range _panel.messages {
				Label(msg, Clr(0, 0, 100, 1), Sz(10), Fonts(Monospace...))
			}
		})
	}
	_panel.messages = nil
}

func DebugMessage(msg string) {
	_panel.messages = append(_panel.messages, msg)
}

func DebugVar(name string, value any) {
	// TODO: handle structs or other nested objects!!
	DebugMessage(fmt.Sprintf("%s: %v", name, compactJson(value)))
}

func compactJson(value any) string {
	var buf0, _ = json.MarshalIndent(value, "", "")
	var buf bytes.Buffer
	json.Compact(&buf, buf0)
	return buf.String()
}
