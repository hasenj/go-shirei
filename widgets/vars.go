package widgets

import (
	"bytes"
	"encoding/json"
	"fmt"

	. "go.hasen.dev/shirei"
)

type _DebugPanel struct {
	messages    []string
	position    Vec2
	frameNumber int64
}

var _panel = _DebugPanel{position: Vec2{10, 10}}

// DebugPanel draws the floating, draggable debug overlay holding the messages
// collected via DebugMessage and DebugVar this frame (when show is true), then
// clears them for the next frame. Call it once per frame, typically at the end
// of your UI.
func DebugPanel(show bool) {
	if show && len(_panel.messages) > 0 {
		ContainerWithKey(&_panel, Attrs(FloatVec(_panel.position), Background(0, 0, 0, 0.8), Corners(4), Pad(4), Gap(4), NoAnimate), func() {
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
				Label(msg, TextColor(0, 0, 100, 1), FontSize(10), Fonts(Monospace...))
			}
		})
	} else {
		Nil()
	}
	_panel.messages = nil
	_panel.frameNumber = FrameNumber
}

// DebugMessage adds a line of text to the debug overlay for this frame. It is a
// no-op when DebugPanel isn't being called, so messages don't accumulate.
func DebugMessage(msg string) {
	if FrameNumber > _panel.frameNumber+1 {
		// DebugPanel is not called; don't do anything (don't leak memory)
		return
	}
	_panel.messages = append(_panel.messages, msg)
}

// DebugVar adds a "name: value" line to the debug overlay, formatting value as
// compact JSON.
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
