package mock

import (
	"bytes"
	"errors"
	"testing"

	"github.com/influx6/octo"
	"github.com/influx6/octo/parsers/blockparser"
	"github.com/influx6/octo/transmission"
)

// System defines a base system which can be used for testing.
type System struct {
	t    *testing.T
	base octo.AuthCredential
}

// NewSystem returns a new system instance for accessing a octo.System.
func NewSystem(t *testing.T, b octo.AuthCredential) System {
	return System{
		t:    t,
		base: b,
	}
}

// Serve handles the processing of different requests coming from the outside.
func (System) Serve(message []byte, tx transmission.Stream) error {
	cmds, err := blockparser.Blocks.Parse(message)
	if err != nil {
		return err
	}

	for _, command := range cmds {
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
	}

	return errors.New("Invalid Command")
}

// Authenticate authenticates the provided credentials and implements
// the octo.Authenticator interface.
func (s System) Authenticate(cred octo.AuthCredential) error {
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
