//go:build (!darwin && !windows && !linux) || (darwin && (ios || x11darwin)) || windows || linux

package menu

func platformSupported() bool    { return false }
func platformOnMainThread() bool { return true }
func platformUpdate(Model) error { return nil }
