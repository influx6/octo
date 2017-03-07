package mock

import (
	"bytes"
	"errors"

	"github.com/influx6/octo"
	"github.com/influx6/octo/clients/goclient"
	"github.com/influx6/octo/consts"
)

// ClientSystem defines a base system which can be used for testing.
type ClientSystem struct {
	base octo.AuthCredential
}

// NewClientSystem returns a new system instance for accessing a octo.ClientSystem.
func NewClientSystem(b octo.AuthCredential) ClientSystem {
	return ClientSystem{
		base: b,
	}
}

// Credential returns the credential of the giving ClientSystem.
func (c ClientSystem) Credential() octo.AuthCredential {
	return c.base
}

// Serve handles the processing of different requests coming from the outside.
func (ClientSystem) Serve(message interface{}, tx goclient.Stream) error {
	command, ok := message.(octo.Command)
	if !ok {
		return consts.ErrUnsupported
	}

	switch {
	case bytes.Equal([]byte("PUMP"), command.Name):
		return tx.Send([]byte("RUMP"), true)
	case bytes.Equal([]byte("REX"), command.Name):
		return tx.Send([]byte("DEX"), true)
	case bytes.Equal([]byte("BONG"), command.Name):
		return tx.Send([]byte("BING"), true)
	default:
		break
	}

	return errors.New("Invalid Command")
}
