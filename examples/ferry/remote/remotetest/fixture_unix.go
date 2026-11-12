//go:build !windows

package remotetest

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// StartSSHD launches /usr/sbin/sshd as the current user on a high
// localhost port. Auth is a generated key; sftp is sshd's internal-sftp.
// Everything lives in the test's temp dir and dies with the test.
func StartSSHD(t *testing.T) Fixture {
	t.Helper()
	dir := t.TempDir()

	mustKeygen(t, filepath.Join(dir, "host_key"))
	mustKeygen(t, filepath.Join(dir, "user_key"))
	pub, err := os.ReadFile(filepath.Join(dir, "user_key.pub"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "authorized_keys"), pub, 0o600); err != nil {
		t.Fatal(err)
	}

	port := freePort(t)
	conf := fmt.Sprintf(`Port %d
ListenAddress 127.0.0.1
HostKey %s
PidFile %s
AuthorizedKeysFile %s
StrictModes no
PasswordAuthentication no
KbdInteractiveAuthentication no
UsePAM no
Subsystem sftp internal-sftp
LogLevel ERROR
`, port,
		filepath.Join(dir, "host_key"),
		filepath.Join(dir, "sshd.pid"),
		filepath.Join(dir, "authorized_keys"))
	confPath := filepath.Join(dir, "sshd_config")
	if err := os.WriteFile(confPath, []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("/usr/sbin/sshd", "-f", confPath, "-D", "-e")
	// own process group: sshd forks a child per session, and Stop must
	// take those down too or a dead "server" would keep connections alive
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start sshd fixture: %v", err)
	}
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			// sshd-session children setsid into their own process groups,
			// so the listener's group kill misses them — and a surviving
			// session keeps "dead" connections alive. Kill their groups
			// first (each session leader is its own group).
			out, _ := exec.Command("pgrep", "-P", strconv.Itoa(cmd.Process.Pid)).Output()
			for _, field := range strings.Fields(string(out)) {
				if pid, err := strconv.Atoi(field); err == nil {
					syscall.Kill(-pid, syscall.SIGKILL)
				}
			}
			syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			cmd.Wait()
		})
	}
	t.Cleanup(stop)

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("sshd fixture did not come up on %s", addr)
		}
		time.Sleep(50 * time.Millisecond)
	}

	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	return Fixture{
		User:         u.Username,
		Hostname:     "127.0.0.1",
		Port:         fmt.Sprintf("%d", port),
		IdentityFile: filepath.Join(dir, "user_key"),
		KnownHosts:   filepath.Join(dir, "known_hosts"),
		Stop:         stop,
	}
}

func mustKeygen(t *testing.T, path string) {
	t.Helper()
	out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", path).CombinedOutput()
	if err != nil {
		t.Fatalf("ssh-keygen: %v: %s", err, out)
	}
}
