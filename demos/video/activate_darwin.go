//go:build darwin && !ios

package main

import "go.hasen.dev/shirei/cocoabackend"

func activate() { cocoabackend.Activate() }
