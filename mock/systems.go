package mock

import (
	"errors"

	"github.com/influx6/faux/utils"
	"github.com/influx6/octo"
)

// MessageHandler defines a function type for handle message requests.
type MessageHandler func(utils.Message, octo.Transmission) error

// MSystem defines a structure to implement the octo.System interface
// for testing.
type MSystem struct {
	handlers map[string]MessageHandler
}

// NewMSystem returns a new instance of a MSystem.
func NewMSystem(handlers map[string]MessageHandler) *MSystem {
	return &MSystem{handlers: handlers}
}

// Authenticate authenticates all credentials and returns true.
func (m *MSystem) Authenticate(auth octo.AuthCredential) error {
	return nil
}

// ErrRequestUnsearvable defines the error returned when a request can not
// be handled.
var ErrRequestUnsearvable = errors.New("Request Unserveable")

// Serve handles message requests from a giving server.
func (m *MSystem) Serve(data []byte, tx octo.Transmission) error {
	messages, err := utils.BlockParser.Parse(data)
	if err != nil {
		return err
	}

	for _, message := range messages {
		if handler, ok := m.handlers[string(message.Command)]; ok {
			if err := handler(message, tx); err != nil {
				return err
			}
		} else {
			return ErrRequestUnsearvable
		}
	}

	return nil
}
