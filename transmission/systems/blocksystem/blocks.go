package blocksystem

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/influx6/octo"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/parsers/byteutils"
	"github.com/influx6/octo/transmission"
)

// Clusters defines an interface which returns a slice of Contact of it's internal
// registered clusters.
type Clusters interface {
	Clusters() []octo.Contact
}

// ClusterHandler defines an interface that exposes a method to handle clusters
// details.
type ClusterHandler interface {
	HandleClusters([]octo.Contact)
}

// ClusterHandlers returns a map of handlers suited for cluster requests and
// response cycles.
func ClusterHandlers(master Clusters, handler ClusterHandler, sendMessage func([]byte) error) transmission.HandlerMap {
	return transmission.HandlerMap{
		string(consts.ClusterPostOK): func(m octo.Command, tx transmission.Stream) error {
			return tx.Send(byteutils.MakeByteMessage(consts.ClusterRequest, nil), true)
		},
		string(consts.ClusterDistRequest): func(m octo.Command, tx transmission.Stream) error {
			dataLen := len(m.Data)
			if dataLen > 1 || dataLen == 0 {
				return errors.New("Cluster distribution expects at most a single element in the data list of a Message")
			}

			realData := m.Data[0]
			realData = bytes.TrimPrefix(realData, []byte("("))
			realData = bytes.TrimSuffix(realData, []byte(")"))

			if sendMessage == nil {
				return errors.New("Cluster distribution expects a message function to be provided")
			}

			return sendMessage(realData)
		},
		string(consts.ClusterRequest): func(m octo.Command, tx transmission.Stream) error {
			var clusterData [][]byte

			for _, cluster := range master.Clusters() {
				parsed, err := json.Marshal(cluster)
				if err != nil {
					return err
				}

				clusterData = append(clusterData, parsed)
			}

			return tx.Send(byteutils.MakeByteMessage(consts.ClusterResponse, clusterData...), true)
		},
		string(consts.ClusterResponse): func(m octo.Command, tx transmission.Stream) error {
			var clusters []octo.Contact
			_, serverContact := tx.Contact()

			for _, message := range m.Data {
				var info octo.Contact
				if err := json.Unmarshal(message, &info); err != nil {
					return err
				}

				// If we are matching the same server then skip.
				if info.UUID == serverContact.SUUID {
					continue
				}

				clusters = append(clusters, info)
			}

			handler.HandleClusters(clusters)

			return tx.Send(byteutils.MakeByteMessage(consts.OK, nil), true)
		},
	}
}

// AuthHandlers provides a Handlers providing auth operations/events
// handling.
func AuthHandlers(credential octo.Credentials) transmission.HandlerMap {
	return transmission.HandlerMap{
		string(consts.AuthRequest): func(m octo.Command, tx transmission.Stream) error {
			parsed, err := json.Marshal(credential.Credential())
			if err != nil {
				return err
			}

			return tx.Send(byteutils.WrapResponseBlock(consts.AuthResponse, parsed), true)
		},
	}
}

// BaseHandlers provides a set of Handlers providing common operations/events
// that can be requested during the operations of a giving request.
func BaseHandlers() transmission.HandlerMap {
	return transmission.HandlerMap{
		"OK": func(m octo.Command, tx transmission.Stream) error {
			return nil
		},
		"CLOSE": func(m octo.Command, tx transmission.Stream) error {
			defer tx.Close()

			return tx.Send(byteutils.WrapResponseBlock([]byte("OK"), nil), true)
		},
		"PONG": func(m octo.Command, tx transmission.Stream) error {
			return tx.Send(byteutils.WrapResponseBlock([]byte("PING"), nil), true)
		},
		"PING": func(m octo.Command, tx transmission.Stream) error {
			return tx.Send(byteutils.WrapResponseBlock([]byte("PONG"), nil), true)
		},
		string(consts.ClientContactRequest): func(m octo.Command, tx transmission.Stream) error {
			clientContact, _ := tx.Contact()

			infx, err := json.Marshal(clientContact)
			if err != nil {
				return err
			}

			return tx.Send(byteutils.WrapResponseBlock(consts.ClientContactResponse, infx), true)
		},
		string(consts.ContactRequest): func(m octo.Command, tx transmission.Stream) error {
			_, serverContact := tx.Contact()

			infx, err := json.Marshal(serverContact)
			if err != nil {
				return err
			}

			return tx.Send(byteutils.WrapResponseBlock(consts.ContactResponse, infx), true)
		},
	}
}
