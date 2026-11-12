//go:build !windows

package main

import (
	"fmt"

	"go.hasen.dev/shirei/behavior_test/btmode"
)

func run(*btmode.Mode) { fmt.Println("SKIP: windows-activation requires Windows") }
