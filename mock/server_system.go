package mock

import (
	"bytes"
	"errors"

	"github.com/influx6/octo"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/parsers/blockparser"
	"github.com/influx6/octo/transmission"
)

// ServerSystem defines a base system which can be used for testing.
type ServerSystem struct {
	base octo.AuthCredential
}

// NewServerSystem returns a new system instance for accessing a octo.ServerSystem.
func NewServerSystem(b octo.AuthCredential) *ServerSystem {
	return &ServerSystem{
		base: b,
	}
}

// Serve handles the processing of different requests coming from the outside.
func (s *ServerSystem) Serve(message []byte, tx transmission.Stream) error {
	cmds, err := blockparser.Blocks.Parse(message)
	if err != nil {
		return err
	}

	for _, command := range cmds {
		switch {
		case bytes.Equal(consts.ContactRequest, command.Name):
			return tx.Send([]byte("OK"), true)
		case bytes.Equal([]byte("PUMP"), command.Name):
			return tx.Send([]byte("RUMP"), true)
		case bytes.Equal([]byte("REX"), command.Name):
			return tx.Send([]byte("DEX"), true)
		case bytes.Equal([]byte("BONG"), command.Name):
			return tx.Send([]byte("BING"), true)
		default:
			break
		}
	}

	return errors.New("Invalid Command")
}

// Authenticate authenticates the provided credentials and implements
// the octo.Authenticator interface.
func (s *ServerSystem) Authenticate(cred octo.AuthCredential) error {
	if cred.Scheme != s.base.Scheme {
		return errors.New("Scheme does not match")
	}

	if cred.Key != s.base.Key {
		return errors.New("Key does not match")
	}

	if cred.Token != s.base.Token {
		return errors.New("Token does not match")
	}

	if !bytes.Equal(s.base.Data, cred.Data) {
		return errors.New("Data  does not match")
	}

	return nil
}
