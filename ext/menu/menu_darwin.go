//go:build darwin && !ios && !x11darwin && cgo

package menu

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#include "menu_darwin.h"
*/
import "C"

import (
	"unsafe"
)

func platformSupported() bool { return true }

func platformOnMainThread() bool { return C.shirei_ext_menu_is_main_thread() != 0 }

func platformUpdate(model Model) error {
	C.shirei_ext_menu_begin()
	appLabel := C.CString(model.ApplicationName)
	C.shirei_ext_menu_add_application_menu(appLabel)
	C.free(unsafe.Pointer(appLabel))
	for _, menu := range model.Menus {
		label := C.CString(menu.Label)
		parent := C.shirei_ext_menu_add_menu(label)
		C.free(unsafe.Pointer(label))
		addItems(parent, menu.Items)
	}
	C.shirei_ext_menu_commit()
	return nil
}

func addItems(parent unsafe.Pointer, items []Item) {
	for _, item := range items {
		switch item.Kind {
		case SeparatorItem:
			C.shirei_ext_menu_add_separator(parent)
		case CommandItem:
			addItem(parent, item)
		case SubmenuItem:
			label := C.CString(item.Label)
			submenu := C.shirei_ext_menu_add_submenu(parent, label)
			C.free(unsafe.Pointer(label))
			addItems(submenu, item.Children)
		}
	}
}

func addItem(parent unsafe.Pointer, item Item) {
	id, label, key := C.CString(string(item.ID)), C.CString(item.Label), C.CString(item.Shortcut.Key)
	defer C.free(unsafe.Pointer(id))
	defer C.free(unsafe.Pointer(label))
	defer C.free(unsafe.Pointer(key))
	C.shirei_ext_menu_add_item(parent, id, label, key, C.int(item.Shortcut.Modifiers), C.int(boolInt(item.Enabled)), C.int(boolInt(item.Checked)), C.int(item.Role))
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

//export shireiExtMenuOnAction
func shireiExtMenuOnAction(id *C.char) {
	queueActivation(ID(C.GoString(id)))
}
