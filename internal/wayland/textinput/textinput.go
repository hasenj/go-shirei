// Package textinput implements the unstable text-input-v3 protocol
// (zwp_text_input_manager_v3 / zwp_text_input_v3). Hand-written in the same
// style as cursorshape/: shirei needs only the client half, and owning the
// binding keeps the event loop free of external generators.
//
// Protocol: https://wayland.app/protocols/text-input-unstable-v3
//
//	Manager: destroy(0), get_text_input(1, new_id, seat)
//	TextInput requests: destroy(0), enable(1), disable(2),
//	  set_surrounding_text(3), set_text_change_cause(4), set_content_type(5),
//	  set_cursor_rectangle(6), commit(7)
//	TextInput events: enter(0), leave(1), preedit_string(2), commit_string(3),
//	  delete_surrounding_text(4), done(5)
package textinput

import (
	"errors"
	"sync"

	"go.hasen.dev/shirei/internal/wayland/wl"
)

// Interface is the global advertised in the registry.
const Interface = "zwp_text_input_manager_v3"

// Content purpose / hint constants used by set_content_type (v1 uses normal).
const (
	ContentHintNone     = 0
	ContentPurposeNormal = 0
)

// ChangeCauseOther is set_text_change_cause's "other" value (not input-method).
const ChangeCauseOther = 1

var errNoContext = errors.New("textinput: proxy has no context")

// Manager is zwp_text_input_manager_v3 — factory for TextInput objects.
type Manager struct{ wl.BaseProxy }

func (*Manager) Dispatch(*wl.Event) {}

// TextInput is zwp_text_input_v3 for one seat.
type TextInput struct {
	wl.BaseProxy
	mu       sync.RWMutex
	enters   map[EnterHandler]struct{}
	leaves   map[LeaveHandler]struct{}
	preedits map[PreeditStringHandler]struct{}
	commits  map[CommitStringHandler]struct{}
	deletes  map[DeleteSurroundingTextHandler]struct{}
	dones    map[DoneHandler]struct{}
}

func (t *TextInput) init() {
	t.enters = make(map[EnterHandler]struct{})
	t.leaves = make(map[LeaveHandler]struct{})
	t.preedits = make(map[PreeditStringHandler]struct{})
	t.commits = make(map[CommitStringHandler]struct{})
	t.deletes = make(map[DeleteSurroundingTextHandler]struct{})
	t.dones = make(map[DoneHandler]struct{})
}

// BindManager binds zwp_text_input_manager_v3 from the registry.
func BindManager(registry *wl.Registry, name, version uint32) (*Manager, error) {
	ctx := registry.Context()
	if ctx == nil {
		return nil, errNoContext
	}
	if version > 1 {
		version = 1
	}
	mgr := &Manager{}
	ctx.Register(mgr)
	if err := registry.Bind(name, Interface, version, mgr); err != nil {
		return nil, err
	}
	return mgr, nil
}

// GetTextInput creates a text-input object for the given seat.
func (m *Manager) GetTextInput(seat *wl.Seat) (*TextInput, error) {
	ctx := m.Context()
	if ctx == nil {
		return nil, errNoContext
	}
	ti := &TextInput{}
	ti.init()
	ctx.Register(ti)
	if err := ctx.SendRequest(m, 1, ti, seat); err != nil {
		return nil, err
	}
	return ti, nil
}

// Destroy destroys the manager.
func (m *Manager) Destroy() error {
	ctx := m.Context()
	if ctx == nil {
		return errNoContext
	}
	return ctx.SendRequest(m, 0)
}

// Destroy destroys the text-input object.
func (t *TextInput) Destroy() error {
	ctx := t.Context()
	if ctx == nil {
		return errNoContext
	}
	return ctx.SendRequest(t, 0)
}

// Enable requests text input on the surface from the last enter event.
// Double-buffered: applied on the next Commit.
func (t *TextInput) Enable() error {
	ctx := t.Context()
	if ctx == nil {
		return errNoContext
	}
	return ctx.SendRequest(t, 1)
}

// Disable turns text input off for the current surface.
// Double-buffered: applied on the next Commit.
func (t *TextInput) Disable() error {
	ctx := t.Context()
	if ctx == nil {
		return errNoContext
	}
	return ctx.SendRequest(t, 2)
}

// SetSurroundingText publishes document text around the caret (UTF-8 byte offsets).
// Double-buffered: applied on the next Commit. Optional for v1.
func (t *TextInput) SetSurroundingText(text string, cursor, anchor int32) error {
	ctx := t.Context()
	if ctx == nil {
		return errNoContext
	}
	return ctx.SendRequest(t, 3, text, cursor, anchor)
}

// SetTextChangeCause tells the compositor why surrounding text changed.
// Double-buffered: applied on the next Commit.
func (t *TextInput) SetTextChangeCause(cause uint32) error {
	ctx := t.Context()
	if ctx == nil {
		return errNoContext
	}
	return ctx.SendRequest(t, 4, cause)
}

