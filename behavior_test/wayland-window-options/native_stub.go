//go:build !linux || android

package main

import "go.hasen.dev/shirei"

func waylandAvailable() bool                                          { return false }
func closeMain(shirei.BackendContext)                                 {}
func positionMain(shirei.BackendContext, shirei.Vec2)                 {}
func checkRoles(shirei.BackendContext, []shirei.BackendContext) error { return nil }
