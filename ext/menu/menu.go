// Package menu exposes a small application-agnostic native application menu
// extension. Update is intended to be called from a Shirei frame; native
// callbacks only queue opaque IDs for the caller to dispatch on that frame.
package menu

import (
	"errors"
	"reflect"
	"sync"

	"go.hasen.dev/shirei"
)

type ID string

type ItemKind uint8

const (
	CommandItem ItemKind = iota
	SeparatorItem
	SubmenuItem
)

type Modifiers uint8

const (
	ModPrimary Modifiers = 1 << iota
	ModShift
	ModAlt
	ModControl
)

type Shortcut struct {
	Key       string
	Modifiers Modifiers
}

type Role uint8

const (
	RoleNone Role = iota
	RoleAbout
	RolePreferences
	RoleServices
	RoleHide
	RoleHideOthers
	RoleShowAll
	RoleQuit
)

type Item struct {
	Kind     ItemKind
	ID       ID
	Label    string
	Shortcut Shortcut
	Enabled  bool
	Checked  bool
	Role     Role
	Children []Item
}

type Menu struct {
	Label string
	Items []Item
}

type Model struct {
	Menus []Menu
}

var (
	ErrInvalidModel  = errors.New("invalid native menu model")
	ErrNotMainThread = errors.New("native menu update must run on the GUI thread")

	mu       sync.Mutex
	previous Model
	hasModel bool
	queued   []ID
)

// Supported reports whether this build has a native application-menu
// implementation. Unsupported platforms retain the caller's rendered menu.
func Supported() bool { return platformSupported() }

// Update installs or reconciles model and returns native activations queued
// since the previous call. On Darwin this function must be called on the
// Shirei GUI/main thread. An unchanged model is a cheap no-op.
func Update(model Model) ([]ID, error) {
	if err := validate(model); err != nil {
		return nil, err
	}
	mu.Lock()
	changed := !hasModel || !reflect.DeepEqual(previous, model)
	mu.Unlock()
	if changed && platformSupported() {
		if !platformOnMainThread() {
			return nil, ErrNotMainThread
		}
		if err := platformUpdate(model); err != nil {
			return nil, err
		}
	}
	if changed {
		mu.Lock()
		previous = cloneModel(model)
		hasModel = true
		mu.Unlock()
	}
	mu.Lock()
	ids := append([]ID(nil), queued...)
	queued = nil
	mu.Unlock()
	return ids, nil
}

func validate(model Model) error {
	seen := make(map[ID]struct{})
	var visit func([]Item) error
	visit = func(items []Item) error {
		for _, item := range items {
			switch item.Kind {
			case SeparatorItem:
				if item.ID != "" || item.Label != "" || len(item.Children) != 0 {
					return ErrInvalidModel
				}
			case CommandItem:
				if item.ID == "" || len(item.Children) != 0 {
					return ErrInvalidModel
				}
				if _, ok := seen[item.ID]; ok {
					return ErrInvalidModel
				}
				seen[item.ID] = struct{}{}
			case SubmenuItem:
				if item.ID != "" || item.Label == "" || len(item.Children) == 0 {
					return ErrInvalidModel
				}
				if err := visit(item.Children); err != nil {
					return err
				}
			default:
				return ErrInvalidModel
			}
		}
		return nil
	}
	for _, menu := range model.Menus {
		if menu.Label == "" {
			return ErrInvalidModel
		}
		if err := visit(menu.Items); err != nil {
			return err
		}
	}
	return nil
}

func cloneModel(model Model) Model {
	if model.Menus == nil {
		return Model{}
	}
	clone := Model{Menus: make([]Menu, len(model.Menus))}
	for i, menu := range model.Menus {
		clone.Menus[i] = Menu{Label: menu.Label, Items: cloneItems(menu.Items)}
	}
	return clone
}

func cloneItems(items []Item) []Item {
	if items == nil {
		return nil
	}
	clone := make([]Item, len(items))
	for i, item := range items {
		clone[i] = item
		clone[i].Children = cloneItems(item.Children)
	}
	return clone
}

func queueActivation(id ID) {
	mu.Lock()
	queued = append(queued, id)
	mu.Unlock()
	shirei.RequestNextFrame()
}
