package remote

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Conn is one live server connection: the ssh transport, plus an sftp
// client for browsing and exec sessions for transfers and commit scripts.
type Conn struct {
	Host Host
	ssh  *ssh.Client
	SFTP *sftp.Client
}

type DialOptions struct {
	KnownHostsPath string
	AcceptHostKey  HostKeyPrompt
	AskPassword    PasswordPrompt // nil = password auth unavailable
	Timeout        time.Duration  // default 5s
}

func Dial(host Host, opts DialOptions) (*Conn, error) {
	methods, skipped := AuthMethods(host.IdentityFiles)
	if opts.AskPassword != nil {
		// offered after publickey: the server only asks for it if the key
		// attempts fail (or "password" is all it accepts)
		methods = append(methods, passwordMethod(host.User, host.Addr(), opts.AskPassword))
	}
	if len(methods) == 0 {
		msg := "no ssh-agent and no usable identity files"
		if len(skipped) > 0 {
			msg += fmt.Sprintf(" (passphrase-protected, not yet supported: %s)", strings.Join(skipped, ", "))
		}
		return nil, fmt.Errorf("%s: %s", host.Alias, msg)
	}

	hostKeys, err := KnownHostsCallback(opts.KnownHostsPath, opts.AcceptHostKey)
	if err != nil {
		return nil, err
	}

	// x/crypto negotiates its own preferred host-key algorithm, which can
	// differ from the key type recorded in known_hosts (e.g. an entry
	// written by OpenSSH) and then reads as a mismatch. OpenSSH avoids
	// this by preferring recorded key types; we emulate that with a
	// capture-and-redial: if the only "mismatch" is an algorithm
	// difference, retry constrained to the recorded types. A genuine
	// same-type mismatch is never retried.
	var (
		mismatch  *knownhosts.KeyError
		presented string
	)
	capture := func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := hostKeys(hostname, remote, key)
		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) && len(keyErr.Want) > 0 {
			mismatch, presented = keyErr, key.Type()
		}
		return err
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	cfg := &ssh.ClientConfig{
		User:            host.User,
		Auth:            methods,
		HostKeyCallback: capture,
		Timeout:         timeout,
	}
	client, err := ssh.Dial("tcp", host.Addr(), cfg)
	if err != nil && mismatch != nil {
		if algos := recordedKeyAlgorithms(mismatch.Want, presented); len(algos) > 0 {
			cfg.HostKeyAlgorithms = algos
			client, err = ssh.Dial("tcp", host.Addr(), cfg)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("dial %s (%s): %w", host.Alias, host.Addr(), err)
	}

	sf, err := sftp.NewClient(client)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("%s: sftp subsystem: %w", host.Alias, err)
	}
	return &Conn{Host: host, ssh: client, SFTP: sf}, nil
}

func (c *Conn) Close() error {
	if c.SFTP != nil {
		c.SFTP.Close()
	}
	if c.ssh != nil {
		return c.ssh.Close()
	}
	return nil
}

// Wait blocks until the underlying ssh transport closes — the GUI's
// disconnect watcher parks here.
func (c *Conn) Wait() error {
	return c.ssh.Wait()
}

// Output runs one command and returns its combined output, trailing
// newline trimmed. For small helper commands.
func (c *Conn) Output(cmd string) (string, error) {
	sess, err := c.ssh.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	out, err := sess.CombinedOutput(cmd)
	return strings.TrimSuffix(string(out), "\n"), err
}

// RunScript pipes a locally generated script into `sh` on the remote.
// Commit scripts go through here: generating the script locally with
// strict quoting beats fighting argv length limits.
func (c *Conn) RunScript(script string) (string, error) {
	sess, err := c.ssh.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	sess.Stdin = strings.NewReader(script)
	out, err := sess.CombinedOutput("sh")
	return strings.TrimSuffix(string(out), "\n"), err
}

// recordedKeyAlgorithms maps the known_hosts entries' key types to
// host-key algorithms for a constrained redial. Returns nil when the
// presented type is already among the recorded ones — that is a genuine
// key mismatch, not a negotiation artifact.
func recordedKeyAlgorithms(want []knownhosts.KnownKey, presentedType string) []string {
	var algos []string
	seen := map[string]bool{}
	for _, k := range want {
		t := k.Key.Type()
		if t == presentedType {
			return nil
		}
		if seen[t] {
			continue
		}
		seen[t] = true
		if t == ssh.KeyAlgoRSA {
			algos = append(algos, ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA)
		} else {
			algos = append(algos, t)
		}
	}
	return algos
}

// shQuote makes s safe as a single shell word. Single quotes preserve
// every byte (including newlines); embedded quotes become '\”.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func randSuffix() string {
	var b [4]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
