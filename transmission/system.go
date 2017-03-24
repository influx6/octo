package transmission

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/influx6/faux/context"
	"github.com/influx6/octo"
	"github.com/influx6/octo/consts"
)

//================================================================================

// Stream defines an interface which exposes methods to transmit data over
// a giving transmit.
type Stream interface {
	Close() error
	Contact() (octo.Contact, octo.Contact)
	Ctx() context.Context
	Send(data []byte, flush bool) error
	SendAll(data []byte, flush bool) error
}

// Server defines a type which exposes a method to service a provided
// data.
type Server interface {
	Serve(data []byte, tx Stream) error
}

//================================================================================

// System defines a interface for processing systems which handle request
// connections from a server.
type System interface {
	octo.Authenticator
	Server
}

//================================================================================

// ProtocolServer defines a type which manages all TranmissionProtocol with it's underline
// system.
type ProtocolServer struct {
	system System
	procs  []Protocol
}

// New returns a new instance of a ProtocolServer which manages the interaction
// between a series of systems and a series of TranmissionProtocol.
func New(system System) *ProtocolServer {
	return &ProtocolServer{
		system: system,
	}
}

// Use initiates the giving TransmissionProtocol with the internal system manager
// of the oction, add it into it's internal protocol list.
func (o *ProtocolServer) Use(tx Protocol) error {
	o.procs = append(o.procs, tx)
	return tx.Listen(o.system)
}

// Close closes all internal protocols and returns the last error received.
func (o *ProtocolServer) Close() error {
	procs := o.procs
	o.procs = nil

	var err error
	for _, procs := range procs {
		err = procs.Close()
	}

	return err
}

//================================================================================

// SelectiveServer defines a tye which exposes the capability to
// test if the data lies within it's jurisdiction for service.
type SelectiveServer interface {
	Server
	CanServe(data []byte) bool
}

// SelectiveServers defines a slice of TransmissionServer and exposes a method to
// use this has a single server where if an element returns an error then that
// error is .
type SelectiveServers []SelectiveServer

// Serve delivers the data and Transmission to the first capable server by calling
// each servers to individually attests to their  capability to serve the request.
func (s SelectiveServers) Serve(data []byte, tx Stream) error {
	for _, server := range s {
		if !server.CanServe(data) {
			continue
		}

		return server.Serve(data, tx)
	}

	return consts.ErrRequestUnsearvable
}

//================================================================================

// Protocol defines a type which accepts a system and encapsulates the transmission
// and exchange of data using the System has the processing unit.
type Protocol interface {
	Close() error
	Listen(System) error
}

//================================================================================

// Handler defines a function type for handle message requests.
type Handler func(octo.Command, Stream) error

// HandlerMap defines a map type for a registry of string keyed
// MessageHandlers.
type HandlerMap map[string]Handler

// String returns a slice of keynames for the giving registry
func (m HandlerMap) String() string {
	var keys []string

	for key := range m {
		keys = append(keys, strconv.Quote(key))
	}

	return fmt.Sprintf("[%s]", strings.Join(keys, ", "))
}

//================================================================================

// BaseSystem defines a structure which implements the System
// interface and allows customization of internal handlers.
type BaseSystem struct {
	handlers      HandlerMap
	authenticator octo.Authenticator
	log           octo.Logs
	parser        octo.Parser
}

// NewBaseSystem returns a new instance of a BaseSystem.
func NewBaseSystem(authenticator octo.Authenticator, parser octo.Parser, log octo.Logs, handles ...HandlerMap) *BaseSystem {
	var b BaseSystem
	b.log = log
	b.parser = parser
	b.handlers = make(map[string]Handler)
	b.authenticator = authenticator

	b.AddAll(handles...)
	return &b
}

// AddAll adds the contents giving set of handler map into the BaseSystem.
func (b *BaseSystem) AddAll(items ...HandlerMap) {
	b.log.Log(octo.LOGINFO, "octo.BaseSystem", "AddAll", "Started : Items : %+s", items)

	for _, item := range items {
		for tag, handle := range item {
			b.handlers[strings.ToLower(tag)] = handle
		}
	}

	b.log.Log(octo.LOGINFO, "octo.BaseSystem", "AddAll", "Completed")
}

