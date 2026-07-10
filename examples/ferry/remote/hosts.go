// Package remote is ferry's data layer: host discovery from an ssh config,
// connections (agent-first auth, known_hosts verification), sftp browsing,
// and the staged tar-pipe transfer engine.
package remote

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Host is one concrete alias from an ssh config. Discovery of alias
// *names* is ours (EnumerateHosts, which follows Include); resolving each
// name to its settings is `ssh -G`'s job, so Match/Include/defaults are
// OpenSSH's problem there.
type Host struct {
	Alias         string
	Hostname      string
	User          string
	Port          string
	IdentityFiles []string
}

func (h Host) Addr() string { return net.JoinHostPort(h.Hostname, h.Port) }

// EnumerateHosts parses configPath for concrete Host aliases (wildcard
// and negation patterns are skipped), following Include directives, and
// resolves each discovered alias via `ssh -G`.
func EnumerateHosts(configPath string) ([]Host, error) {
	var aliases []string
	if err := collectAliases(configPath, true, map[string]bool{}, map[string]bool{}, &aliases); err != nil {
		return nil, err
	}

	hosts := make([]Host, 0, len(aliases))
	for _, alias := range aliases {
		h, err := ResolveHost(configPath, alias)
		if err != nil {
			return nil, fmt.Errorf("resolving %s: %w", alias, err)
		}
		hosts = append(hosts, h)
	}
	return hosts, nil
}

// collectAliases scans one ssh config file for Host aliases (appended, in
// order, to aliases) and recurses into its Include directives. seenFile
// guards against include cycles and diamonds; seenAlias dedups names
// across files. required is true only for the top-level file: a missing
// top file is an error, a missing include is skipped (matching OpenSSH).
//
// Include tokens are treated UNCONDITIONALLY, even inside a Host/Match
// block: enumeration has no target hostname to test a condition against,
// and a wrongly-surfaced alias still resolves correctly through `ssh -G`.
func collectAliases(path string, required bool, seenFile, seenAlias map[string]bool, aliases *[]string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if seenFile[abs] {
		return nil
	}
	seenFile[abs] = true

	f, err := os.Open(path)
	if err != nil {
		if required {
			return err
		}
		return nil // missing include: skip, don't fail the whole enumeration
	}
	defer f.Close()

	dir := filepath.Dir(path)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		switch {
		case strings.EqualFold(fields[0], "Host"):
			for _, alias := range fields[1:] {
				if strings.ContainsAny(alias, "*?!") {
					continue
				}
				if !seenAlias[alias] {
					seenAlias[alias] = true
					*aliases = append(*aliases, alias)
				}
			}
		case strings.EqualFold(fields[0], "Include"):
			for _, token := range fields[1:] {
				for _, inc := range expandInclude(token, dir) {
					if err := collectAliases(inc, false, seenFile, seenAlias, aliases); err != nil {
						return err
					}
				}
			}
		}
	}
	return scanner.Err()
}

// expandInclude resolves one Include token to the files it names: ~
// expands to home, a relative path is taken against baseDir (the
// including file's directory — for ~/.ssh/config that is ~/.ssh, matching
// OpenSSH), and the result is glob-expanded (a glob matching nothing
// yields nothing). filepath.Glob returns matches in lexical order, so the
// listing is stable.
func expandInclude(token, baseDir string) []string {
	p := ExpandTilde(token)
	if !filepath.IsAbs(p) {
		p = filepath.Join(baseDir, p)
	}
	matches, err := filepath.Glob(p)
	if err != nil {
		return nil
	}
	return matches
}

// ResolveHost runs `ssh -G -F configPath alias` and extracts the fields
// ferry needs. -F also makes ssh ignore the system-wide config.
func ResolveHost(configPath, alias string) (Host, error) {
	h := Host{Alias: alias}
	out, err := exec.Command("ssh", "-G", "-F", configPath, alias).Output()
	if err != nil {
		return h, fmt.Errorf("ssh -G: %w", err)
	}
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		key, value, found := strings.Cut(scanner.Text(), " ")
		if !found {
			continue
		}
		switch key {
		case "hostname":
			h.Hostname = value
		case "user":
			h.User = value
		case "port":
			h.Port = value
		case "identityfile":
			h.IdentityFiles = append(h.IdentityFiles, ExpandTilde(value))
		}
	}
	if h.Hostname == "" {
		return h, fmt.Errorf("ssh -G returned no hostname for %s", alias)
	}
	return h, nil
}

// ExpandTilde expands a leading ~/ to the user's home directory (ssh -G
// prints identityfile paths unexpanded).
func ExpandTilde(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return p
		}
		return home + p[1:]
	}
	return p
}
