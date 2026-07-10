package main

// A Session is one live server connection and everything that belongs to
// it: the sftp connection, the remote pane showing it, its disconnect
// state, and its deletion bin. Tabs let several sessions stay alive at
// once (appData.sessions); appData.active is the one on screen. The local
// pane (appData.left) is shared across all of them — one local machine.
//
// What is NOT per-session: the transfer queue and its panel are global (a
// transfer records which server it targets — Transfer.Server/Conn), and
// the modals (host-key, password, conflict, new-folder) are one-at-a-time
// and act on the active session.

import (
	"go.hasen.dev/shirei"

	"go.hasen.dev/shirei/examples/ferry/remote"
)

type Session struct {
	Conn          *remote.Conn
	Alias         string
	Pane          *Pane // the remote pane for this connection
	Disconnected  bool
	DisconnectErr error

	// per-session deletion bin (deletebin.go): staged deletions belong to
	// one server and must not leak across tabs
	deleteBin   []BinItem
	binned      map[string]bool
	binExpanded bool
	deleteBusy  bool
	deleteErr   error
}

// hasRemote reports whether there is an active, still-live server.
func (a *AppState) hasRemote() bool {
	return a.active != nil && a.active.Conn != nil && !a.active.Disconnected
}

// remotePane is the active session's pane, or nil on the servers screen.
func (a *AppState) remotePane() *Pane {
	if a.active == nil {
		return nil
	}
	return a.active.Pane
}

// addSession appends a new connected session and makes it active.
func addSession(s *Session) {
	appData.sessions = append(appData.sessions, s)
	activateSession(s)
}

// activateSession brings s on screen: it becomes the remote pane, and
// arrow keys act on it.
func activateSession(s *Session) {
	appData.active = s
	appData.activePane = s.Pane
	appData.screen = ScreenMain
}

// closeSession disconnects s, drops its queued transfers, removes its
// tab, and activates a neighbor (or returns to the servers screen when it
// was the last one). The bin warning, if any, is handled by the caller
// before this runs.
func closeSession(s *Session) {
	cancelSessionTransfers(s)
	if s.Conn != nil {
		conn := s.Conn
		s.Conn = nil // the disconnect watcher checks this before flagging
		go conn.Close()
	}

	idx := -1
	for i, x := range appData.sessions {
		if x == s {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	appData.sessions = append(appData.sessions[:idx], appData.sessions[idx+1:]...)

	if appData.active != s {
		return // closed a background tab; the active one is unchanged
	}
	switch {
	case len(appData.sessions) == 0:
		appData.active = nil
		appData.activePane = appData.left
		appData.screen = ScreenServers
	default:
		// activate the neighbor that slid into this slot (or the last one)
		next := appData.sessions[min(idx, len(appData.sessions)-1)]
		activateSession(next)
	}
}

// reconnectSession re-dials a dropped session's host in place, keeping
// its tab and cwd. Called from UI (frame lock held).
func reconnectSession(s *Session) {
	host, ok := hostByAlias(s.Alias)
	if !ok || appData.connecting != "" {
		return
	}
	resumeCWD := ""
	if s.Pane != nil {
		resumeCWD = s.Pane.CWD
	}
	appData.connecting = s.Alias
	delete(appData.connectErrs, s.Alias)

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
				appData.connectErrs[s.Alias] = err
				return
			}
			s.Conn = conn
			s.Disconnected = false
			s.DisconnectErr = nil
			s.Pane = newPane(PaneFS{
				Label: s.Alias, Home: home,
				List: conn.List, ReadHead: conn.ReadHead, Mkdir: conn.Mkdir,
			}, false)
			if appData.active == s {
				appData.activePane = s.Pane
			}
		})
		shirei.RequestNextFrame()
		if err == nil {
			watchDisconnect(conn)
		}
	}()
}

// sessionOfConn finds the session owning conn (for the disconnect
// watcher, which only has the connection in hand).
func sessionOfConn(conn *remote.Conn) *Session {
	for _, s := range appData.sessions {
		if s.Conn == conn {
			return s
		}
	}
	return nil
}

// watchDisconnect parks on the ssh transport and flags the owning session
// if it dies while still live.
func watchDisconnect(conn *remote.Conn) {
	err := conn.Wait()
	shirei.WithFrameLock(func() {
		if s := sessionOfConn(conn); s != nil {
			s.Disconnected = true
			s.DisconnectErr = err
		}
	})
	shirei.RequestNextFrame()
}
