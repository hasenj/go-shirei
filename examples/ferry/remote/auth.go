package remote

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// AuthMethods builds the publickey auth for a host: identity files named
// by the config first (the config explicitly says which key this host
// wants), then whatever the ssh-agent holds. Encrypted keys are skipped
// for now (no passphrase prompt yet); their paths are reported in skipped
// so callers can explain a failed auth.
//
// Everything goes through ONE AuthMethod: x/crypto tries each auth method
// *name* once, so a separate agent method would mask the file keys — an
// empty agent would dead-end the whole publickey attempt.
func AuthMethods(identityFiles []string) (methods []ssh.AuthMethod, skipped []string) {
	var fileSigners []ssh.Signer
	for _, path := range identityFiles {
		pem, err := os.ReadFile(ExpandTilde(path))
		if err != nil {
			continue // configs list default identity paths that often don't exist
		}
		signer, err := ssh.ParsePrivateKey(pem)
		if err != nil {
			var missing *ssh.PassphraseMissingError
			if errors.As(err, &missing) {
				skipped = append(skipped, path)
			}
			continue
		}
		fileSigners = append(fileSigners, signer)
	}

	var ag agent.ExtendedAgent
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if conn, err := net.Dial("unix", sock); err == nil {
			// stays open for the process lifetime; signing happens lazily
			ag = agent.NewClient(conn)
		}
	}

	if len(fileSigners) == 0 && ag == nil {
		return nil, skipped
	}
	callback := func() ([]ssh.Signer, error) {
		signers := append([]ssh.Signer{}, fileSigners...)
		if ag != nil {
			if agentSigners, err := ag.Signers(); err == nil {
				signers = append(signers, agentSigners...)
			}
		}
		return signers, nil
	}
	return []ssh.AuthMethod{ssh.PublicKeysCallback(callback)}, skipped
}

// HostKeyPrompt decides whether to trust a previously unseen host key.
// Returning true accepts it and records it in the known_hosts file.
type HostKeyPrompt func(addr string, key ssh.PublicKey) bool

// PasswordPrompt asks the user for a password. attempt starts at 1 and
// increments on each retry after a rejection, so a UI can show "wrong
// password" from attempt 2 on. Returning ok=false cancels the dial.
// Runs on the connect goroutine and may block on the user — same
// contract as HostKeyPrompt.
type PasswordPrompt func(user, addr string, attempt int) (password string, ok bool)

// passwordMethod wraps prompt as an ssh auth method: tried only if the
// server offers "password" (after publickey fails or is unavailable),
// re-prompting up to passwordAttempts times.
func passwordMethod(user, addr string, prompt PasswordPrompt) ssh.AuthMethod {
	attempt := 0
	cb := ssh.PasswordCallback(func() (string, error) {
		attempt++
		pw, ok := prompt(user, addr, attempt)
		if !ok {
			return "", fmt.Errorf("password entry cancelled")
		}
		return pw, nil
	})
	return ssh.RetryableAuthMethod(cb, passwordAttempts)
}

const passwordAttempts = 3

// KnownHostsCallback verifies host keys against path (created if missing).
// First contact goes through accept; a key that disagrees with a recorded
// one is always fatal.
func KnownHostsCallback(path string, accept HostKeyPrompt) (ssh.HostKeyCallback, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	f.Close()

	check, err := knownhosts.New(path)
	if err != nil {
		return nil, err
	}

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := check(hostname, remote, key)
		if err == nil {
			return nil
		}
		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) && len(keyErr.Want) == 0 {
			// first contact: not recorded under any name
			if accept != nil && accept(hostname, key) {
				return appendKnownHost(path, hostname, key)
			}
			return fmt.Errorf("host %s not trusted (key %s)", hostname, ssh.FingerprintSHA256(key))
		}
		return fmt.Errorf("HOST KEY MISMATCH for %s — recorded key differs, refusing: %w", hostname, err)
	}, nil
}

func appendKnownHost(path, hostname string, key ssh.PublicKey) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key))
	return err
}
