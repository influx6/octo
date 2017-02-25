package octo

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/influx6/faux/utils"
	"github.com/influx6/octo/consts"
)

// ErrRequestUnsearvable defines the error returned when a request can not
// be handled.
var ErrRequestUnsearvable = errors.New("Request Unserveable")

// MessageHandler defines a function type for handle message requests.
type MessageHandler func(utils.Message, Transmission) error

// BaseSystem defines a structure which implements the System
// interface and allows customization of internal handlers.
type BaseSystem struct {
	handlers      map[string]MessageHandler
	authenticator Authenticator
}

// NewBaseSystem returns a new instance of a BaseSystem.
func NewBaseSystem(authenticator Authenticator, handles ...map[string]MessageHandler) *BaseSystem {
	var b BaseSystem
	b.handlers = make(map[string]MessageHandler)
	b.authenticator = authenticator

	b.AddAll(handles...)
	return &b
}

// AddAll adds the contents giving set of handler map into the BaseSystem.
func (b *BaseSystem) AddAll(items ...map[string]MessageHandler) {
	for _, item := range items {
		for tag, handle := range item {
			b.handlers[strings.ToLower(tag)] = handle
		}
	}
}

// Add adds the giving handler using the expected tag and over-writes
// any preious tag found.
func (b *BaseSystem) Add(tag string, handle MessageHandler) {
	b.handlers[strings.ToLower(tag)] = handle
}

// Authenticate authenticates all credentials and returns true.
func (b *BaseSystem) Authenticate(auth AuthCredential) error {
	if b.authenticator != nil {
		return b.authenticator.Authenticate(auth)
	}

	return nil
}

// ServeBase handles message received and returns messages slice it can not handle.
func (b *BaseSystem) ServeBase(data []byte, tx Transmission) (utils.Messages, error) {
	messages, err := utils.BlockParser.Parse(data)
	if err != nil {
		return nil, err
	}

	var unserved []utils.Message

	for _, message := range messages {
		if handler, ok := b.handlers[string(message.Command)]; ok {
			if err := handler(message, tx); err != nil {
				return nil, err
			}
		} else {
			unserved = append(unserved, message)
		}
	}

	return unserved, nil
}

// Serve handles message requests recieved and retuns an error on a message it cant
// handle.
func (b *BaseSystem) Serve(data []byte, tx Transmission) error {
	messages, err := utils.BlockParser.Parse(data)
	if err != nil {
		return err
	}

	for _, message := range messages {
		command := strings.ToLower(string(message.Command))
		if handler, ok := b.handlers[command]; ok {
			if err := handler(message, tx); err != nil {
				return err
			}
		} else {
			return ErrRequestUnsearvable
		}
	}

	return nil
}

// Clusters defines an interface which returns a slice of Info of it's internal
// registered clusters.
type Clusters interface {
	Clusters() []Info
}

// ClusterHandlers returns a map of handlers suited for cluster requests and
// response cycles.
func ClusterHandlers(master Clusters) map[string]MessageHandler {
	return map[string]MessageHandler{
		string(consts.ClusterRequest): func(m utils.Message, tx Transmission) error {
			var clusterData [][]byte

			for _, cluster := range master.Clusters() {
				parsed, err := json.Marshal(cluster)
				if err != nil {
					return err
				}

				clusterData = append(clusterData, parsed)
			}

			return tx.Send(utils.MakeByteMessage(consts.ClusterResponse, clusterData...), true)
		},
	}
}

// AuthHandlers provides a MessageHandlers providing auth operations/events
// handling.
func AuthHandlers(credential Credentials) map[string]MessageHandler {
	return map[string]MessageHandler{
		string(consts.AuthRequest): func(m utils.Message, tx Transmission) error {
			parsed, err := json.Marshal(credential.Credential())
			if err != nil {
				return err
			}

			return tx.Send(utils.WrapResponseBlock(consts.AuthResponse, parsed), true)
		},
	}
}

// BaseHandlers provides a set of MessageHandlers providing common operations/events
// that can be requested during the operations of a giving request.
func BaseHandlers() map[string]MessageHandler {
	return map[string]MessageHandler{
		"CLOSE": func(m utils.Message, tx Transmission) error {
			defer tx.Close()

			return tx.Send(utils.WrapResponseBlock([]byte("OK"), nil), true)
		},
		"PONG": func(m utils.Message, tx Transmission) error {
			return tx.Send(utils.WrapResponseBlock([]byte("PING"), nil), true)
		},
		"PING": func(m utils.Message, tx Transmission) error {
			return tx.Send(utils.WrapResponseBlock([]byte("PONG"), nil), true)
		},
		string(consts.ClientInfoRequest): func(m utils.Message, tx Transmission) error {
			clientInfo, _ := tx.Info()

			infx, err := json.Marshal(clientInfo)
			if err != nil {
				return err
			}

			return tx.Send(utils.WrapResponseBlock(consts.ClientInfoResponse, infx), true)
		},
		string(consts.InfoRequest): func(m utils.Message, tx Transmission) error {
			_, serverInfo := tx.Info()

			infx, err := json.Marshal(serverInfo)
			if err != nil {
				return err
			}

			return tx.Send(utils.WrapResponseBlock(consts.InfoResponse, infx), true)
		},
	}
}
