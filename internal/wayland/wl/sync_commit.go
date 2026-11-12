package wl

import (
	"encoding/binary"
	"fmt"

	wos "go.hasen.dev/shirei/internal/wayland/os"
)

// SyncBeforeCommit puts a sync marker immediately before a surface commit in one
// socket write. Events preceding the marker describe the old surface state;
// events caused by the commit follow it. The listener must not dispatch events
// recursively from its callback.
func (d *Display) SyncBeforeCommit(surface *Surface, listener CallbackDoneHandler) error {
	ctx := d.Context()
	if surface.Context() != ctx {
		return fmt.Errorf("sync and commit use different Wayland connections")
	}
	cb := NewCallback(ctx)
	cb.AddDoneHandler(listener)
	packet := make([]byte, 20)
	binary.LittleEndian.PutUint32(packet, uint32(d.Id()))
	binary.LittleEndian.PutUint32(packet[4:], 12<<16) // wl_display.sync(new_id)
	binary.LittleEndian.PutUint32(packet[8:], uint32(cb.Id()))
	binary.LittleEndian.PutUint32(packet[12:], uint32(surface.Id()))
	binary.LittleEndian.PutUint32(packet[16:], 8<<16|6) // wl_surface.commit
	var err error
	if ctx.conn != nil {
		var written int
		written, _, err = ctx.conn.WriteMsgUnix(packet, nil, nil)
		if err == nil && written != len(packet) {
			err = ErrContextSendRequestUnixLength
		}
	} else if ctx.sockFD != -1 {
		var addr wos.Sockaddr
		err = wos.Sendmsg(ctx.sockFD, packet, nil, addr, 0)
	} else {
		err = ErrContextSendRequestNotPossible
	}
	if err != nil {
		cb.Unregister()
	}
	return err
}
