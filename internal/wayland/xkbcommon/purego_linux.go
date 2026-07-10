package xkbcommon

import (
	"fmt"
	"runtime"

	"github.com/ebitengine/purego"
)

func getSystemLibrary() string {
	switch runtime.GOOS {
	case "linux":
		return "libxkbcommon.so"
	default:
		panic(fmt.Errorf("GOOS=%s is not supported", runtime.GOOS))
	}
}

func getSystemLibraryDotVersion() string {
	switch runtime.GOOS {
	case "linux":
		return ".0"
	default:
		panic(fmt.Errorf("GOOS=%s is not supported", runtime.GOOS))
	}
}

var xkb_compose_state_feed func(uintptr, uint) uintptr
var xkb_compose_state_get_one_sym func(uintptr) uintptr
var xkb_compose_state_get_status func(uintptr) uintptr
var xkb_compose_state_get_utf8 func(uintptr, []byte, uint64) uint64
var xkb_compose_state_new func(uintptr, uint32) uintptr
var xkb_compose_state_unref func(uintptr)
var xkb_compose_table_new_from_locale func(uintptr, string, uint32) uintptr
var xkb_compose_table_unref func(uintptr)
var xkb_context_new func(uint32) uintptr
var xkb_context_unref func(uintptr)
var xkb_keymap_key_repeats func(uintptr, uint) uintptr
var xkb_keymap_mod_get_index func(uintptr, string) uintptr
var xkb_keymap_new_from_string func(uintptr, []byte, uint32, uint32) uintptr
var xkb_keymap_unref func(uintptr)
var xkb_keysym_to_utf32 func(uint) uint
var xkb_state_key_get_one_sym func(uintptr, uint) uintptr
var xkb_state_key_get_syms func(uintptr, uint, **uint) uintptr
var xkb_state_key_get_utf32 func(uintptr, uint) uint32
var xkb_state_new func(uintptr) uintptr
var xkb_state_serialize_mods func(uintptr, uint32) uint32
var xkb_state_unref func(uintptr)
var xkb_state_update_mask func(uintptr, uint, uint, uint, uint, uint, uint) uint32

func init() {
	libxkbcommon, err := purego.Dlopen(getSystemLibrary(), purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		libxkbcommon, _ = purego.Dlopen(getSystemLibrary()+getSystemLibraryDotVersion(), purego.RTLD_NOW|purego.RTLD_GLOBAL)
	}
	if libxkbcommon == 0 {
		return
	}
	purego.RegisterLibFunc(&xkb_compose_state_feed, libxkbcommon, "xkb_compose_state_feed")
	purego.RegisterLibFunc(&xkb_compose_state_get_one_sym, libxkbcommon, "xkb_compose_state_get_one_sym")
	purego.RegisterLibFunc(&xkb_compose_state_get_status, libxkbcommon, "xkb_compose_state_get_status")
	purego.RegisterLibFunc(&xkb_compose_state_get_utf8, libxkbcommon, "xkb_compose_state_get_utf8")
	purego.RegisterLibFunc(&xkb_compose_state_new, libxkbcommon, "xkb_compose_state_new")
	purego.RegisterLibFunc(&xkb_compose_state_unref, libxkbcommon, "xkb_compose_state_unref")
	purego.RegisterLibFunc(&xkb_compose_table_new_from_locale, libxkbcommon, "xkb_compose_table_new_from_locale")
	purego.RegisterLibFunc(&xkb_compose_table_unref, libxkbcommon, "xkb_compose_table_unref")
	purego.RegisterLibFunc(&xkb_context_new, libxkbcommon, "xkb_context_new")
	purego.RegisterLibFunc(&xkb_context_unref, libxkbcommon, "xkb_context_unref")
	purego.RegisterLibFunc(&xkb_keymap_key_repeats, libxkbcommon, "xkb_keymap_key_repeats")
	purego.RegisterLibFunc(&xkb_keymap_mod_get_index, libxkbcommon, "xkb_keymap_mod_get_index")
	purego.RegisterLibFunc(&xkb_keymap_new_from_string, libxkbcommon, "xkb_keymap_new_from_string")
	purego.RegisterLibFunc(&xkb_keymap_unref, libxkbcommon, "xkb_keymap_unref")
	purego.RegisterLibFunc(&xkb_keysym_to_utf32, libxkbcommon, "xkb_keysym_to_utf32")
	purego.RegisterLibFunc(&xkb_state_key_get_one_sym, libxkbcommon, "xkb_state_key_get_one_sym")
	purego.RegisterLibFunc(&xkb_state_key_get_syms, libxkbcommon, "xkb_state_key_get_syms")
	purego.RegisterLibFunc(&xkb_state_key_get_utf32, libxkbcommon, "xkb_state_key_get_utf32")
	purego.RegisterLibFunc(&xkb_state_new, libxkbcommon, "xkb_state_new")
	purego.RegisterLibFunc(&xkb_state_serialize_mods, libxkbcommon, "xkb_state_serialize_mods")
	purego.RegisterLibFunc(&xkb_state_unref, libxkbcommon, "xkb_state_unref")
	purego.RegisterLibFunc(&xkb_state_update_mask, libxkbcommon, "xkb_state_update_mask")

}
