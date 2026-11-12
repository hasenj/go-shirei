package remotetest

import "testing"

// StartSSHD requires a Unix sshd and process groups for session cleanup.
// The in-process password server is available on Windows.
func StartSSHD(t *testing.T) Fixture {
	t.Helper()
	t.Skip("Unix sshd fixture requires /usr/sbin/sshd and process groups")
	return Fixture{}
}
