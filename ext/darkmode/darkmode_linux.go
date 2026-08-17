//go:build linux && !android

package darkmode

import (
	"os"
	"strings"

	"github.com/godbus/dbus/v5"
)

func initPlatform() {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		checkFallbackEnv()
		return
	}

	// Query current setting from XDG Settings Portal
	obj := conn.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop")
	var val dbus.Variant
	err = obj.Call("org.freedesktop.portal.Settings.Read", 0, "org.freedesktop.appearance", "color-scheme").Store(&val)
	if err == nil {
		setDarkMode(parseColorScheme(val))
	} else {
		checkFallbackEnv()
	}

	// Subscribe to portal change notifications
	if err := conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.portal.Settings"),
		dbus.WithMatchMember("SettingChanged"),
	); err == nil {
		ch := make(chan *dbus.Signal, 10)
		conn.Signal(ch)
		go func() {
			for sig := range ch {
				if sig == nil {
					return
				}
				if sig.Path == "/org/freedesktop/portal/desktop" &&
					sig.Name == "org.freedesktop.portal.Settings.SettingChanged" &&
					len(sig.Body) >= 3 {
					ns, _ := sig.Body[0].(string)
					key, _ := sig.Body[1].(string)
					if ns == "org.freedesktop.appearance" && key == "color-scheme" {
						setDarkMode(parseColorScheme(sig.Body[2]))
					}
				}
			}
		}()
	}
}

func parseColorScheme(v any) bool {
	for {
		if variant, ok := v.(dbus.Variant); ok {
			v = variant.Value()
			continue
		}
		break
	}
	switch n := v.(type) {
	case uint32:
		return n == 1
	case int32:
		return n == 1
	case uint64:
		return n == 1
	case int64:
		return n == 1
	case int:
		return n == 1
	case uint8:
		return n == 1
	case string:
		s := strings.ToLower(n)
		return strings.Contains(s, "dark")
	}
	return false
}

func checkFallbackEnv() {
	gtkTheme := os.Getenv("GTK_THEME")
	if gtkTheme != "" && strings.Contains(strings.ToLower(gtkTheme), "dark") {
		setDarkMode(true)
	}
}
