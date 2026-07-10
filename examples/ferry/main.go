// ferry — a two-pane SFTP file manager (GUI + CLI). See README.md.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/term"

	"go.hasen.dev/shirei/examples/ferry/remote"
)

// entrySSHConfig is the default entry-point ssh config — the real user
// config. ferry now reads production by design (the roadmap's final
// step); a leading ~ is expanded. Override per-run with -F (e.g.
// `-F ./ferry/dev_ssh_config` to browse the lima test boxes instead).
// Includes are followed by both ferry's enumeration and `ssh -G`.
const entrySSHConfig = "~/.ssh/config"

// configPathOverride is set from -F when launching the GUI; the CLI
// subcommands take -F through commonFlags instead.
var configPathOverride string

// configuredPath is the entry config as configured (unexpanded, for
// display); defaultConfigPath is the same expanded for actual use.
func configuredPath() string {
	if configPathOverride != "" {
		return configPathOverride
	}
	return entrySSHConfig
}

const usageText = `ferry — a two-pane sftp gui

usage:
  ferry [-F <config>]                  open the gui (default config: ~/.ssh/config)
  ferry --png <out.png>                render one frame headlessly and exit
  ferry hosts                          list hosts from the ssh config
  ferry ls <host>[:path]               list a remote directory (default: home)
  ferry head <host>:<path>             print the first 4KB of a remote file
  ferry put <local>... <host>:<dir>    upload into a remote directory
  ferry get <host>:<path>... <dir>     download into a local directory

put/get flags:
  -on-conflict fail|merge|replace   when destination names exist (default fail)

common flags:
  -F <path>             ssh config (default: ~/.ssh/config; -F ./ferry/dev_ssh_config for the lima test boxes)
  -known-hosts <path>   known hosts file (default: known_hosts next to the config)
  -accept-new           trust first-contact host keys without prompting
`

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "--png" {
		if err := RenderPNG(os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, "render failed:", err)
			os.Exit(1)
		}
		return
	}

	// A known subcommand runs the CLI; anything else — no args, or leading
	// flags like `ferry -F <config>` — opens the GUI.
	if len(os.Args) >= 2 && !strings.HasPrefix(os.Args[1], "-") {
		var err error
		switch os.Args[1] {
		case "hosts":
			err = cmdHosts(os.Args[2:])
		case "ls":
			err = cmdLs(os.Args[2:])
		case "head":
			err = cmdHead(os.Args[2:])
		case "put":
			err = cmdPut(os.Args[2:])
		case "get":
			err = cmdGet(os.Args[2:])
		case "help":
			fmt.Print(usageText)
		default:
			fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usageText)
			os.Exit(2)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	fs := flag.NewFlagSet("ferry", flag.ExitOnError)
	fs.Usage = func() { fmt.Print(usageText) }
	cfg := fs.String("F", "", "ssh config path (default: ~/.ssh/config)")
	fs.Parse(os.Args[1:]) // handles -h/--help via fs.Usage
	configPathOverride = *cfg
	RunGUI()
}

type commonFlags struct {
	config     string
	knownHosts string
	acceptNew  bool
}

func registerCommon(fs *flag.FlagSet) *commonFlags {
	cf := &commonFlags{}
	fs.StringVar(&cf.config, "F", "", "ssh config path")
	fs.StringVar(&cf.knownHosts, "known-hosts", "", "known_hosts path")
	fs.BoolVar(&cf.acceptNew, "accept-new", false, "trust first-contact host keys")
	return cf
}

func (cf *commonFlags) resolve() {
	if cf.config == "" {
		cf.config = defaultConfigPath()
	}
	cf.config = remote.ExpandTilde(cf.config)
	if cf.knownHosts == "" {
		cf.knownHosts = defaultKnownHostsPath(cf.config)
	}
}

