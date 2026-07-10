package remotetest

// The throwaway-sshd fixture cannot test password auth: sshd only checks
// passwords as root (via PAM / the system user database). This in-process
// x/crypto server stands in for that one gap — it accepts exactly one
// password, no publickey at all, and serves the real sftp subsystem
// (rooted wherever the client asks; pkg/sftp serves the process view of
// the filesystem). Same rule as the sshd fixture: no ferry/remote import,
// callers assemble their own Host from the returned address.

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net"
	"testing"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// StartPasswordServer listens on a random localhost port and serves
// ssh+sftp to any client that presents the given password. Wrong
// passwords are rejected (the client may retry within one connection).
// Dies with the test.
func StartPasswordServer(t *testing.T, password string) (hostname, port string) {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}

	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if string(pass) == password {
				return nil, nil
			}
			return nil, fmt.Errorf("wrong password for %s", c.User())
		},
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed at cleanup
			}
			go servePasswordConn(conn, cfg)
		}
	}()

	hostname, port, err = net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	return hostname, port
}

func servePasswordConn(conn net.Conn, cfg *ssh.ServerConfig) {
	sconn, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		conn.Close() // auth failed (or the client gave up retrying)
		return
	}
	defer sconn.Close()
	go ssh.DiscardRequests(reqs)
	for newCh := range chans {
		if newCh.ChannelType() != "session" {
			newCh.Reject(ssh.UnknownChannelType, "only session channels")
			continue
		}
		ch, requests, err := newCh.Accept()
		if err != nil {
			continue
		}
		go serveSession(ch, requests)
	}
}

// serveSession answers the sftp subsystem request and serves it; every
// other request type (exec, shell, pty) is refused — this server exists
// for the auth path, not for transfers.
func serveSession(ch ssh.Channel, requests <-chan *ssh.Request) {
	for req := range requests {
		// subsystem payload: uint32 name length + name
		if req.Type == "subsystem" && len(req.Payload) > 4 && string(req.Payload[4:]) == "sftp" {
			req.Reply(true, nil)
			if srv, err := sftp.NewServer(ch); err == nil {
				srv.Serve()
			}
			ch.Close()
			return
		}
		req.Reply(false, nil)
	}
}
