//go:build !android

package main

func ensureVideoPermission(onReady func()) {
	onReady()
}