// defaultConfigPath is the entry-point ssh config in use (the const, or a
// -F override), with a leading ~ expanded for actual filesystem use.
func defaultConfigPath() string {
	return remote.ExpandTilde(configuredPath())
}

// defaultKnownHostsPath is the known_hosts beside the config: for the
// real ~/.ssh/config that resolves to ~/.ssh/known_hosts (so ferry shares
// the trust store ssh already uses); for -F ./ferry/dev_ssh_config it is
// ferry/known_hosts (gitignored). config must already be tilde-expanded.
func defaultKnownHostsPath(config string) string {
	return filepath.Join(filepath.Dir(config), "known_hosts")
}

func connect(cf *commonFlags, alias string) (*remote.Conn, error) {
	host, err := remote.ResolveHost(cf.config, alias)
	if err != nil {
		return nil, err
	}
	accept := func(addr string, key ssh.PublicKey) bool {
		fingerprint := ssh.FingerprintSHA256(key)
		if cf.acceptNew {
			fmt.Fprintf(os.Stderr, "first contact with %s, trusting key %s\n", addr, fingerprint)
			return true
		}
		fmt.Fprintf(os.Stderr, "first contact with %s\nhost key: %s\ntrust it? [y/N] ", addr, fingerprint)
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		answer := strings.ToLower(strings.TrimSpace(line))
		return answer == "y" || answer == "yes"
	}
	return remote.Dial(host, remote.DialOptions{
		KnownHostsPath: cf.knownHosts,
		AcceptHostKey:  accept,
		AskPassword:    cliPasswordPrompt,
	})
}

// cliPasswordPrompt reads a password from the terminal with echo off. A
// non-tty stdin (piped, CI) can't answer, which reads as a cancel — the
// dial then fails with the auth error instead of hanging.
func cliPasswordPrompt(user, addr string, attempt int) (string, bool) {
	if attempt > 1 {
		fmt.Fprintln(os.Stderr, "wrong password, try again")
	}
	fmt.Fprintf(os.Stderr, "%s@%s's password: ", user, addr)
	pw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", false
	}
	return string(pw), true
}

// splitHostPath splits "alias:path"; path may be empty ("alias" alone).
func splitHostPath(s string) (alias, p string) {
	alias, p, _ = strings.Cut(s, ":")
	return
}

func parseStrategy(s string) (remote.Strategy, error) {
	switch s {
	case "fail":
		return remote.StrategyFail, nil
	case "merge":
		return remote.StrategyMerge, nil
	case "replace":
		return remote.StrategyReplace, nil
	}
	return 0, fmt.Errorf("invalid -on-conflict %q (want fail, merge, or replace)", s)
}

func cmdHosts(args []string) error {
	fs := flag.NewFlagSet("hosts", flag.ExitOnError)
	cf := registerCommon(fs)
	fs.Parse(args)
	cf.resolve()

	hosts, err := remote.EnumerateHosts(cf.config)
	if err != nil {
		return err
	}
	fmt.Printf("hosts from %s:\n", cf.config)
	for _, h := range hosts {
		fmt.Printf("  %-12s %s@%s\n", h.Alias, h.User, h.Addr())
	}
	return nil
}

func cmdLs(args []string) error {
	fs := flag.NewFlagSet("ls", flag.ExitOnError)
	cf := registerCommon(fs)
	fs.Parse(args)
	cf.resolve()
	if fs.NArg() != 1 {
		return errors.New("usage: ferry ls <host>[:path]")
	}
	alias, dir := splitHostPath(fs.Arg(0))

	conn, err := connect(cf, alias)
	if err != nil {
		return err
	}
	defer conn.Close()

	if dir == "" {
		if dir, err = conn.HomeDir(); err != nil {
			return err
		}
	}
	entries, err := conn.List(dir)
	if err != nil {
		return err
	}
	fmt.Printf("%s:%s\n", alias, dir)
	for _, e := range entries {
		name := e.Name
		if e.IsDir() {
			name += "/"
		}
		fmt.Printf("  %s %10s  %s\n", e.Mode, fmtBytes(e.Size), name)
	}
	return nil
}

