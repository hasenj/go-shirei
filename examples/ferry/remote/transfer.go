package remote

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Strategy says what to do when a top-level destination name already exists.
type Strategy int

const (
	// StrategyFail aborts before any data moves.
	StrategyFail Strategy = iota
	// StrategyMerge adds the payload into existing trees: files are replaced
	// atomically one by one, directory contents mix.
	StrategyMerge
	// StrategyReplace swaps conflicting entries wholesale: old moved aside,
	// new renamed in, old deleted.
	StrategyReplace
)

// ProgressFunc receives cumulative uncompressed payload bytes against the
// precomputed total. Called synchronously from the transfer; keep it cheap.
type ProgressFunc func(done, total int64)

// ConflictError reports which top-level names already exist at the
// destination under StrategyFail.
type ConflictError struct{ Names []string }

func (e *ConflictError) Error() string {
	return "destination already has: " + strings.Join(e.Names, ", ")
}

func stageName() string { return ".ferry-stage-" + randSuffix() }

type planFile struct {
	LocalPath string // empty on downloads (content comes from the stream)
	RelPath   string // slash-separated, rooted at a top-level name
	Size      int64
	Mode      fs.FileMode
	ModTime   time.Time
}

type planDir struct {
	RelPath string
	Mode    fs.FileMode
	ModTime time.Time
}

type planLink struct {
	RelPath string
	Target  string
}

// transferPlan is the local walk of an upload: what goes in the tar, what
// the commit script needs to know, and the progress total.
type transferPlan struct {
	TopNames []string
	Dirs     []planDir
	Links    []planLink
	Files    []planFile
	Total    int64
}

func planLocalWalk(items []string) (*transferPlan, error) {
	plan := &transferPlan{}
	seen := map[string]bool{}
	for _, item := range items {
		abs, err := filepath.Abs(item)
		if err != nil {
			return nil, err
		}
		info, err := os.Lstat(abs)
		if err != nil {
			return nil, err
		}
		base := filepath.Base(abs)
		if base == "/" || base == "." {
			return nil, fmt.Errorf("cannot upload %q; name a file or directory", item)
		}
		if seen[base] {
			return nil, fmt.Errorf("two items would land as %q at the destination", base)
		}
		seen[base] = true
		plan.TopNames = append(plan.TopNames, base)

		switch {
		case info.Mode().IsRegular():
			plan.Files = append(plan.Files, planFile{abs, base, info.Size(), info.Mode(), info.ModTime()})
			plan.Total += info.Size()
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(abs)
			if err != nil {
				return nil, err
			}
			plan.Links = append(plan.Links, planLink{base, target})
		case info.IsDir():
			err := filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				rel := base
				if p != abs {
					sub, err := filepath.Rel(abs, p)
					if err != nil {
						return err
					}
					rel = base + "/" + filepath.ToSlash(sub)
				}
				fi, err := d.Info()
				if err != nil {
					return err
				}
				switch {
				case d.IsDir():
					plan.Dirs = append(plan.Dirs, planDir{rel, fi.Mode(), fi.ModTime()})
				case fi.Mode().IsRegular():
					plan.Files = append(plan.Files, planFile{p, rel, fi.Size(), fi.Mode(), fi.ModTime()})
					plan.Total += fi.Size()
				case fi.Mode()&fs.ModeSymlink != 0:
					target, err := os.Readlink(p)
					if err != nil {
						return err
					}
					plan.Links = append(plan.Links, planLink{rel, target})
				}
				// sockets, fifos, devices: silently skipped
				return nil
			})
			if err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("%s: unsupported file type", item)
		}
	}
	return plan, nil
}

// copyWithProgress copies src into dst in chunks, bumping *done and
// checking for cancellation between chunks.
func copyWithProgress(ctx context.Context, dst io.Writer, src io.Reader, buf []byte, done *int64, total int64, progress ProgressFunc) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, rerr := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return werr
			}
			*done += int64(n)
			if progress != nil {
				progress(*done, total)
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}

func underTop(rel, top string) bool {
	return rel == top || strings.HasPrefix(rel, top+"/")
}
