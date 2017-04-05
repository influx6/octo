package client

import (
	"sync"

	"github.com/influx6/octo"
)

// Stream defines an interface that provides a means of delivery giving messages
// to another encapsulated endpoint.
type Stream interface {
	Close() error
	Send(interface{}, bool) error
}

// Server defines a interface which exposes a method to handle/process a giving
// byte slice and recieves the core pipe for response.
type Server interface {
	Serve(interface{}, Stream) error
}

// SystemServer defines a interface which exposes its credentials and servicing
// methods for usage.
type SystemServer interface {
	Server
	octo.Credentials
}

// System defines a interface which provides a means by which connections
// are made and processed.
type System interface {
	Stream
	Listen(SystemServer, octo.MessageEncoding) error
	Register(StateHandlerType, interface{})
}

//================================================================================

// StateHandlerType defines a int type to specific a handler type for registry.
type StateHandlerType int

// contains the value to reference the handler to be registered for.
const (
	ConnectHandler StateHandlerType = iota
	DisconnectHandler
	ClosedHandler
	ErrorHandler
)

// StateHandler defines a function which is called for the change of state of a
// connection eg closed, connected, disconnected.
type StateHandler func(octo.Contact, Stream)

// ErrorStateHandler defines a function which is called for the error that occurs
// during connections.
type ErrorStateHandler func(octo.Contact, Stream, error)

// Pub defines a set of structure for holding different callbacks for the lifecycle
// operations of a giving connection.
type Pub struct {
	cml         sync.Mutex
	connects    []StateHandler
	disconnects []StateHandler
	closes      []StateHandler
	errors      []ErrorStateHandler
}

// NewPub returns a new instance of a Pub.
func NewPub() *Pub {
	var pub Pub
	return &pub
}

// Clear empties all handlers registered  to the Pub.
func (w *Pub) Clear() {
	w.cml.Lock()
	w.connects = nil
	w.disconnects = nil
	w.closes = nil
	w.errors = nil
	w.cml.Unlock()
}

// Register registers the handler for a given handler.
func (w *Pub) Register(tm StateHandlerType, hmi interface{}) {
	var hms StateHandler
	var hme ErrorStateHandler

	switch ho := hmi.(type) {
	case StateHandler:
		hms = ho
	case ErrorStateHandler:
		hme = ho
	}

	// If the type does not match then return
	if hme == nil && tm == ErrorHandler {
		return
	}

	// If the type does not match then return
	if hms == nil && tm != ErrorHandler {
		return
	}

	switch tm {
	case ConnectHandler:
		w.cml.Lock()
		w.connects = append(w.connects, hms)
		w.cml.Unlock()
	case DisconnectHandler:
		w.cml.Lock()
		w.disconnects = append(w.disconnects, hms)
		w.cml.Unlock()
	case ErrorHandler:
		w.cml.Lock()
		w.errors = append(w.errors, hme)
		w.cml.Unlock()
	case ClosedHandler:
		w.cml.Lock()
		w.closes = append(w.closes, hms)
		w.cml.Unlock()
	}
}

// Notify calls the giving callbacks for each different type of state.
func (w *Pub) Notify(n StateHandlerType, cm octo.Contact, sm Stream, err error) {
	switch n {
	case ErrorHandler:
		w.cml.Lock()
		defer w.cml.Unlock()

		for _, handler := range w.errors {
			handler(cm, sm, err)
		}
	case ConnectHandler:
		w.cml.Lock()
		defer w.cml.Unlock()

		for _, handler := range w.connects {
			handler(cm, sm)
		}
	case DisconnectHandler:
		w.cml.Lock()
		defer w.cml.Unlock()

		for _, handler := range w.disconnects {
			handler(cm, sm)
		}
	case ClosedHandler:
		w.cml.Lock()
		defer w.cml.Unlock()

		for _, handler := range w.closes {
			handler(cm, sm)
		}
	}
}

//================================================================================
