package octo

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrRequestUnsearvable defines the error returned when a request can not
// be handled.
var ErrRequestUnsearvable = errors.New("Request Unserveable")

// MessageHandler defines a function type for handle message requests.
type MessageHandler func(Command, Transmission) error

// MessageHandlerMap defines a map type for a registry of string keyed
// MessageHandlers.
type MessageHandlerMap map[string]MessageHandler

// String returns a slice of keynames for the giving registry
func (m MessageHandlerMap) String() string {
	var keys []string

	for key := range m {
		keys = append(keys, strconv.Quote(key))
	}

	return fmt.Sprintf("[%s]", strings.Join(keys, ", "))
}

// BaseSystem defines a structure which implements the System
// interface and allows customization of internal handlers.
type BaseSystem struct {
	handlers      MessageHandlerMap
	authenticator Authenticator
	log           Logs
	parser        Parser
}

// NewBaseSystem returns a new instance of a BaseSystem.
func NewBaseSystem(authenticator Authenticator, parser Parser, log Logs, handles ...MessageHandlerMap) *BaseSystem {
	var b BaseSystem
	b.log = log
	b.parser = parser
	b.handlers = make(map[string]MessageHandler)
	b.authenticator = authenticator

	b.AddAll(handles...)
	return &b
}

// AddAll adds the contents giving set of handler map into the BaseSystem.
func (b *BaseSystem) AddAll(items ...MessageHandlerMap) {
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
func (b *BaseSystem) ServeBase(data []byte, tx Transmission) ([]Command, error) {
	b.log.Log(LOGINFO, "octo.BaseSystem", "ServeBase", "Started : %+q", data)

	messages, err := b.parser.Parse(data)
	if err != nil {
		b.log.Log(LOGERROR, "octo.BaseSystem", "ServeBase", "Completed : %+q", err.Error())
		return nil, err
	}

	var unserved []Command

	for _, message := range messages {
		b.log.Log(LOGINFO, "octo.BaseSystem", "Serve", "Serving Message : %+s", message)

		command := strings.ToLower(string(message.Name))
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

	messages, err := b.parser.Parse(data)
	if err != nil {
		b.log.Log(LOGERROR, "octo.BaseSystem", "Serve", "Completed : Parse Error : %+q", err.Error())
		return err
	}

	for _, message := range messages {
		b.log.Log(LOGINFO, "octo.BaseSystem", "Serve", "Serving Message : %+s", message)

		command := strings.ToLower(string(message.Name))
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
