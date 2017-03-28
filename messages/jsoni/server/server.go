package server

import (
	"github.com/influx6/octo"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/messages/jsoni"
	"github.com/influx6/octo/streams/server"
)

// AuthServer defines a new struct which implements the several message
// handling for the jsoni message types.
type AuthServer struct {
	octo.Credentials
}

// Serve handles the response requested by the giving jsoni.CommandMessage returning
// then needed response.
func (c AuthServer) Serve(cmd jsoni.CommandMessage, tx server.Stream) error {
	return sendJSON(tx, jsoni.CommandMessage{
		Name: string(consts.AuthResponse),
		Data: c.Credential(),
	}, true)
}

// CanServe returns true/false if the giving element is able to server the
// provided message.Command.
func (c AuthServer) CanServe(cmd jsoni.CommandMessage) bool {
	return string(consts.AuthRequest) == cmd.Name
}

//================================================================================

// ConversationServer defines a new struct which implements the several message
// handling for the jsoni message types.
type ConversationServer struct {
}

// Serve handles the response requested by the giving jsoni.CommandMessage returning
// then needed response.
func (c ConversationServer) Serve(cmd jsoni.CommandMessage, tx server.Stream) error {
	switch cmd.Name {
	case string(consts.CLOSE):
		if err := sendJSON(tx, jsoni.CommandMessage{Name: string(consts.OK)}, true); err != nil {
			return err
		}

		return tx.Close()

	case string(consts.PONG):
		return sendJSON(tx, jsoni.CommandMessage{Name: string(consts.PING)}, true)

	case string(consts.PING):
		return sendJSON(tx, jsoni.CommandMessage{Name: string(consts.PONG)}, true)

	case string(consts.OK):
		return nil

	default:
		return consts.ErrUnservable
	}
}

// CanServe returns true/false if the giving element is able to server the
// provided message.Command.
func (c ConversationServer) CanServe(cmd jsoni.CommandMessage) bool {
	switch cmd.Name {
	case string(consts.CLOSE):
		return true
	case string(consts.PONG):
		return true
	case string(consts.PING):
		return true
	case string(consts.OK):
		return true
	default:
		return false
	}
}

//================================================================================

// ContactServer defines a new struct which implements the processing of contact
// requests recieved through CommandMessages.
type ContactServer struct {
}

// Serve handles the response requested by the giving jsoni.CommandMessage returning
// then needed response.
func (c ContactServer) Serve(cmd jsoni.CommandMessage, tx server.Stream) error {
	switch cmd.Name {
	case string(consts.ClientContactRequest):
		clientContact, _ := tx.Contact()
		return sendJSON(tx, jsoni.CommandMessage{
			Name: string(consts.ClientContactResponse),
			Data: clientContact,
		}, true)

	case string(consts.ContactRequest):
		_, serverContact := tx.Contact()

		return sendJSON(tx, jsoni.CommandMessage{
			Name: string(consts.ContactResponse),
			Data: serverContact,
		}, true)
	default:
		return consts.ErrUnservable
	}
}

// CanServe returns true/false if the giving element is able to server the
// provided message.Command.
func (c ContactServer) CanServe(cmd jsoni.CommandMessage) bool {
	switch cmd.Name {
	case string(consts.ClientContactRequest):
		return true
	case string(consts.ContactRequest):
		return true
	default:
		return false
	}
}

//================================================================================

func sendJSON(tx server.Stream, val interface{}, flush bool) error {
	data, err := jsoni.Parser.Encode(val)
	if err != nil {
		return err
	}

	return tx.Send(data, flush)
}
