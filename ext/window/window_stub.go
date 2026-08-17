//go:build (!darwin && !windows && !linux) || (darwin && ios)

package window

// Platform implementations default to no-ops on non-desktop platforms.
