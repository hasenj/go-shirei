package remote

// Password-auth tests run against remotetest.StartPasswordServer — an
// in-process x/crypto server, because a non-root sshd cannot check
// passwords (see that file's comment). The server offers ONLY password
// auth, so these also pin the method order contract: publickey methods
// being present must not block the password fallback.

import (
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"go.hasen.dev/shirei/examples/ferry/remote/remotetest"
)

func passwordDialOptions(t *testing.T, prompt PasswordPrompt) DialOptions {
	t.Helper()
	return DialOptions{
		KnownHostsPath: filepath.Join(t.TempDir(), "known_hosts"),
		AcceptHostKey:  func(addr string, key ssh.PublicKey) bool { return true },
		AskPassword:    prompt,
	}
}

func passwordHost(hostname, port string) Host {
	return Host{Alias: "pw-fixture", Hostname: hostname, User: "tester", Port: port}
}

// scriptedPrompt answers each attempt from the list, recording the
// attempt numbers it saw. Attempts beyond the script cancel.
func scriptedPrompt(answers []string, seen *[]int) PasswordPrompt {
	return func(user, addr string, attempt int) (string, bool) {
		*seen = append(*seen, attempt)
		if len(*seen) > len(answers) {
			return "", false
		}
		return answers[len(*seen)-1], true
	}
}

func TestDialPasswordSuccess(t *testing.T) {
	hostname, port := remotetest.StartPasswordServer(t, "sesame")
	var seen []int
	conn, err := Dial(passwordHost(hostname, port), passwordDialOptions(t, scriptedPrompt([]string{"sesame"}, &seen)))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if len(seen) != 1 || seen[0] != 1 {
		t.Fatalf("prompt attempts = %v, want [1]", seen)
	}
	// the connection is real: sftp works over it
	if _, err := conn.List("."); err != nil {
		t.Fatalf("sftp listing over password auth: %v", err)
	}
}

func TestDialPasswordRetry(t *testing.T) {
	hostname, port := remotetest.StartPasswordServer(t, "sesame")
	var seen []int
	conn, err := Dial(passwordHost(hostname, port),
		passwordDialOptions(t, scriptedPrompt([]string{"wrong", "still wrong", "sesame"}, &seen)))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if len(seen) != 3 || seen[2] != 3 {
		t.Fatalf("prompt attempts = %v, want [1 2 3]", seen)
	}
}

func TestDialPasswordCancel(t *testing.T) {
	hostname, port := remotetest.StartPasswordServer(t, "sesame")
	prompt := func(user, addr string, attempt int) (string, bool) { return "", false }
	conn, err := Dial(passwordHost(hostname, port), passwordDialOptions(t, prompt))
	if err == nil {
		conn.Close()
		t.Fatal("cancelled prompt must fail the dial")
	}
	if !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("error should say the entry was cancelled: %v", err)
	}
}

func TestDialPasswordExhausted(t *testing.T) {
	hostname, port := remotetest.StartPasswordServer(t, "sesame")
	var seen []int
	prompt := func(user, addr string, attempt int) (string, bool) {
		seen = append(seen, attempt)
		return "never right", true
	}
	conn, err := Dial(passwordHost(hostname, port), passwordDialOptions(t, prompt))
	if err == nil {
		conn.Close()
		t.Fatal("exhausted retries must fail the dial")
	}
	if len(seen) != passwordAttempts {
		t.Fatalf("prompt ran %d times, want %d", len(seen), passwordAttempts)
	}
}
