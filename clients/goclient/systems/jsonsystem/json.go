package jsonsystem

import (
	"bytes"
	"encoding/json"

	"github.com/influx6/octo"
	"github.com/influx6/octo/clients/goclient"
	"github.com/influx6/octo/consts"
)

func sendJSON(tx goclient.Stream, val interface{}, flush bool) error {
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
func AuthHandlers(credential octo.Credentials, authenticator octo.Authenticator) goclient.HandlerMap {
	return goclient.HandlerMap{
		string(consts.AuthResponse): func(m octo.Command, tx goclient.Stream) error {
			var userCredentials octo.AuthCredential

			if err := json.Unmarshal(bytes.Join(m.Data, []byte("")), &userCredentials); err != nil {
				return err
			}

			return authenticator.Authenticate(userCredentials)
		},
		string(consts.AuthRequest): func(m octo.Command, tx goclient.Stream) error {
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
func BaseHandlers() goclient.HandlerMap {
	return goclient.HandlerMap{
		"OK": func(m octo.Command, tx goclient.Stream) error {
			return nil
		},
		"CLOSE": func(m octo.Command, tx goclient.Stream) error {
			defer tx.Close()

			return sendJSON(tx, octo.Command{Name: consts.OK}, true)
		},
		"PONG": func(m octo.Command, tx goclient.Stream) error {
			return sendJSON(tx, octo.Command{Name: consts.PING}, true)
		},
		"PING": func(m octo.Command, tx goclient.Stream) error {
			return sendJSON(tx, octo.Command{Name: consts.PONG}, true)
		},
	}
}
