// Package wl implements the stable Wayland protocol
package wl

// ProxyId is a Proxy identifier that is sent to compositor over the wayland socket
type ProxyId uint32

// Dispatcher is anything that can process an Event
type Dispatcher interface {
	Dispatch(*Event)
}

// Proxy object
type Proxy interface {
	Context() *Context
	SetContext(c *Context)
	Id() ProxyId
	SetId(id ProxyId)
	//	Name() uint32
	//	SetName(name uint32)
	Unregister()
}

// BaseProxy (Base Proxy) is a struct that stores Context and ProxyId explicitly
type BaseProxy struct {
	id  ProxyId
	ctx *Context
}

// Id BaseProxy implements Id to get ProxyId
func (p *BaseProxy) Id() ProxyId {
	return p.id
}

// SetId BaseProxy implements SetId to set ProxyId
func (p *BaseProxy) SetId(id ProxyId) {
	p.id = id
}

// Context BaseProxy implements Context to get Context
func (p *BaseProxy) Context() *Context {
	return p.ctx
}

// SetContext BaseProxy implements SetContext to set Context
func (p *BaseProxy) SetContext(c *Context) {
	p.ctx = c
}

// BaseProxy implements Name
//func (p *BaseProxy) Name() uint32 {
//	return p.name
//}

// BaseProxy implements SetName
//func (p *BaseProxy) SetName(name uint32) {
//	p.name = name
//}

// Unregister BaseProxy Unregister removes this BaseProxy from the map of all Context objects
func (p *BaseProxy) Unregister() {
	if p != nil && p.ctx != nil {
		p.ctx.Unregister(p.id)
	}
}
