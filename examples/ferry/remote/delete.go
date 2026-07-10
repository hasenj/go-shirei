package remote

import (
	"fmt"
	"path"
	"strings"
)

// Delete removes paths on the remote, recursively for directories. This
// is the engine's one destructive operation, so it refuses anything
// ambiguous outright: every path must be absolute, cleaned, and below
// the root — a bug upstream must not be able to smuggle in "/", "", or
// a relative path resolved against who-knows-what cwd. rm -rf is
// idempotent: a path that is already gone counts as deleted.
func (c *Conn) Delete(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString("rm -rf --")
	for _, p := range paths {
		clean := path.Clean(p)
		if !strings.HasPrefix(p, "/") || clean == "/" {
			return fmt.Errorf("refusing to delete %q: not an absolute path below /", p)
		}
		// depth guard: a file manager deletes things INSIDE directories;
		// removing /var or /home wholesale is an admin op and far more
		// likely a path-handling bug than an intent
		if strings.Count(clean, "/") < 2 {
			return fmt.Errorf("refusing to delete top-level directory %q", clean)
		}
		b.WriteByte(' ')
		b.WriteString(shQuote(clean))
	}
	out, err := c.RunScript(b.String())
	if err != nil {
		if out != "" {
			return fmt.Errorf("delete: %s: %w", out, err)
		}
		return fmt.Errorf("delete: %w", err)
	}
	return nil
}
