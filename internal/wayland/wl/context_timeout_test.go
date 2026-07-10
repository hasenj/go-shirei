package wl

// Tests for the shirei-added RunTimeout: expiry must classify as
// ErrContextRunTimeout (not a fatal read error), and a deadline must never
// split an event — once the header is read, the payload read is exempt.
// These drive a raw unix socketpair; no compositor involved.

import (
	"encoding/binary"
	"net"
	"os"
	"syscall"
	"testing"
	"time"
)

// testConnPair returns both ends of a connected unix stream socketpair —
// pathless, so it dodges the sun_path length limit under test temp dirs.
func testConnPair(t *testing.T) (*net.UnixConn, *net.UnixConn) {
	t.Helper()
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	wrap := func(fd int) *net.UnixConn {
		f := os.NewFile(uintptr(fd), "socketpair")
		defer f.Close()
		c, err := net.FileConn(f)
		if err != nil {
			t.Fatal(err)
		}
		return c.(*net.UnixConn)
	}
	a, b := wrap(fds[0]), wrap(fds[1])
	t.Cleanup(func() { a.Close(); b.Close() })
	return a, b
}

func testContext(c *net.UnixConn) *Context {
	return &Context{conn: c, sockFD: -1, objects: map[ProxyId]Proxy{}}
}

// wireEvent encodes a wayland event: proxy id, opcode, total size, payload.
func wireEvent(pid uint32, opcode uint16, payload []byte) []byte {
	buf := make([]byte, 8+len(payload))
	binary.LittleEndian.PutUint32(buf[0:4], pid)
	binary.LittleEndian.PutUint16(buf[4:6], opcode)
	binary.LittleEndian.PutUint16(buf[6:8], uint16(8+len(payload)))
	copy(buf[8:], payload)
	return buf
}

// a silent peer must produce ErrContextRunTimeout after roughly the deadline
func TestRunTimeoutExpires(t *testing.T) {
	client, _ := testConnPair(t)
	ctx := testContext(client)

	start := time.Now()
	err := ctx.RunTimeout(60 * time.Millisecond)
	elapsed := time.Since(start)

	if err != ErrContextRunTimeout {
		t.Fatalf("err = %v, want ErrContextRunTimeout", err)
	}
	if elapsed < 50*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("returned after %v, want ~60ms", elapsed)
	}
}

// an event whose payload arrives after the deadline must still be read in
// full: the deadline only guards the wait for the header
func TestRunTimeoutNeverSplitsEvent(t *testing.T) {
	client, server := testConnPair(t)
	ctx := testContext(client)

	full := wireEvent(999, 0, make([]byte, 8))
	go func() {
		server.Write(full[:8]) // header now
		time.Sleep(150 * time.Millisecond)
		server.Write(full[8:]) // payload well past the 60ms deadline
	}()

	start := time.Now()
	err := ctx.RunTimeout(60 * time.Millisecond)
	elapsed := time.Since(start)

	// proxy 999 is unregistered: reaching ErrContextRunProxyNil proves the
	// event was read completely (header + late payload) with no timeout
	if err != ErrContextRunProxyNil {
		t.Fatalf("err = %v, want ErrContextRunProxyNil (event fully read)", err)
	}
	if elapsed < 140*time.Millisecond {
		t.Fatalf("returned after %v, want ≥150ms (waited for late payload)", elapsed)
	}
}

// a prompt event must dispatch immediately, well under the deadline
func TestRunTimeoutPromptEvent(t *testing.T) {
	client, server := testConnPair(t)
	ctx := testContext(client)

	server.Write(wireEvent(999, 0, nil))
	start := time.Now()
	err := ctx.RunTimeout(500 * time.Millisecond)
	if err != ErrContextRunProxyNil {
		t.Fatalf("err = %v, want ErrContextRunProxyNil", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("took %v, want immediate", elapsed)
	}
}
