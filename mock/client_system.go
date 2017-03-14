package mock

import (
	"bytes"
	"errors"
	"sync"

	"github.com/influx6/octo"
	"github.com/influx6/octo/clients/goclient"
	"github.com/influx6/octo/consts"
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
func (c *ClientSystem) Serve(message interface{}, tx goclient.Stream) error {
	command, ok := message.(octo.Command)
	if !ok {
		return consts.ErrUnsupportedFormat
	}

	switch {
	case bytes.Equal(consts.ContactRequest, command.Name):
		defer c.wg.Done()
		if err := tx.Send([]byte("OK"), true); err != nil {
			return err
		}

		return nil
	default:
		break
	}

	return errors.New("Invalid Command")
}

//==============================================================================================================

// CallbackClientSystem defines a base system which can be used for testing.
type CallbackClientSystem struct {
	base octo.AuthCredential
	wg   sync.WaitGroup
	cb   func(octo.Command, goclient.Stream) error
}

// NewCallbackClientSystem returns a new system instance for accessing a octo.ClientSystem.
func NewCallbackClientSystem(b octo.AuthCredential, cb func(octo.Command, goclient.Stream) error) *CallbackClientSystem {
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
func (c *CallbackClientSystem) Serve(message interface{}, tx goclient.Stream) error {
	command, ok := message.(octo.Command)
	if !ok {
		return consts.ErrUnsupportedFormat
	}

	c.wg.Add(1)
	defer c.wg.Done()

	return c.cb(command, tx)
}
