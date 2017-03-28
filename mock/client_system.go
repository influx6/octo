package mock

import (
	"errors"
	"sync"

	"github.com/influx6/octo"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/messages/jsoni"
	"github.com/influx6/octo/streams/client"
)

// ClientSystem defines a base system which can be used for testing.
type ClientSystem struct {
	base octo.AuthCredential
	wg   sync.WaitGroup
}

// NewClientSystem returns a new system instance for accessing a octo.ClientSystem.
func NewClientSystem(b octo.AuthCredential) *ClientSystem {
	return &ClientSystem{
		base: b,
	}
}

// Wait calls the underline ClientSystem wait call.
func (c *ClientSystem) Wait() {
	c.wg.Wait()
}

// Credential returns the credential of the giving ClientSystem.
func (c *ClientSystem) Credential() octo.AuthCredential {
	return c.base
}

// Serve handles the processing of different requests coming from the outside.
func (c *ClientSystem) Serve(message interface{}, tx client.Stream) error {
	commands, ok := message.([]jsoni.CommandMessage)
	if !ok {
		return consts.ErrUnsupportedFormat
	}

	for _, command := range commands {
		switch command.Name {
		case string(consts.ContactRequest):
			defer c.wg.Done()
			if err := tx.Send([]byte("OK"), true); err != nil {
				return err
			}

			return nil
		default:
			return errors.New("Invalid Command")
		}
	}

	return nil
}

//==============================================================================================================

// CallbackClientSystem defines a base system which can be used for testing.
type CallbackClientSystem struct {
	base octo.AuthCredential
	wg   sync.WaitGroup
	cb   func(jsoni.CommandMessage, client.Stream) error
}

// NewCallbackClientSystem returns a new system instance for accessing a octo.ClientSystem.
func NewCallbackClientSystem(b octo.AuthCredential, cb func(jsoni.CommandMessage, client.Stream) error) *CallbackClientSystem {
	return &CallbackClientSystem{
		base: b,
		cb:   cb,
	}
}

// Wait calls the underline ClientSystem wait call.
func (c *CallbackClientSystem) Wait() {
	c.wg.Wait()
}

// Credential returns the credential of the giving ClientSystem.
func (c *CallbackClientSystem) Credential() octo.AuthCredential {
	return c.base
}

// Serve handles the processing of different requests coming from the outside.
func (c *CallbackClientSystem) Serve(message interface{}, tx client.Stream) error {
	commands, ok := message.([]jsoni.CommandMessage)
	if !ok {
		return consts.ErrUnsupportedFormat
	}

	c.wg.Add(1)
	defer c.wg.Done()

	for _, command := range commands {
		if err := c.cb(command, tx); err != nil {
			return err
		}
	}

	return nil
}
