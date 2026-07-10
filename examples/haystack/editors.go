package main

import (
	"fmt"
	"os/exec"
)

// Editor is one text editor we can hand a file+line to. We detect which are
// installed once at startup (via their command-line launcher on PATH) so the
// result rows only show buttons that will actually do something.
type Editor struct {
	Name string
	bin  string
	args func(path string, line int) []string
}

// Open launches the editor at the given file and line, fire-and-forget. We do
// not wait — the GUI must never block on a subprocess.
func (e Editor) Open(path string, line int) {
	cmd := exec.Command(e.bin, e.args(path, line)...)
	// Detach: we don't read output and we don't care about exit status.
	_ = cmd.Start()
}

// detectEditors returns the subset of known editors whose launcher is on PATH.
// Each launcher accepts a "file:line" spelling (VS Code needs -g/--goto for it).
func detectEditors() []Editor {
	candidates := []Editor{
		{
			Name: "VS Code", bin: "code",
			args: func(p string, l int) []string { return []string{"-g", fmt.Sprintf("%s:%d", p, l)} },
		},
		{
			Name: "Sublime", bin: "subl",
			args: func(p string, l int) []string { return []string{fmt.Sprintf("%s:%d", p, l)} },
		},
		{
			Name: "Zed", bin: "zed",
			args: func(p string, l int) []string { return []string{fmt.Sprintf("%s:%d", p, l)} },
		},
	}

	var found []Editor
	for _, e := range candidates {
		if _, err := exec.LookPath(e.bin); err == nil {
			found = append(found, e)
		}
	}
	return found
}
