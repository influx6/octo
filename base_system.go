package octo

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/influx6/faux/utils"
	"github.com/influx6/octo/consts"
)

// ErrRequestUnsearvable defines the error returned when a request can not
// be handled.
var ErrRequestUnsearvable = errors.New("Request Unserveable")

// MessageHandler defines a function type for handle message requests.
type MessageHandler func(utils.Message, Transmission) error

// MessageHandlerRegistry defines a map type for a registry of string keyed
// MessageHandlers.
type MessageHandlerRegistry map[string]MessageHandler

// String returns a slice of keynames for the giving registry
func (m MessageHandlerRegistry) String() string {
	var keys []string

	for key := range m {
		keys = append(keys, strconv.Quote(key))
	}

	return fmt.Sprintf("[%s]", strings.Join(keys, ", "))
}

// BaseSystem defines a structure which implements the System
// interface and allows customization of internal handlers.
type BaseSystem struct {
	handlers      MessageHandlerRegistry
	authenticator Authenticator
	log           Logs
}

// NewBaseSystem returns a new instance of a BaseSystem.
func NewBaseSystem(authenticator Authenticator, log Logs, handles ...MessageHandlerRegistry) *BaseSystem {
	var b BaseSystem
	b.log = log
	b.handlers = make(map[string]MessageHandler)
	b.authenticator = authenticator

	b.AddAll(handles...)
	return &b
}

// AddAll adds the contents giving set of handler map into the BaseSystem.
func (b *BaseSystem) AddAll(items ...MessageHandlerRegistry) {
	b.log.Log(LOGINFO, "octo.BaseSystem", "AddAll", "Started : Items : %+s", items)

	for _, item := range items {
		for tag, handle := range item {
			b.handlers[strings.ToLower(tag)] = handle
		}
	}

	b.log.Log(LOGINFO, "octo.BaseSystem", "AddAll", "Completed")
}

// Add adds the giving handler using the expected tag and over-writes
// any preious tag found.
func (b *BaseSystem) Add(tag string, handle MessageHandler) {
	b.log.Log(LOGINFO, "octo.BaseSystem", "Add", "Started : Adding %q")
	b.handlers[strings.ToLower(tag)] = handle
	b.log.Log(LOGINFO, "octo.BaseSystem", "Add", "Completed")
}

// Authenticate authenticates all credentials and returns true.
func (b *BaseSystem) Authenticate(auth AuthCredential) error {
	b.log.Log(LOGINFO, "octo.BaseSystem", "Authenticate", "Started : %#v", auth)
	if b.authenticator != nil {
		if err := b.authenticator.Authenticate(auth); err != nil {
			b.log.Log(LOGERROR, "octo.BaseSystem", "Authenticate", "Completed : %+q", err.Error())
			return err
		}
	}

	b.log.Log(LOGINFO, "octo.BaseSystem", "Authenticate", "Completed")
	return nil
}

// ServeBase handles message received and returns messages slice it can not handle.
func (b *BaseSystem) ServeBase(data []byte, tx Transmission) (utils.Messages, error) {
	b.log.Log(LOGINFO, "octo.BaseSystem", "ServeBase", "Started : %+q", data)

	messages, err := utils.BlockParser.Parse(data)
	if err != nil {
		b.log.Log(LOGERROR, "octo.BaseSystem", "ServeBase", "Completed : %+q", err.Error())
		return nil, err
	}

	var unserved []utils.Message

	for _, message := range messages {
		b.log.Log(LOGINFO, "octo.BaseSystem", "Serve", "Serving Message : %+s", message)

		command := strings.ToLower(string(message.Command))
		if handler, ok := b.handlers[command]; ok {
			if err := handler(message, tx); err != nil {
				b.log.Log(LOGERROR, "octo.BaseSystem", "ServeBase", "ServeError : %+q", err.Error())
				return nil, err
			}
		} else {
			unserved = append(unserved, message)
		}
	}

	b.log.Log(LOGINFO, "octo.BaseSystem", "ServeBase", "Unserved : %+s", unserved)

	b.log.Log(LOGINFO, "octo.BaseSystem", "ServeBase", "Completed")
	return unserved, nil
}

// Serve handles message requests recieved and retuns an error on a message it cant
// handle.
func (b *BaseSystem) Serve(data []byte, tx Transmission) error {
	b.log.Log(LOGINFO, "octo.BaseSystem", "Serve", "Started : %+q", data)

	messages, err := utils.BlockParser.Parse(data)
	if err != nil {
		b.log.Log(LOGERROR, "octo.BaseSystem", "Serve", "Completed : Parse Error : %+q", err.Error())
		return err
	}

	for _, message := range messages {
		b.log.Log(LOGINFO, "octo.BaseSystem", "Serve", "Serving Message : %+s", message)

		command := strings.ToLower(string(message.Command))
		if handler, ok := b.handlers[command]; ok {
			if err := handler(message, tx); err != nil {
				b.log.Log(LOGERROR, "octo.BaseSystem", "Serve", "Completed : Command[%q] : %+q", command, err.Error())
				return err
			}
		} else {
			b.log.Log(LOGERROR, "octo.BaseSystem", "Serve", "Completed : Command[%q] : %+q", command, ErrRequestUnsearvable.Error())
			return ErrRequestUnsearvable
		}
	}

	b.log.Log(LOGINFO, "octo.BaseSystem", "Serve", "Completed")
	return nil
}

