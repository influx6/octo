package server

import (
	"github.com/influx6/faux/context"

	"github.com/influx6/octo"
)

// Stream defines an interface which exposes methods to transmit data over
// a giving transmit.
type Stream interface {
	Close() error
	Send([]byte, bool) error
	SendAll(data []byte, flush bool) error
	Contact() (octo.Contact, octo.Contact)
	Ctx() context.Context
}

// Server defines a type which exposes a method to service a provided
// data.
type Server interface {
	Serve(data []byte, tx Stream) error
}

// System defines a interface for processing systems which handle request
// connections from a server.
type System interface {
	octo.Authenticator
	Server
}
