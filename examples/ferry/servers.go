package main

import (
	"golang.org/x/crypto/ssh"

	"go.hasen.dev/shirei"

	"go.hasen.dev/shirei/examples/ferry/remote"
)

type Screen int

const (
	ScreenServers Screen = iota
	ScreenMain
)

// HostKeyRequest bridges Dial's blocking accept callback (running on the
// connect goroutine) to the GUI modal: the goroutine parks on Answer
// while the user decides.
type HostKeyRequest struct {
	Addr        string
	Fingerprint string
	Answer      chan bool
}

// PasswordRequest bridges Dial's password callback to the GUI modal, same
// parking pattern as HostKeyRequest. Buf is the modal's input buffer —
// living on the request keeps a cancelled attempt from leaking into the
// next prompt. Attempt > 1 means the previous password was rejected.
type PasswordRequest struct {
	User    string
	Addr    string
	Attempt int
	Buf     string
	Answer  chan passwordAnswer
}

type passwordAnswer struct {
	password string
	ok       bool
}

// loadHosts enumerates the entry-point ssh config in use — by default the
// real ~/.ssh/config (invariant 6: the deliberate final-step switch;
// automated tests still use fixtures / an explicit -F dev config, never
// this).
func loadHosts() {
	appData.hosts, appData.hostsErr = remote.EnumerateHosts(defaultConfigPath())
}

// startConnect dials the host in the background and opens it as a new
// session/tab (existing tabs stay live). resumeCWD, when non-empty,
// becomes the remote pane's starting directory. Called from UI code, so
// the frame lock is held.
func startConnect(host remote.Host, resumeCWD string) {
	if appData.connecting != "" {
		return
	}
	appData.connecting = host.Alias
	delete(appData.connectErrs, host.Alias)

	go func() {
		conn, err := remote.Dial(host, remote.DialOptions{
			KnownHostsPath: appData.knownHostsPath,
			AcceptHostKey:  guiHostKeyPrompt,
			AskPassword:    guiPasswordPrompt,
		})
		home := resumeCWD
		if err == nil && home == "" {
			home, err = conn.HomeDir()
		}
		shirei.WithFrameLock(func() {
			appData.connecting = ""
			if err != nil {
				if conn != nil {
					conn.Close()
				}
				appData.connectErrs[host.Alias] = err
				return
			}
			pane := newPane(PaneFS{
				Label:    host.Alias,
				Home:     home,
				List:     conn.List,
				ReadHead: conn.ReadHead,
				Mkdir:    conn.Mkdir,
			}, false)
			addSession(&Session{Conn: conn, Alias: host.Alias, Pane: pane})
		})
		shirei.RequestNextFrame()
		if err == nil {
			watchDisconnect(conn)
		}
	}()
}

// guiHostKeyPrompt runs on the connect goroutine: publish the request for
// the modal, park until the user answers.
func guiHostKeyPrompt(addr string, key ssh.PublicKey) bool {
	req := &HostKeyRequest{
		Addr:        addr,
		Fingerprint: ssh.FingerprintSHA256(key),
		Answer:      make(chan bool, 1),
	}
	shirei.WithFrameLock(func() { appData.hostKeyReq = req })
	shirei.RequestNextFrame()
	answer := <-req.Answer
	shirei.WithFrameLock(func() {
		if appData.hostKeyReq == req {
			appData.hostKeyReq = nil
		}
	})
	shirei.RequestNextFrame()
	return answer
}

// guiPasswordPrompt runs on the connect goroutine: publish the request
// for the modal, park until the user submits or cancels.
func guiPasswordPrompt(user, addr string, attempt int) (string, bool) {
	req := &PasswordRequest{
		User:    user,
		Addr:    addr,
		Attempt: attempt,
		Answer:  make(chan passwordAnswer, 1),
	}
	shirei.WithFrameLock(func() { appData.passwordReq = req })
	shirei.RequestNextFrame()
	a := <-req.Answer
	shirei.WithFrameLock(func() {
		if appData.passwordReq == req {
			appData.passwordReq = nil
		}
	})
	shirei.RequestNextFrame()
	return a.password, a.ok
}

// requestServersScreen shows the servers list to add another connection.
// Existing tabs stay live — this no longer disconnects anything.
func requestServersScreen() {
	appData.screen = ScreenServers
}

// requestCloseTab closes a session's tab, but a non-empty deletion bin
// gets a warning first (LeaveConfirmModal): its staged deletions never
// ran, and the user must not close it believing they did.
func requestCloseTab(s *Session) {
	if len(s.deleteBin) > 0 {
		appData.closeTarget = s
		appData.leaveConfirm = true
		return
	}
	closeSession(s)
}

func hostByAlias(alias string) (remote.Host, bool) {
	for _, h := range appData.hosts {
		if h.Alias == alias {
			return h, true
		}
	}
	return remote.Host{}, false
}
