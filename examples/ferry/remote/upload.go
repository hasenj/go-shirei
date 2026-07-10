package remote

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

// Upload copies local files/directories into the remote directory destDir;
// each item lands as destDir/basename(item). The payload streams as a
// gzip'd tar into a stage directory inside destDir and is committed with
// renames — on any failure the stage is deleted and the destination is
// untouched.
func (c *Conn) Upload(ctx context.Context, items []string, destDir string, strat Strategy, progress ProgressFunc) error {
	plan, err := planLocalWalk(items)
	if err != nil {
		return err
	}

	conflicts, err := c.uploadConflicts(destDir, plan.TopNames)
	if err != nil {
		return err
	}
	if len(conflicts) > 0 && strat == StrategyFail {
		return &ConflictError{Names: conflicts}
	}

	stage := path.Join(destDir, stageName())
	if err := c.SFTP.Mkdir(stage); err != nil {
		return fmt.Errorf("creating stage in %s: %w", destDir, err)
	}
	fail := func(err error) error {
		c.RunScript("rm -rf " + shQuote(stage) + "\n")
		return err
	}

	if err := c.streamTarTo(ctx, plan, stage, progress); err != nil {
		return fail(err)
	}

	script := commitScript(plan, stage, destDir, strat, conflicts)
	if out, err := c.RunScript(script); err != nil {
		return fail(fmt.Errorf("commit: %w\n%s", err, out))
	}
	return nil
}

func (c *Conn) uploadConflicts(destDir string, names []string) ([]string, error) {
	info, err := c.SFTP.Stat(destDir)
	if err != nil {
		return nil, fmt.Errorf("destination %s: %w", destDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("destination %s is not a directory", destDir)
	}
	var conflicts []string
	for _, name := range names {
		_, err := c.SFTP.Lstat(path.Join(destDir, name))
		switch {
		case err == nil:
			conflicts = append(conflicts, name)
		case errors.Is(err, os.ErrNotExist):
		default:
			return nil, err
		}
	}
	return conflicts, nil
}

// streamTarTo runs `tar -xzf -` on the remote rooted at the stage and
// streams the plan into its stdin.
func (c *Conn) streamTarTo(ctx context.Context, plan *transferPlan, stage string, progress ProgressFunc) error {
	sess, err := c.ssh.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()

	stdin, err := sess.StdinPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	sess.Stderr = &stderr
	if err := sess.Start("tar -xzf - -C " + shQuote(stage)); err != nil {
		return err
	}

	werr := writeTar(ctx, stdin, plan, progress)
	stdin.Close()
	terr := sess.Wait()
	if werr != nil {
		return werr // the local/cancellation error wins; remote tar noise is a symptom
	}
	if terr != nil {
		return fmt.Errorf("remote tar: %w: %s", terr, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func writeTar(ctx context.Context, w io.Writer, plan *transferPlan, progress ProgressFunc) error {
	gz, err := gzip.NewWriterLevel(w, gzip.BestSpeed)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(gz)

	// directories first, so their modes exist before content lands in them
	for _, d := range plan.Dirs {
		hdr := &tar.Header{Typeflag: tar.TypeDir, Name: d.RelPath + "/", Mode: int64(d.Mode.Perm()), ModTime: d.ModTime}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
	}
	for _, l := range plan.Links {
		hdr := &tar.Header{Typeflag: tar.TypeSymlink, Name: l.RelPath, Linkname: l.Target, Mode: 0o777}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
	}
	var done int64
	buf := make([]byte, 128*1024)
	for _, f := range plan.Files {
		hdr := &tar.Header{Name: f.RelPath, Size: f.Size, Mode: int64(f.Mode.Perm()), ModTime: f.ModTime}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		src, err := os.Open(f.LocalPath)
		if err != nil {
			return err
		}
		cerr := copyWithProgress(ctx, tw, src, buf, &done, plan.Total, progress)
		src.Close()
		if cerr != nil {
			return cerr
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

// commitScript moves staged entries into place. Fresh names are single
// renames; conflicted names follow the strategy. Type conflicts (uploading
// a file over an existing directory or vice versa) surface as script errors
// under set -e, which aborts the commit and cleans the stage.
func commitScript(plan *transferPlan, stage, destDir string, strat Strategy, conflicts []string) string {
	conflicted := map[string]bool{}
	for _, n := range conflicts {
		conflicted[n] = true
	}

	var b strings.Builder
	b.WriteString("set -e\n")
	for _, name := range plan.TopNames {
		src := path.Join(stage, name)
		dst := path.Join(destDir, name)
		switch {
		case !conflicted[name]:
			fmt.Fprintf(&b, "mv %s %s\n", shQuote(src), shQuote(dst))
		case strat == StrategyReplace:
			old := path.Join(destDir, ".ferry-old-"+randSuffix())
			fmt.Fprintf(&b, "mv %s %s\n", shQuote(dst), shQuote(old))
			fmt.Fprintf(&b, "mv %s %s\n", shQuote(src), shQuote(dst))
			fmt.Fprintf(&b, "rm -rf %s\n", shQuote(old))
		case strat == StrategyMerge:
			for _, d := range plan.Dirs {
				if underTop(d.RelPath, name) {
					fmt.Fprintf(&b, "mkdir -p %s\n", shQuote(path.Join(destDir, d.RelPath)))
				}
			}
			for _, l := range plan.Links {
				if underTop(l.RelPath, name) {
					fmt.Fprintf(&b, "mv -f %s %s\n", shQuote(path.Join(stage, l.RelPath)), shQuote(path.Join(destDir, l.RelPath)))
				}
			}
			for _, f := range plan.Files {
				if underTop(f.RelPath, name) {
					fmt.Fprintf(&b, "mv -f %s %s\n", shQuote(path.Join(stage, f.RelPath)), shQuote(path.Join(destDir, f.RelPath)))
				}
			}
		}
	}
	fmt.Fprintf(&b, "rm -rf %s\n", shQuote(stage))
	return b.String()
}