// SetContentType sets content purpose/hint. Double-buffered: applied on Commit.
func (t *TextInput) SetContentType(hint, purpose uint32) error {
	ctx := t.Context()
	if ctx == nil {
		return errNoContext
	}
	return ctx.SendRequest(t, 5, hint, purpose)
}

// SetCursorRectangle sets the caret area in surface-local coordinates.
// Double-buffered: applied on the next Commit.
func (t *TextInput) SetCursorRectangle(x, y, width, height int32) error {
	ctx := t.Context()
	if ctx == nil {
		return errNoContext
	}
	return ctx.SendRequest(t, 6, x, y, width, height)
}

// Commit atomically applies pending state requests. Each call increments the
// serial the compositor echoes in done events.
func (t *TextInput) Commit() error {
	ctx := t.Context()
	if ctx == nil {
		return errNoContext
	}
	return ctx.SendRequest(t, 7)
}

// Dispatch routes compositor events to registered handlers.
func (t *TextInput) Dispatch(event *wl.Event) {
	switch event.Opcode {
	case 0: // enter
		ev := EnterEvent{Surface: wl.SafeCast[*wl.Surface](event.Proxy(t.Context()))}
		t.mu.RLock()
		for h := range t.enters {
			h.HandleTextInputEnter(ev)
		}
		t.mu.RUnlock()
	case 1: // leave
		ev := LeaveEvent{Surface: wl.SafeCast[*wl.Surface](event.Proxy(t.Context()))}
		t.mu.RLock()
		for h := range t.leaves {
			h.HandleTextInputLeave(ev)
		}
		t.mu.RUnlock()
	case 2: // preedit_string
		ev := PreeditStringEvent{
			Text:        event.String(),
			CursorBegin: event.Int32(),
			CursorEnd:   event.Int32(),
		}
		t.mu.RLock()
		for h := range t.preedits {
			h.HandleTextInputPreeditString(ev)
		}
		t.mu.RUnlock()
	case 3: // commit_string
		ev := CommitStringEvent{Text: event.String()}
		t.mu.RLock()
		for h := range t.commits {
			h.HandleTextInputCommitString(ev)
		}
		t.mu.RUnlock()
	case 4: // delete_surrounding_text
		ev := DeleteSurroundingTextEvent{
			BeforeLength: event.Uint32(),
			AfterLength:  event.Uint32(),
		}
		t.mu.RLock()
		for h := range t.deletes {
			h.HandleTextInputDeleteSurroundingText(ev)
		}
		t.mu.RUnlock()
	case 5: // done
		ev := DoneEvent{Serial: event.Uint32()}
		t.mu.RLock()
		for h := range t.dones {
			h.HandleTextInputDone(ev)
		}
		t.mu.RUnlock()
	}
}

// --- events -----------------------------------------------------------------

// EnterEvent is zwp_text_input_v3.enter — text-input focus on a surface.
type EnterEvent struct {
	Surface *wl.Surface
}

// LeaveEvent is zwp_text_input_v3.leave — text-input focus left a surface.
type LeaveEvent struct {
	Surface *wl.Surface
}

// PreeditStringEvent is pending composition text (double-buffered until done).
// CursorBegin/CursorEnd are UTF-8 byte offsets into Text; both -1 means hide caret.
type PreeditStringEvent struct {
	Text        string
	CursorBegin int32
	CursorEnd   int32
}

// CommitStringEvent is pending committed text (double-buffered until done).
type CommitStringEvent struct {
	Text string
}

// DeleteSurroundingTextEvent asks to delete bytes around the caret (v1: ignored).
type DeleteSurroundingTextEvent struct {
	BeforeLength uint32
	AfterLength  uint32
}

// DoneEvent applies the pending preedit/commit/delete state.
type DoneEvent struct {
	Serial uint32
}

// --- handlers ---------------------------------------------------------------

type EnterHandler interface{ HandleTextInputEnter(EnterEvent) }
type LeaveHandler interface{ HandleTextInputLeave(LeaveEvent) }
type PreeditStringHandler interface {
	HandleTextInputPreeditString(PreeditStringEvent)
}
type CommitStringHandler interface {
	HandleTextInputCommitString(CommitStringEvent)
}
type DeleteSurroundingTextHandler interface {
	HandleTextInputDeleteSurroundingText(DeleteSurroundingTextEvent)
}
type DoneHandler interface{ HandleTextInputDone(DoneEvent) }

// Listener is the union of all text-input event handlers.
type Listener interface {
	EnterHandler
	LeaveHandler
	PreeditStringHandler
	CommitStringHandler
	DeleteSurroundingTextHandler
	DoneHandler
}

// AddListener registers h for every text-input event.
func (t *TextInput) AddListener(h Listener) {
	if h == nil {
		return
	}
	t.mu.Lock()
	t.enters[h] = struct{}{}
	t.leaves[h] = struct{}{}
	t.preedits[h] = struct{}{}
	t.commits[h] = struct{}{}
	t.deletes[h] = struct{}{}
	t.dones[h] = struct{}{}
	t.mu.Unlock()
}