// Add adds the giving handler using the expected tag and over-writes
// any preious tag found.
func (b *BaseSystem) Add(tag string, handle Handler) {
	b.log.Log(octo.LOGINFO, "octo.BaseSystem", "Add", "Started : Adding %q")
	b.handlers[strings.ToLower(tag)] = handle
	b.log.Log(octo.LOGINFO, "octo.BaseSystem", "Add", "Completed")
}

// Authenticate authenticates all credentials and returns true.
func (b *BaseSystem) Authenticate(auth octo.AuthCredential) error {
	b.log.Log(octo.LOGINFO, "octo.BaseSystem", "Authenticate", "Started : %#v", auth)
	if b.authenticator != nil {
		if err := b.authenticator.Authenticate(auth); err != nil {
			b.log.Log(octo.LOGERROR, "octo.BaseSystem", "Authenticate", "Completed : %+q", err.Error())
			return err
		}
	}

	b.log.Log(octo.LOGINFO, "octo.BaseSystem", "Authenticate", "Completed")
	return nil
}

// ServeBase handles message received and returns messages slice it can not handle.
func (b *BaseSystem) ServeBase(data []byte, tx Stream) ([]byte, error) {
	b.log.Log(octo.LOGINFO, "octo.BaseSystem", "ServeBase", "Started : %+q", data)

	messages, err := b.parser.Parse(data)
	if err != nil {
		b.log.Log(octo.LOGERROR, "octo.BaseSystem", "ServeBase", "Completed : %+q", err.Error())
		return nil, err
	}

	var unserved []octo.Command

	for _, message := range messages {
		b.log.Log(octo.LOGINFO, "octo.BaseSystem", "ServeBase", "Serving Message : %+s", message)

		command := strings.ToLower(string(message.Name))
		if handler, ok := b.handlers[command]; ok {
			if herr := handler(message, tx); err != nil {
				b.log.Log(octo.LOGERROR, "octo.BaseSystem", "ServeBase", "ServeError : %+q", herr.Error())
				return nil, herr
			}
		} else {
			unserved = append(unserved, message)
		}
	}

	b.log.Log(octo.LOGINFO, "octo.BaseSystem", "ServeBase", "Unserved : %+s", unserved)
	if unserved == nil {
		b.log.Log(octo.LOGINFO, "octo.BaseSystem", "ServeBase", "Completed")
		return nil, nil
	}

	pdata, err := b.parser.Unparse(unserved)
	if err != nil {
		b.log.Log(octo.LOGERROR, "octo.BaseSystem", "ServeBase", "Completed : Unable to unparse remainder")
		return nil, err
	}

	b.log.Log(octo.LOGINFO, "octo.BaseSystem", "ServeBase", "Unparsed : %+s", pdata)

	b.log.Log(octo.LOGINFO, "octo.BaseSystem", "ServeBase", "Completed")
	return pdata, err
}

// Serve handles message requests recieved and retuns an error on a message it cant
// handle.
func (b *BaseSystem) Serve(data []byte, tx Stream) error {
	b.log.Log(octo.LOGINFO, "octo.BaseSystem", "Serve", "Started : %+q", data)

	messages, err := b.parser.Parse(data)
	if err != nil {
		b.log.Log(octo.LOGERROR, "octo.BaseSystem", "Serve", "Completed : Parse Error : %+q", err.Error())
		return err
	}

	for _, message := range messages {
		b.log.Log(octo.LOGINFO, "octo.BaseSystem", "Serve", "Serving Message : %+s", message)

		command := strings.ToLower(string(message.Name))
		if handler, ok := b.handlers[command]; ok {
			if err := handler(message, tx); err != nil {
				b.log.Log(octo.LOGERROR, "octo.BaseSystem", "Serve", "Completed : octo.Command[%q] : %+q", command, err.Error())
				return err
			}
		} else {
			b.log.Log(octo.LOGERROR, "octo.BaseSystem", "Serve", "Completed : octo.Command[%q] : %+q", command, consts.ErrRequestUnsearvable)

			return consts.ErrRequestUnsearvable
		}
	}

	b.log.Log(octo.LOGINFO, "octo.BaseSystem", "Serve", "Completed")
	return nil
}
