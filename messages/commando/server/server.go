package server

import (
	"bytes"
	"encoding/json"

	"github.com/influx6/octo"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/messages/commando"
	"github.com/influx6/octo/streams/server"
)

// Clusters defines an interface which returns a slice of Contact of it's internal
// registered clusters.
type Clusters interface {
	Clusters() []octo.Contact
}

// ClientsMessageDelivery defines an interface which delivers a message to an
// underline list of clusters.
type ClientsMessageDelivery interface {
	DeliverToClients([]byte) error
}

// ClusterHandler defines an interface that exposes a method to handle clusters
// details.
type ClusterHandler interface {
	HandleClusters([]octo.Contact)
}

// ClusterServer defines a server which handles servicing cluster based messages
// received.
type ClusterServer struct {
	Clusters
	ClusterHandler
	ClientsMessageDelivery
}

// Serve handles the response requested by the giving commando.CommandMessage returning
// then needed response.
func (c ClusterServer) Serve(cmd commando.CommandMessage, tx server.Stream) error {
	switch cmd.Name {
	case string(consts.ClusterPostOK):
		return tx.Send(commando.MakeByteMessage(consts.ClusterRequest, nil), true)

	case string(consts.ClusterDistRequest):
		if len(cmd.Data) == 0 {
			return consts.ErrEmptyMessage
		}

		realData := cmd.Data[0]
		realData = bytes.TrimPrefix(realData, []byte("("))
		realData = bytes.TrimSuffix(realData, []byte(")"))

		return c.DeliverToClients(realData)

	case string(consts.ClusterRequest):
		var clusterData [][]byte

		for _, cluster := range c.Clusters.Clusters() {
			parsed, err := json.Marshal(cluster)
			if err != nil {
				return err
			}

			clusterData = append(clusterData, parsed)
		}

		return tx.Send(commando.MakeByteMessage(consts.ClusterResponse, clusterData...), true)

	case string(consts.ClusterResponse):
		var clusters []octo.Contact
		_, serverContact := tx.Contact()

		for _, message := range cmd.Data {
			var info octo.Contact
			if err := json.Unmarshal(message, &info); err != nil {
				return err
			}

			// If we are matching the same server then skip.
			if info.UUID == serverContact.SUUID {
				continue
			}

			clusters = append(clusters, info)
		}

		c.ClusterHandler.HandleClusters(clusters)

		return tx.Send(commando.MakeByteMessage(consts.OK, nil), true)

	default:
		return consts.ErrUnservable
	}
}

// CanServe returns true/false if the giving element is able to server the
// provided message.Command.
func (c ClusterServer) CanServe(cmd commando.CommandMessage) bool {
	switch cmd.Name {
	case string(consts.ClusterPostOK):
		return true
	case string(consts.ClusterDistRequest):
		return true

	case string(consts.ClusterRequest):
		return true

	case string(consts.ClusterResponse):
		return true

	default:
		return false
	}
}

//================================================================================

// AuthServer defines a new struct which implements the several message
// handling for the commando message types.
type AuthServer struct {
	octo.Credentials
}

// Serve handles the response requested by the giving commando.CommandMessage returning
// then needed response.
func (c AuthServer) Serve(cmd commando.CommandMessage, tx server.Stream) error {
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

// CloseServer defines a Server which provides CLOSE message handler.
type CloseServer struct{}

// Serve handles the response requested by the giving commando.CommandMessage returning
// then needed response.
func (c CloseServer) Serve(cmd commando.CommandMessage, tx server.Stream) error {
	switch cmd.Name {
	case string(consts.CLOSE):
		if err := tx.Send(commando.WrapResponseBlock(consts.OK, nil), true); err != nil {
			return err
		}

		return tx.Close()

	default:
		return consts.ErrUnservable
	}
}

// CanServe returns true/false if the giving element is able to server the
// provided message.Command.
func (c CloseServer) CanServe(cmd commando.CommandMessage) bool {
	switch cmd.Name {
	case string(consts.CLOSE):
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
func (c ConversationServer) Serve(cmd commando.CommandMessage, tx server.Stream) error {
	switch cmd.Name {
	case string(consts.PONG):

		// If supported by the Streamer, then notify of Pong.
		if no, ok := tx.(octo.PingPongs); ok {
			no.NotifyPong()
		}

		return tx.Send(commando.WrapResponseBlock(consts.PING, nil), true)

	case string(consts.PING):

		// If supported by the Streamer, then notify of Ping.
		if no, ok := tx.(octo.PingPongs); ok {
			no.NotifyPing()
		}

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

// Serve handles the response requested by the giving commando.CommandMessage returning
// then needed response.
func (c ContactServer) Serve(cmd commando.CommandMessage, tx server.Stream) error {
	switch cmd.Name {
	case string(consts.ClientContactRequest):
		clientContact, _ := tx.Contact()

		infx, err := json.Marshal(clientContact)
		if err != nil {
			return err
		}

		return tx.Send(commando.WrapResponseBlock(consts.ClientContactResponse, infx), true)

	case string(consts.ContactRequest):
		_, serverContact := tx.Contact()

		infx, err := json.Marshal(serverContact)
		if err != nil {
			return err
		}

		return tx.Send(commando.WrapResponseBlock(consts.ContactResponse, infx), true)

	default:
		return consts.ErrUnservable
	}
}

// CanServe returns true/false if the giving element is able to server the
// provided message.Command.
func (c ContactServer) CanServe(cmd commando.CommandMessage) bool {
	switch cmd.Name {
	case string(consts.ClientContactRequest):
		return true
	case string(consts.ContactRequest):
		return true
	default:
		return false
	}
}
