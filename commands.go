package shirei

import (
	"fmt"
	"os"
)

// Widget commands: an app posts an imperative request at a widget instance,
// and the widget consumes it the way it consumes input — by checking for
// data on its next render. The address has three parts:
//
//   - widget: the widget kind, a scoping/ownership marker — two different
//     widget types may legitimately key off the same app object.
//   - id: the widget instance — the same value the widget uses for its root
//     container id. Must be a typed pointer (or comparable value) to
//     app-owned data, globally unique among live widget instances.
//   - name: the command.
//
// One slot per (widget, id, name): a second post before consumption
// overwrites the first (last wins). Unconsumed commands are dropped at the
// start of the second frame after the post — a command outlives its posting
// frame by exactly one frame, which covers every build order (consumer
// before or after the poster in the tree) without letting stale requests
// lurk: if the target didn't consume by then, nobody will.
//
// Call both functions under the frame lock (UI code, or WithFrameLock).

type _CommandKey struct {
	widget string
	key    any
	name   string
}

type pendingCommand struct {
	arg       any
	postFrame int64
}

var pendingCommands = map[_CommandKey]pendingCommand{}

// PostCommand queues a command for a widget instance. It does NOT wake the loop
// eagerly: at post time it can't know whether the consumer builds later this same
// frame (no follow-up needed) or earlier (needs the next frame). That is decided at
// frame end by pendingCommandNeedsNextFrame, so a command consumed same-frame — the
// common case when the poster builds before its consumer — costs no wake. This is
// what lets an app post a standing query every frame and
// still go idle.
//
// A command posted OUTSIDE a frame (a background goroutine under the frame lock)
// can't be caught by the end-of-frame check, so it wakes the loop directly.
func PostCommand(widget string, key any, name string, arg any) {
	if key == nil {
		return
	}
	pendingCommands[_CommandKey{widget, key, name}] = pendingCommand{arg: arg, postFrame: FrameNumber}
	if !frameInProgress {
		RequestNextFrame()
	}
}

// pendingCommandNeedsNextFrame reports whether a command posted during THIS frame
// is still unconsumed at frame end — i.e. its consumer built before its poster or
// wasn't rendered, so it needs another frame to be delivered. A command whose
// consumer took it out of the queue this frame leaves nothing here, so no wake.
func pendingCommandNeedsNextFrame() bool {
	for _, cmd := range pendingCommands {
		if cmd.postFrame == FrameNumber {
			return true
		}
	}
	return false
}

// TakeCommand consumes a pending command, returning its argument as T.
// Absent → zero, false. An argument of the wrong type is a program bug:
// reported on stderr (like duplicate ids), consumed, and returned as
// zero, false.
func TakeCommand[T any](widget string, key any, name string) (T, bool) {
	var zero T
	commandKey := _CommandKey{widget: widget, key: key, name: name}
	cmd, ok := pendingCommands[commandKey]
	if !ok {
		return zero, false
	}
	delete(pendingCommands, commandKey)
	arg, ok := cmd.arg.(T)
	if !ok {
		fmt.Fprintf(os.Stderr, "shirei: command %s %q argument is %T, taken as %T\n",
			widget, name, cmd.arg, zero)
		return zero, false
	}
	return arg, true
}

// flushStaleCommands runs at frame start: drop anything posted before the
// previous frame.
func flushStaleCommands() {
	if len(pendingCommands) == 0 {
		return
	}
	for key, cmd := range pendingCommands {
		if cmd.postFrame < FrameNumber-1 {
			delete(pendingCommands, key)
		}
	}
}
