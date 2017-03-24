package mock

import (
	"bytes"
	"encoding/json"
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
		switch command.Name {
		case string(consts.ContactRequest):
			return tx.Send([]byte("OK"), true)
		case "PUMP":
			return tx.Send([]byte("RUMP"), true)
		case ("REX"):
			return tx.Send([]byte("DEX"), true)
		case "BONG":
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

//================================================================================================

// ServerCommandSystem defines a base system which can be used for testing.
type ServerCommandSystem struct {
	base octo.AuthCredential
}

// NewServerCommandSystem returns a new system instance for accessing a octo.ServerSystem.
func NewServerCommandSystem(b octo.AuthCredential) *ServerCommandSystem {
	return &ServerCommandSystem{
		base: b,
	}
}

// Serve handles the processing of different requests coming from the outside.
func (s *ServerCommandSystem) Serve(message []byte, tx transmission.Stream) error {
	var commands []octo.Command

	if err := json.Unmarshal(message, &commands); err != nil {
		var single octo.Command

		if errx := json.Unmarshal(message, &single); errx != nil {
			return errx
		}

		commands = append(commands, single)
	}

	for _, command := range commands {
		switch command.Name {
		case string(consts.ContactRequest):
			cmdData, err := json.Marshal(octo.Command{Name: ("OK")})
			if err != nil {
				return err
			}

			return tx.Send(cmdData, true)
		case "PUMP":
			cmdData, err := json.Marshal(octo.Command{Name: ("RUMP")})
			if err != nil {
				return err
			}

			return tx.Send(cmdData, true)
		case "REX":
			cmdData, err := json.Marshal(octo.Command{Name: ("DEX")})
			if err != nil {
				return err
			}

			return tx.Send(cmdData, true)

		case "BONG":
			cmdData, err := json.Marshal(octo.Command{Name: ("BING")})
			if err != nil {
				return err
			}

			return tx.Send(cmdData, true)

		default:
			break
		}
	}

	return errors.New("Invalid Command")
}

// Authenticate authenticates the provided credentials and implements
// the octo.Authenticator interface.
func (s *ServerCommandSystem) Authenticate(cred octo.AuthCredential) error {
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
