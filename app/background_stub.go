//go:build (!darwin && !windows && !linux) || android || ios

package app

func setupBackgroundBackend() {}
