package jsonsystem

import (
	"bytes"
	"encoding/json"

	"github.com/influx6/octo"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/transmission"
)

func sendJSON(tx transmission.Stream, val interface{}, flush bool) error {
	var data []byte
	var err error

	switch item := val.(type) {
	case []byte:
		data = item
	default:
		data, err = json.Marshal(val)
		if err != nil {
			return err
		}
	}

	return tx.Send(data, flush)
}

// AuthHandlers provides a MessageHandlers providing auth operations/events
// handling.
func AuthHandlers(credential octo.Credentials, authenticator octo.Authenticator) transmission.HandlerMap {
	return transmission.HandlerMap{
		string(consts.AuthResponse): func(m octo.Command, tx transmission.Stream) error {
			var userCredentials octo.AuthCredential

			if err := json.Unmarshal(bytes.Join(m.Data, []byte("")), &userCredentials); err != nil {
				return err
			}

			return authenticator.Authenticate(userCredentials)
		},
		string(consts.AuthRequest): func(m octo.Command, tx transmission.Stream) error {
			parsed, err := json.Marshal(credential.Credential())
			if err != nil {
				return err
			}

			return sendJSON(tx, octo.Command{Name: consts.AuthResponse, Data: [][]byte{parsed}}, true)
		},
	}
}

// BaseHandlers provides a set of MessageHandlers providing common operations/events
// that can be requested during the operations of a giving request.
func BaseHandlers() transmission.HandlerMap {
	return transmission.HandlerMap{
		"OK": func(m octo.Command, tx transmission.Stream) error {
			return nil
		},
		"CLOSE": func(m octo.Command, tx transmission.Stream) error {
			defer tx.Close()

			return sendJSON(tx, octo.Command{Name: consts.OK}, true)
		},
		"PONG": func(m octo.Command, tx transmission.Stream) error {
			return sendJSON(tx, octo.Command{Name: consts.PING}, true)
		},
		"PING": func(m octo.Command, tx transmission.Stream) error {
			return sendJSON(tx, octo.Command{Name: consts.PONG}, true)
		},
		string(consts.ClientContactRequest): func(m octo.Command, tx transmission.Stream) error {
			clientContact, _ := tx.Contact()

			infx, err := json.Marshal(clientContact)
			if err != nil {
				return err
			}

			return sendJSON(tx, octo.Command{Name: consts.ClientContactResponse, Data: [][]byte{infx}}, true)
		},
		string(consts.ContactRequest): func(m octo.Command, tx transmission.Stream) error {
			_, serverContact := tx.Contact()

			infx, err := json.Marshal(serverContact)
			if err != nil {
				return err
			}

			return sendJSON(tx, octo.Command{Name: consts.ContactResponse, Data: [][]byte{infx}}, true)
		},
	}
}
