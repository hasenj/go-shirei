//go:build js

package jsbackend

import "go.hasen.dev/shirei"

// Context is the BackendContext for the browser/wasm shell.
type Context struct{}

// Platform implements shirei.BackendContext.
func (Context) Platform() string { return "js" }

var _ shirei.BackendContext = Context{}
