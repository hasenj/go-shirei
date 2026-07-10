package wl

import (
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"time"

	sys "go.hasen.dev/shirei/internal/wayland/os"
	//"reflect"
)

// Context wraps the wayland connection together with the map of all Context objects (proxies)
type Context struct {
	mu        sync.RWMutex
	conn      *net.UnixConn
	sockFD    int
	currentId ProxyId
	objects   map[ProxyId]Proxy
	scms      []sys.SocketControlMessage
}

func (ctx *Context) RegisterMapped(proxy Proxy, num uint32) {
	ctx.mu.Lock()
	proxy.SetId(ProxyId(num))
	proxy.SetContext(ctx)
	ctx.objects[ProxyId(num)] = proxy
	ctx.mu.Unlock()
}

// Register registers a proxy in the map of all Context objects (proxies)
func (ctx *Context) Register(proxy Proxy) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	// avoid colliding with server side mapped proxy id?
	startId := ctx.currentId
	for {
		ctx.currentId += 1
		if _, ok := ctx.objects[ctx.currentId]; !ok {
			break
		}
		// Prevent infinite loop if all IDs are exhausted
		if ctx.currentId == startId {
			panic("proxy ID space exhausted")
		}
	}
	proxy.SetId(ctx.currentId)
	proxy.SetContext(ctx)
	if c, ok := proxy.(*Registry); ok {
		SetUserData(c, &ctx)
	}
	ctx.objects[ctx.currentId] = proxy
}

// Unregister unregisters a proxy in the map of all Context objects (proxies)
func (ctx *Context) Unregister(id ProxyId) {
	ctx.mu.Lock()
	if ctx.objects != nil {
		delete(ctx.objects, id)
	}
	ctx.mu.Unlock()
}

// LookupProxy looks up a specific proxy by it's Id in the map of all Context objects (proxies)
func (ctx *Context) LookupProxy(id ProxyId) Proxy {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	proxy, ok := ctx.objects[id]
	if !ok {
		return nil
	}
	return proxy
}

// ErrXdgRuntimeDirNotSet is returned by Connect when the operating system does not provide the required
// XDG_RUNTIME_DIR environment variable
var ErrXdgRuntimeDirNotSet = errors.New("variable XDG_RUNTIME_DIR not set in the environment")

// Connect connects to a Wayland compositor running on a specific wayland unix socket
func Connect(addr string) (ret *Display, err error) {
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		return nil, ErrXdgRuntimeDirNotSet
	}
	if addr == "" {
		addr = os.Getenv("WAYLAND_DISPLAY")
	}
	if addr == "" {
		addr = "wayland-0"
	}
	addr = runtimeDir + "/" + addr
	c := new(Context)
	c.objects = make(map[ProxyId]Proxy)
	c.currentId = 0
	c.conn, err = net.DialUnix("unix", nil, &net.UnixAddr{Name: addr, Net: "unix"})
	if err != nil {
		return nil, err
	}
	err = c.conn.SetReadDeadline(time.Time{})
	if err != nil {
		c.conn.Close()
		return nil, err
	}
	//DON'T dispatch events in separate goroutine
	//go c.Run()
	return NewDisplay(c), nil
}

var errFoundMyCallback = errors.New("run found my callback")

// RunTill (Context RunTill) runs until a specific callback or an error occurs, see Context Run
// for a description of a likely errors
func (ctx *Context) RunTill(cb *Callback) (err error) {
	for {
		err = ctx.run(cb)
		if err == errFoundMyCallback {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// ErrContextRunEventReadingError (Context Run event reading error), use InternalError to get the underlying cause
var ErrContextRunEventReadingError = errors.New("event reading error")

// ErrContextRunConnectionClosed (Context Run connection closed)
var ErrContextRunConnectionClosed = errors.New("connection closed")

// ErrContextRunTimeout (Context Run timeout error)
var ErrContextRunTimeout = errors.New("timeout error")

// ErrContextRunProtocolError (Context Run protocol error), use InternalError to get the underlying cause
var ErrContextRunProtocolError = errors.New("protocol error")

// ErrContextRunNotDispatched (Context Run not dispatched)
var ErrContextRunNotDispatched = errors.New("not dispatched")

// ErrContextRunProxyNil (Context Run proxy nil)
var ErrContextRunProxyNil = errors.New("proxy nil")

// Run (Context Run) reads and processes one event, a specific ErrContextRunXXX error
// may be returned in case of failure
func (ctx *Context) Run() error {
	return ctx.run(nil)
}

// RunTimeout is Run with a read deadline: it processes one event, or returns
// ErrContextRunTimeout when no event arrives within d. The deadline only
// guards the wait for an event's header; once a header is read, the payload
// read is not subject to it (readEvent clears the deadline), so a timeout
// can never split an event and desync the stream. [shirei addition]
func (ctx *Context) RunTimeout(d time.Duration) error {
	if ctx == nil {
		return ErrContextNil
	}
	if ctx.conn == nil {
		return ErrContextConnNil
	}
	if err := ctx.conn.SetReadDeadline(time.Now().Add(d)); err != nil {
		return err
	}
	return ctx.run(nil)
}

// ErrContextNil (Error context is nil) occurs if the thread closes context and then
// it wants to run, another thread probably cannot close it safely
var ErrContextNil = errors.New("context is nil")

// ErrContextConnNil (Error context conn is nil) occurs if the thread closes context and then
// it wants to run, another thread probably cannot close it safely
var ErrContextConnNil = errors.New("context conn is nil")

func (ctx *Context) run(cb *Callback) error {
	// ctx := context.Background()

	if ctx == nil {
		return ErrContextNil
	}

	ev, err := ctx.readEvent()
	if err != nil {
		if err == io.EOF {
			return ErrContextRunConnectionClosed
		}

		if neterr, ok := err.(net.Error); ok && neterr.Timeout() {
			return ErrContextRunTimeout
		}

		return combinedError{ErrContextRunEventReadingError, err}
	}

	proxy := ctx.LookupProxy(ev.Pid)
	if proxy != nil {
		if dispatcher, ok := proxy.(Dispatcher); dispatcher != nil && ok {
			if foundCb, ok := dispatcher.(*Callback); ok {
				if foundCb == cb {
					bytePool.Give(ev.Data)
					return errFoundMyCallback
				}
			}
			dispatcher.Dispatch(ev)
			bytePool.Give(ev.Data)

			if ev.err != nil {
				return combinedError{ErrContextRunProtocolError, ev.err}
			}
		} else {
			return ErrContextRunNotDispatched
		}
	} else {
		return ErrContextRunProxyNil
	}
	return nil
}

// Close (Context Close) closes Wayland connection
func (ctx *Context) Close() (err error) {
	if ctx == nil {
		return
	}
	ctx.mu.Lock()
	if ctx.conn != nil {
		err = ctx.conn.Close()
		ctx.conn = nil
	}
	ctx.sockFD = -1
	/*
		for i, v := range ctx.objects {
			print("close-time garbage: ")
			print(i)
			print(": ")
			println(reflect.TypeOf(v).String())
		}
	*/
	ctx.objects = nil
	ctx.scms = nil
	ctx.mu.Unlock()
	return err
}
