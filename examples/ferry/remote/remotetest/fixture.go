// Package remotetest provides a throwaway-sshd test fixture for any package
// that needs a real ssh+sftp endpoint in tests. It deliberately avoids
// importing the remote package (so remote's own tests can use it): callers
// assemble their own Host value from the Fixture fields.
package remotetest

import (
	"net"
	"testing"
)

type Fixture struct {
	User         string
	Hostname     string
	Port         string
	IdentityFile string
	KnownHosts   string // path for a fresh known_hosts file in the fixture dir

	// Stop kills the sshd and every forked session child — simulates the
	// server dying mid-session (for disconnect-handling tests). Also runs
	// automatically at test cleanup.
	Stop func()
}

func (f Fixture) Addr() string { return net.JoinHostPort(f.Hostname, f.Port) }

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
