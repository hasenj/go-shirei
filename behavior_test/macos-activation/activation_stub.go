//go:build !darwin || ios || x11darwin

package main

import (
	"fmt"
	"go.hasen.dev/shirei/behavior_test/btmode"
)

func run(*btmode.Mode) { fmt.Println("SKIP: macos-activation requires AppKit") }
