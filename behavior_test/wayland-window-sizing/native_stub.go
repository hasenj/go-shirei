//go:build !linux || android

package main

import "go.hasen.dev/shirei"

func resizeOther(shirei.BackendContext, float32, float32) {}
func constrain(shirei.BackendContext, string)             {}
func configuredState(shirei.BackendContext) int           { return 0 }
func sizeConstrained(shirei.BackendContext) bool          { return false }