// Clusters defines an interface which returns a slice of Info of it's internal
// registered clusters.
type Clusters interface {
	Clusters() []Info
}

// ClusterHandler defines an interface that exposes a method to handle clusters
// details.
type ClusterHandler interface {
	HandleClusters([]Info)
}

// ClusterHandlers returns a map of handlers suited for cluster requests and
// response cycles.
func ClusterHandlers(master Clusters, handler ClusterHandler, sendMessage func([]byte) error) map[string]MessageHandler {
	return map[string]MessageHandler{
		string(consts.ClusterPostOK): func(m utils.Message, tx Transmission) error {
			return tx.Send(utils.MakeByteMessage(consts.ClusterRequest, nil), true)
		},
		string(consts.ClusterDistRequest): func(m utils.Message, tx Transmission) error {
			fmt.Printf("Received New Distribution: %+q\n", m)
			dataLen := len(m.Data)
			if dataLen > 1 || dataLen == 0 {
				return errors.New("Cluster distribution expects at most a single element in the data list of a Message")
			}

			realData := m.Data[0]
			realData = bytes.TrimPrefix(realData, []byte("("))
			realData = bytes.TrimSuffix(realData, []byte(")"))

			fmt.Printf("Received New Distribution: RLDATA : %+q\n", m)
			if sendMessage == nil {
				return errors.New("Cluster distribution expects a message function to be provided")
			}

			return sendMessage(realData)
		},
		string(consts.ClusterRequest): func(m utils.Message, tx Transmission) error {
			var clusterData [][]byte

			for _, cluster := range master.Clusters() {
				parsed, err := json.Marshal(cluster)
				if err != nil {
					return err
				}

				clusterData = append(clusterData, parsed)
			}

			return tx.Send(utils.MakeByteMessage(consts.ClusterResponse, clusterData...), true)
		},
		string(consts.ClusterResponse): func(m utils.Message, tx Transmission) error {
			var clusters []Info
			_, serverInfo := tx.Info()

			for _, message := range m.Data {
				var info Info
				if err := json.Unmarshal(message, &info); err != nil {
					return err
				}

				// If we are matching the same server then skip.
				if info.UUID == serverInfo.SUUID {
					continue
				}

				clusters = append(clusters, info)
			}

			handler.HandleClusters(clusters)

			return tx.Send(utils.MakeByteMessage(consts.OK, nil), true)
		},
	}
}

// AuthHandlers provides a MessageHandlers providing auth operations/events
// handling.
func AuthHandlers(credential Credentials) map[string]MessageHandler {
	return map[string]MessageHandler{
		string(consts.AuthRequest): func(m utils.Message, tx Transmission) error {
			parsed, err := json.Marshal(credential.Credential())
			if err != nil {
				return err
			}

			return tx.Send(utils.WrapResponseBlock(consts.AuthResponse, parsed), true)
		},
	}
}

// BaseHandlers provides a set of MessageHandlers providing common operations/events
// that can be requested during the operations of a giving request.
func BaseHandlers() map[string]MessageHandler {
	return map[string]MessageHandler{
		"OK": func(m utils.Message, tx Transmission) error {
			return nil
		},
		"CLOSE": func(m utils.Message, tx Transmission) error {
			defer tx.Close()

			return tx.Send(utils.WrapResponseBlock([]byte("OK"), nil), true)
		},
		"PONG": func(m utils.Message, tx Transmission) error {
			return tx.Send(utils.WrapResponseBlock([]byte("PING"), nil), true)
		},
		"PING": func(m utils.Message, tx Transmission) error {
			return tx.Send(utils.WrapResponseBlock([]byte("PONG"), nil), true)
		},
		string(consts.ClientInfoRequest): func(m utils.Message, tx Transmission) error {
			clientInfo, _ := tx.Info()

			infx, err := json.Marshal(clientInfo)
			if err != nil {
				return err
			}

			return tx.Send(utils.WrapResponseBlock(consts.ClientInfoResponse, infx), true)
		},
		string(consts.InfoRequest): func(m utils.Message, tx Transmission) error {
			_, serverInfo := tx.Info()

			infx, err := json.Marshal(serverInfo)
			if err != nil {
				return err
			}

			return tx.Send(utils.WrapResponseBlock(consts.InfoResponse, infx), true)
		},
	}
}
