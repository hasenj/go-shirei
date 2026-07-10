package wl

import (
	"bytes"
	"errors"
	"net"
	"time"

	"encoding/binary"
	"go.hasen.dev/shirei/internal/wayland/os"
)

// Event is the Wayland event (e.g. a response) from the compositor
type Event struct {
	Pid    ProxyId
	Opcode uint32
	Data   []byte
	off    int
	err    error
	ctx    *Context
}

// ErrReadHeader (Error unable to read message header) is returned when it is not possible to read enough bytes from the unix socket,
// use Unwrap() to get the underlying cause and GetExternal to get this cause
var ErrReadHeader = errors.New("unable to read message header")

// ErrSizeOfHeaderWrong (Error size of message header is wrong) is returned when the returned size of message heaer is not 8 bytes
var ErrSizeOfHeaderWrong = errors.New("size of message header is wrong")

// ErrControlMsgBuffer (Error insufficient control msg buffer) is returned when the oobn is bigger than the control message buffer
var ErrControlMsgBuffer = errors.New("insufficient control msg buffer")

// ErrControlMsgParseError (Error control message parse error) is returned when the unix socket control message cannot be parsed, use
// Unwrap() to get the underlying cause and GetExternal to get this cause
var ErrControlMsgParseError = errors.New("control message parse error")

// ErrInvalidMsgSize (Error invalid message size) is returned when the payload message size read from the unix socket is incorrect
var ErrInvalidMsgSize = errors.New("invalid message size")

// ErrReadPayload (Error cannot read message) is returned when the payload message cannot be read, use Unwrap() to get the
// underlying cause and GetExternal to get this cause
var ErrReadPayload = errors.New("cannot read message")

func (ctx *Context) readEvent() (*Event, error) {
	buf := bytePool.Take(8)
	control := bytePool.Take(24)

	if ctx.conn == nil {
		return nil, ErrContextConnNil
	}

	n, oobn, _, _, err := ctx.conn.ReadMsgUnix(buf[:], control)
	if err != nil {
		// propagate deadline expiry unwrapped so run() can classify it as
		// ErrContextRunTimeout (RunTimeout support) [shirei addition]
		if neterr, ok := err.(net.Error); ok && neterr.Timeout() {
			return nil, err
		}
		return nil, combinedError{ErrReadHeader, err}
	}
	if n != 8 {
		return nil, ErrSizeOfHeaderWrong
	}
	ev := new(Event)
	ev.ctx = ctx
	if oobn > 0 {
		if oobn > len(control) {
			return nil, ErrControlMsgBuffer
		}
		scms, err := os.ParseSocketControlMessage(control)
		if err != nil {
			return nil, combinedError{ErrControlMsgParseError, err}
		}
		ctx.scms = append(ctx.scms, scms...)
	}

	// A header was read: the payload follows immediately, so it must not be
	// subject to a RunTimeout deadline — a mid-event timeout would desync
	// the stream. [shirei addition]
	ctx.conn.SetReadDeadline(time.Time{})

	ev.Pid = ProxyId(binary.LittleEndian.Uint32(buf[0:4]))
	ev.Opcode = uint32(binary.LittleEndian.Uint16(buf[4:6]))
	size := uint32(binary.LittleEndian.Uint16(buf[6:8]))

	// subtract 8 bytes from header
	data := bytePool.Take(int(size) - 8)
	n, err = ctx.conn.Read(data)
	if err != nil {
		return nil, combinedError{ErrReadPayload, err}
	}
	if n != int(size)-8 {
		return nil, ErrInvalidMsgSize
	}
	ev.Data = data

	bytePool.Give(buf)
	bytePool.Give(control)

	return ev, nil
}

// ErrNoControlMsgs (Error no socket control messages)
var ErrNoControlMsgs = errors.New("no socket control messages")

// ErrUnableToParseUnixRights (Error unable to parse unix rights)
var ErrUnableToParseUnixRights = errors.New("unable to parse unix rights")

// FD (Event FD) extracts the file descriptor and an optional error
func (ev *Event) FD() (uintptr, error) {
	if ev.err != nil {
		return 0, ev.err
	}
	if len(ev.ctx.scms) == 0 {
		return 0, ErrNoControlMsgs
	}
	fds, err := os.ParseUnixRights(&ev.ctx.scms[0])
	if err != nil {
		return 0, ErrUnableToParseUnixRights
	}
	//TODO: is this required??????????????
	ev.ctx.scms = ev.ctx.scms[1:]
	return uintptr(fds[0]), nil
}

// ErrUnableToParseUint32 (Error unable to read unsigned int) is returned when the buffer is too short to contain a specific unsigned int
var ErrUnableToParseUint32 = errors.New("unable to read unsigned int")

// Uint32 (Event Uint32) decodes an Uint32 from the Event
func (ev *Event) Uint32() uint32 {
	buf := ev.next(4)
	if len(buf) != 4 {
		ev.err = ErrUnableToParseUint32
		return 0
	}
	return binary.LittleEndian.Uint32(buf)
}

// SafeCast casts to any type, it doesn't panic if proxy is nil
func SafeCast[T any](p Proxy) (out T) {
	out, _ = p.(T)
	return
}

// Proxy (Event Proxy) decodes Proxy by it's Id from the Event
func (ev *Event) Proxy(c *Context) Proxy {
	id := ev.Uint32()
	if id != 0 {
		return c.LookupProxy(ProxyId(id))
	}
	return nil
}

// ErrUnableToParseString (Error unable to parse string) is returned when the buffer is too short to contain a specific string
var ErrUnableToParseString = errors.New("unable to parse string")

// String (Event String) decodes a string from the Event
func (ev *Event) String() string {
	l := int(ev.Uint32())
	buf := ev.next(l)
	if len(buf) != l {
		ev.err = ErrUnableToParseString
		return ""
	}
	ret := string(bytes.TrimRight(buf, "\x00"))
	//padding to 32 bit boundary
	if (l & 0x3) != 0 {
		ev.next(4 - (l & 0x3))
	}
	return ret
}

// Int32 (Event Int32) decodes an Int32 from the Event
func (ev *Event) Int32() int32 {
	return int32(ev.Uint32())
}

// Float32 (Event Float32) decodes a Float32 from the Event
func (ev *Event) Float32() float32 {
	return float32(FixedToFloat(ev.Int32()))
}

// Array (Event Array) decodes an Array from the Event
func (ev *Event) Array() []int32 {
	l := int(ev.Uint32())
	arr := make([]int32, l/4)
	for i := range arr {
		arr[i] = ev.Int32()
	}
	return arr
}

func (ev *Event) next(n int) []byte {
	ret := ev.Data[ev.off : ev.off+n]
	ev.off += n
	return ret
}

func (ev *Event) NewId(i Proxy, c *Context) Proxy {
	c.RegisterMapped(i, ev.Uint32())
	return i
}
