package jsoni

import (
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/streams/client"
	"github.com/influx6/octo/streams/server"
)

//================================================================================

// CanServe defines a interface which exposes a function to confirm the servibility
// of a giving message.
type CanServe interface {
	CanServe(CommandMessage) bool
}

//================================================================================

// SxServer defines a type which exposes a method to server a CommandMessage for the
// client streaming base.
type SxServer interface {
	CanServe
	Serve(CommandMessage, server.Stream) error
}

// SxServers defines a type for a slice of Server instances.
type SxServers []SxServer

// Serve calls all giving servers in the slice if they are able to provide
// service to the provided messages else return an error.
func (sl SxServers) Serve(cx CommandMessage, tx server.Stream) error {
	for _, service := range sl {
		if !service.CanServe(cx) {
			continue
		}

		return service.Serve(cx, tx)
	}

	return consts.ErrUnservable
}

//================================================================================

// CxServer defines a type which exposes a method to server a CommandMessage for the
// client streaming base.
type CxServer interface {
	CanServe
	Serve(CommandMessage, client.Stream) error
}

// CxServers defines a type for a slice of Server instances.
type CxServers []CxServer

// Serve calls all giving servers in the slice if they are able to provide
// service to the provided messages else return an error.
func (sl CxServers) Serve(cx CommandMessage, tx client.Stream) error {
	for _, service := range sl {
		if !service.CanServe(cx) {
			continue
		}

		return service.Serve(cx, tx)
	}

	return consts.ErrUnservable
}

//================================================================================

// CxConversations defines a central store of servers which can then be used to
// diff out the messages possibly servable by the internal sets of servers and
// forward the rest to the available althernative system.
type CxConversations struct {
	services CxServers
	server   client.Server
}

// NewCxConversations returns a new instance of the CxConversations which implements
// the client.Server interface.
func NewCxConversations(sx client.Server, services ...CxServer) *CxConversations {
	return &CxConversations{
		services: services,
		server:   sx,
	}
}

// Serve handles the provided message received and will appropriately choose the
// proper servers to deliver the data to.
func (c *CxConversations) Serve(data []byte, tx client.Stream) error {

	// Decode message into []CommandMessage type else if failed, then let internal
	// server handle it. Possibly special type.
	messages, err := Parser.Decode(data)
	if err != nil {
		return c.server.Serve(data, tx)
	}

	commands, ok := messages.([]CommandMessage)
	if !ok {
		return consts.ErrUnservable
	}

	var unserved []CommandMessage
	for _, command := range commands {
		if servErr := c.services.Serve(command, tx); servErr != nil {

			// If we recieved an consts.ErrUnservable error then pass to provided althernative
			// server else return err
			if servErr == consts.ErrUnservable {
				unserved = append(unserved, command)
				continue
			}

			return servErr
		}
	}

	if unserved == nil {
		return nil
	}

	// We are possibly dealing with cases of a new set of commands we don't know
	// about hence, encode and let the althernative server handle it
	encoded, err := Parser.Encode(unserved)
	if err != nil {
		return err
	}

	return c.server.Serve(encoded, tx)
}

//================================================================================

// SxConversations defines a central store of servers which can then be used to
// diff out the messages possibly servable by the internal sets of servers and
// forward the rest to the available althernative system.
type SxConversations struct {
	services SxServers
	server   server.Server
}

// NewSxConversations returns a new instance of the SxConversations which implements
// the client.Server interface.
func NewSxConversations(sx server.Server, services ...SxServer) *SxConversations {
	return &SxConversations{
		services: services,
		server:   sx,
	}
}

// Serve handles the provided message received and will appropriately choose the
// proper servers to deliver the data to.
func (c *SxConversations) Serve(data []byte, tx server.Stream) error {

	// Decode message into []CommandMessage type else if failed, then let internal
	// server handle it. Possibly special type.
	messages, err := Parser.Decode(data)
	if err != nil {
		return c.server.Serve(data, tx)
	}

	commands, ok := messages.([]CommandMessage)
	if !ok {
		return consts.ErrUnservable
	}

	var unserved []CommandMessage
	for _, command := range commands {
		if servErr := c.services.Serve(command, tx); servErr != nil {

			// If we recieved an consts.ErrUnservable error then pass to provided althernative
			// server else return err
			if servErr == consts.ErrUnservable {
				unserved = append(unserved, command)
				continue
			}

			return servErr
		}
	}

	if unserved == nil {
		return nil
	}

	// We are possibly dealing with cases of a new set of commands we don't know
	// about hence, encode and let the althernative server handle it
	encoded, err := Parser.Encode(unserved)
	if err != nil {
		return err
	}

	return c.server.Serve(encoded, tx)
}

//================================================================================