func cmdHead(args []string) error {
	fs := flag.NewFlagSet("head", flag.ExitOnError)
	cf := registerCommon(fs)
	fs.Parse(args)
	cf.resolve()
	if fs.NArg() != 1 {
		return errors.New("usage: ferry head <host>:<path>")
	}
	alias, p := splitHostPath(fs.Arg(0))
	if p == "" {
		return errors.New("head needs host:path")
	}

	conn, err := connect(cf, alias)
	if err != nil {
		return err
	}
	defer conn.Close()

	data, err := conn.ReadHead(p, 4096)
	if err != nil {
		return err
	}
	os.Stdout.Write(data)
	return nil
}

func cmdPut(args []string) error {
	fs := flag.NewFlagSet("put", flag.ExitOnError)
	cf := registerCommon(fs)
	onConflict := fs.String("on-conflict", "fail", "fail|merge|replace")
	fs.Parse(args)
	cf.resolve()
	if fs.NArg() < 2 {
		return errors.New("usage: ferry put <local>... <host>:<dir>")
	}
	rest := fs.Args()
	items, dest := rest[:len(rest)-1], rest[len(rest)-1]
	alias, dir := splitHostPath(dest)
	if dir == "" {
		return errors.New("put destination must be host:dir")
	}
	strat, err := parseStrategy(*onConflict)
	if err != nil {
		return err
	}

	conn, err := connect(cf, alias)
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	err = conn.Upload(ctx, items, dir, strat, progressPrinter())
	finishProgress(err)
	return err
}

func cmdGet(args []string) error {
	fs := flag.NewFlagSet("get", flag.ExitOnError)
	cf := registerCommon(fs)
	onConflict := fs.String("on-conflict", "fail", "fail|merge|replace")
	fs.Parse(args)
	cf.resolve()
	if fs.NArg() < 2 {
		return errors.New("usage: ferry get <host>:<path>... <localdir>")
	}
	rest := fs.Args()
	sources, destDir := rest[:len(rest)-1], rest[len(rest)-1]
	strat, err := parseStrategy(*onConflict)
	if err != nil {
		return err
	}

	var conn *remote.Conn
	defer func() {
		if conn != nil {
			conn.Close()
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	for _, src := range sources {
		alias, p := splitHostPath(src)
		if p == "" {
			return fmt.Errorf("get source must be host:path, got %q", src)
		}
		if conn == nil {
			if conn, err = connect(cf, alias); err != nil {
				return err
			}
		} else if alias != conn.Host.Alias {
			return errors.New("all get sources must use the same host")
		}
		err = conn.Download(ctx, path.Dir(p), []string{path.Base(p)}, destDir, strat, progressPrinter())
		finishProgress(err)
		if err != nil {
			return err
		}
	}
	return nil
}

func progressPrinter() remote.ProgressFunc {
	start := time.Now()
	var last time.Time
	return func(done, total int64) {
		if done != total && time.Since(last) < 50*time.Millisecond {
			return
		}
		last = time.Now()
		elapsed := time.Since(start).Seconds()
		rate := ""
		if elapsed > 0.2 {
			rate = "  " + fmtBytes(int64(float64(done)/elapsed)) + "/s"
		}
		if total > 0 {
			fmt.Fprintf(os.Stderr, "\r%6.1f%%  %s / %s%s   ",
				float64(done)/float64(total)*100, fmtBytes(done), fmtBytes(total), rate)
		} else {
			fmt.Fprintf(os.Stderr, "\r%s%s   ", fmtBytes(done), rate)
		}
	}
}

func finishProgress(err error) {
	if err == nil {
		fmt.Fprint(os.Stderr, "\rdone                                        \n")
	} else {
		fmt.Fprintln(os.Stderr)
	}
}

func fmtBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1fGB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%dB", n)
}
