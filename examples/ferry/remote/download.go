package remote

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Download copies remote entries (srcDir/name for each name) into the
// local directory destDir. Mirror of Upload: remote `tar -czf -` streams
// into a stage directory inside destDir, committed with renames.
func (c *Conn) Download(ctx context.Context, srcDir string, names []string, destDir string, strat Strategy, progress ProgressFunc) error {
	for _, n := range names {
		if n == "" || strings.ContainsRune(n, '/') {
			return fmt.Errorf("download names must be plain directory entries, got %q", n)
		}
	}

	total, err := c.remoteTotal(srcDir, names)
	if err != nil {
		return err
	}

	conflicts, err := localConflicts(destDir, names)
	if err != nil {
		return err
	}
	if len(conflicts) > 0 && strat == StrategyFail {
		return &ConflictError{Names: conflicts}
	}

	stage, err := os.MkdirTemp(destDir, ".ferry-stage-")
	if err != nil {
		return err
	}
	fail := func(err error) error {
		os.RemoveAll(stage)
		return err
	}

	if err := c.streamTarFrom(ctx, srcDir, names, stage, total, progress); err != nil {
		return fail(err)
	}

	if err := commitLocal(stage, destDir, names, strat, conflicts); err != nil {
		return fail(fmt.Errorf("commit: %w", err))
	}
	return os.RemoveAll(stage)
}

// remoteTotal walks the payload for the progress denominator (regular file
// bytes only — what actually streams).
func (c *Conn) remoteTotal(srcDir string, names []string) (int64, error) {
	var total int64
	for _, name := range names {
		root := path.Join(srcDir, name)
		if _, err := c.SFTP.Lstat(root); err != nil {
			return 0, fmt.Errorf("%s: %w", root, err)
		}
		walker := c.SFTP.Walk(root)
		for walker.Step() {
			if err := walker.Err(); err != nil {
				return 0, fmt.Errorf("walking %s: %w", walker.Path(), err)
			}
			if info := walker.Stat(); info != nil && info.Mode().IsRegular() {
				total += info.Size()
			}
		}
	}
	return total, nil
}

func localConflicts(destDir string, names []string) ([]string, error) {
	info, err := os.Stat(destDir)
	if err != nil {
		return nil, fmt.Errorf("destination %s: %w", destDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("destination %s is not a directory", destDir)
	}
	var conflicts []string
	for _, name := range names {
		_, err := os.Lstat(filepath.Join(destDir, name))
		switch {
		case err == nil:
			conflicts = append(conflicts, name)
		case errors.Is(err, fs.ErrNotExist):
		default:
			return nil, err
		}
	}
	return conflicts, nil
}

func (c *Conn) streamTarFrom(ctx context.Context, srcDir string, names []string, stage string, total int64, progress ProgressFunc) error {
	sess, err := c.ssh.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()

	stdout, err := sess.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	sess.Stderr = &stderr

	// COPYFILE_DISABLE stops macOS bsdtar from injecting AppleDouble ._*
	// entries (xattrs) into the archive; other systems ignore it.
	cmd := "COPYFILE_DISABLE=1 tar -czf - -C " + shQuote(srcDir)
	for _, name := range names {
		cmd += " " + shQuote(name)
	}
	if err := sess.Start(cmd); err != nil {
		return err
	}

	if err := extractTar(ctx, stdout, stage, total, progress); err != nil {
		// stop reading and tear the session down so the remote tar dies too
		sess.Close()
		return err
	}
	if err := sess.Wait(); err != nil {
		return fmt.Errorf("remote tar: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func extractTar(ctx context.Context, r io.Reader, stage string, total int64, progress ProgressFunc) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	var done int64
	buf := make([]byte, 128*1024)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(path.Clean(hdr.Name), "./")
		if rel == "." || rel == "" {
			continue
		}
		if !filepath.IsLocal(rel) {
			return fmt.Errorf("archive entry escapes the stage: %q", hdr.Name)
		}
		dst := filepath.Join(stage, filepath.FromSlash(rel))
		switch hdr.Typeflag {
		case tar.TypeDir:
			// |0700 so the stage stays writable even for r-x source dirs
			if err := os.MkdirAll(dst, hdr.FileInfo().Mode().Perm()|0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, hdr.FileInfo().Mode().Perm())
			if err != nil {
				return err
			}
			cerr := copyWithProgress(ctx, f, tr, buf, &done, total, progress)
			if closeErr := f.Close(); cerr == nil {
				cerr = closeErr
			}
			if cerr != nil {
				return cerr
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(hdr.Linkname, dst); err != nil {
				return err
			}
		}
		// other types (hardlinks, devices, fifos) are skipped
	}
}

// commitLocal is the local mirror of commitScript: fresh names rename
// once; replace swaps via a hidden aside; merge renames entry by entry
// (os.Rename replaces files atomically on POSIX).
func commitLocal(stage, destDir string, names []string, strat Strategy, conflicts []string) error {
	conflicted := map[string]bool{}
	for _, n := range conflicts {
		conflicted[n] = true
	}
	for _, name := range names {
		src := filepath.Join(stage, name)
		dst := filepath.Join(destDir, name)
		if _, err := os.Lstat(src); err != nil {
			return fmt.Errorf("staged entry missing: %w", err)
		}
		switch {
		case !conflicted[name]:
			if err := os.Rename(src, dst); err != nil {
				return err
			}
		case strat == StrategyReplace:
			old := filepath.Join(destDir, ".ferry-old-"+randSuffix())
			if err := os.Rename(dst, old); err != nil {
				return err
			}
			if err := os.Rename(src, dst); err != nil {
				os.Rename(old, dst) // best-effort rollback
				return err
			}
			os.RemoveAll(old)
		case strat == StrategyMerge:
			err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				rel, err := filepath.Rel(stage, p)
				if err != nil {
					return err
				}
				target := filepath.Join(destDir, rel)
				if d.IsDir() {
					info, err := d.Info()
					if err != nil {
						return err
					}
					return os.MkdirAll(target, info.Mode().Perm())
				}
				return os.Rename(p, target)
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}
