//go:build (!darwin || ios || x11darwin) && !windows && (!linux || android)

package main

import "go.hasen.dev/shirei"

const platformSupported = false

func preferTabs()                             {}
func closeNative(shirei.BackendContext)       {}
func nativeClosed(shirei.BackendContext) bool { return true }
func checkNative(*panel) (bool, error)        { return false, nil }

func markNative(shirei.BackendContext, string)      {}
func finishNativeComposition(shirei.BackendContext) {}

func resizeNative(shirei.BackendContext, int) {}

const nativeChecksDescription = "unsupported backend"

func mainSizeOK() bool { return true }
