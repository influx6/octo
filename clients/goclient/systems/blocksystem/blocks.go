package blocksystem

import (
	"encoding/json"

	"github.com/influx6/octo"
	"github.com/influx6/octo/clients/goclient"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/parsers/byteutils"
)

// AuthHandlers provides a MessageHandlers providing auth operations/events
// handling.
func AuthHandlers(credential octo.Credentials) goclient.HandlerMap {
	return goclient.HandlerMap{
		string(consts.AuthRequest): func(m octo.Command, tx goclient.Stream) error {
			parsed, err := json.Marshal(credential.Credential())
			if err != nil {
				return err
			}

			return tx.Send(byteutils.WrapResponseBlock(consts.AuthResponse, parsed), true)
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

			return tx.Send(byteutils.WrapResponseBlock([]byte("OK"), nil), true)
		},
		"PONG": func(m octo.Command, tx goclient.Stream) error {
			return tx.Send(byteutils.WrapResponseBlock([]byte("PING"), nil), true)
		},
		"PING": func(m octo.Command, tx goclient.Stream) error {
			return tx.Send(byteutils.WrapResponseBlock([]byte("PONG"), nil), true)
		},
	}
}
