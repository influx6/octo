package client

import (
	"encoding/json"

	"github.com/influx6/octo"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/messages/commando"
	"github.com/influx6/octo/streams/client"
)

// AuthServer defines a new struct which implements the several message
// handling for the commando message types.
type AuthServer struct {
	octo.Credentials
}

// Serve handles the response requested by the giving commando.CommandMessage returning
// then needed response.
func (c AuthServer) Serve(cmd commando.CommandMessage, tx client.Stream) error {
	parsed, err := json.Marshal(c.Credential())
	if err != nil {
		return err
	}

	res := commando.WrapResponseBlock(consts.AuthResponse, parsed)
	return tx.Send(res, true)
}

// CanServe returns true/false if the giving element is able to server the
// provided message.Command.
func (c AuthServer) CanServe(cmd commando.CommandMessage) bool {
	switch cmd.Name {
	case string(consts.AuthRequest):
		return true
	default:
		return false
	}
}

//================================================================================

// ConversationServer defines a new struct which implements the several message
// handling for the commando message types.
type ConversationServer struct {
}

// Serve handles the response requested by the giving commando.CommandMessage returning
// then needed response.
func (c ConversationServer) Serve(cmd commando.CommandMessage, tx client.Stream) error {
	switch cmd.Name {
	case string(consts.CLOSE):
		if err := tx.Send(commando.WrapResponseBlock(consts.OK, nil), true); err != nil {
			return err
		}

		return tx.Close()

	case string(consts.PONG):
		return tx.Send(commando.WrapResponseBlock(consts.PING, nil), true)

	case string(consts.PING):
		return tx.Send(commando.WrapResponseBlock(consts.PONG, nil), true)

	case string(consts.OK):
		return nil

	default:
		return consts.ErrUnservable
	}
}

// CanServe returns true/false if the giving element is able to server the
// provided message.Command.
func (c ConversationServer) CanServe(cmd commando.CommandMessage) bool {
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
