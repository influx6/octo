package goclient

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/influx6/octo"
	"github.com/influx6/octo/consts"
)

// Handler defines a function type for handle message requests.
type Handler func(octo.Command, Stream) error

// HandlerMap defines a map type for a registry of string keyed
// MessageHandlers.
type HandlerMap map[string]Handler

// String returns a slice of keynames for the giving registry
func (m HandlerMap) String() string {
	var keys []string

	for key := range m {
		keys = append(keys, strconv.Quote(key))
	}

	return fmt.Sprintf("[%s]", strings.Join(keys, ", "))
}

// BaseSystem defines a structure which implements the System
// interface and allows customization of internal handlers.
type BaseSystem struct {
	handlers HandlerMap
	log      octo.Logs
	parser   octo.Parser
}

// NewBaseSystem returns a new instance of a BaseSystem.
func NewBaseSystem(parser octo.Parser, log octo.Logs, handles ...HandlerMap) *BaseSystem {
	var b BaseSystem
	b.log = log
	b.parser = parser
	b.handlers = make(map[string]Handler)

	b.AddAll(handles...)
	return &b
}

// AddAll adds the contents giving set of handler map into the BaseSystem.
func (b *BaseSystem) AddAll(items ...HandlerMap) {
	b.log.Log(octo.LOGINFO, "goclient.BaseSystem", "AddAll", "Started : Items : %+s", items)

	for _, item := range items {
		for tag, handle := range item {
			b.handlers[strings.ToLower(tag)] = handle
		}
	}

	b.log.Log(octo.LOGINFO, "goclient.BaseSystem", "AddAll", "Completed")
}

// Add adds the giving handler using the expected tag and over-writes
// any preious tag found.
func (b *BaseSystem) Add(tag string, handle Handler) {
	b.log.Log(octo.LOGINFO, "goclient.BaseSystem", "Add", "Started : Adding %q")
	b.handlers[strings.ToLower(tag)] = handle
	b.log.Log(octo.LOGINFO, "goclient.BaseSystem", "Add", "Completed")
}

// ServeBase handles message received and returns messages slice it can not handle.
func (b *BaseSystem) ServeBase(data []byte, tx Stream) ([]byte, error) {
	b.log.Log(octo.LOGINFO, "goclient.BaseSystem", "ServeBase", "Started : %+q", data)

	messages, err := b.parser.Parse(data)
	if err != nil {
		b.log.Log(octo.LOGERROR, "goclient.BaseSystem", "ServeBase", "Completed : %+q", err.Error())
		return nil, err
	}

	var unserved []octo.Command

	for _, message := range messages {
		b.log.Log(octo.LOGINFO, "goclient.BaseSystem", "Serve", "Serving Message : %+s", message)

		command := strings.ToLower(string(message.Name))
		if handler, ok := b.handlers[command]; ok {
			if err := handler(message, tx); err != nil {
				b.log.Log(octo.LOGERROR, "goclient.BaseSystem", "ServeBase", "ServeError : %+q", err.Error())
				return nil, err
			}
		} else {
			unserved = append(unserved, message)
		}
	}

	b.log.Log(octo.LOGINFO, "goclient.BaseSystem", "ServeBase", "Unserved : %+s", unserved)

	if unserved == nil {
		b.log.Log(octo.LOGINFO, "goclient.BaseSystem", "ServeBase", "Completed")
		return nil, nil
	}

	pdata, err := b.parser.Unparse(unserved)
	if err != nil {
		b.log.Log(octo.LOGERROR, "goclient.BaseSystem", "ServeBase", "Completed : Unable to unparse remainder")
		return nil, err
	}

	b.log.Log(octo.LOGINFO, "goclient.BaseSystem", "ServeBase", "Unparsed : %+s", pdata)

	b.log.Log(octo.LOGINFO, "goclient.BaseSystem", "ServeBase", "Completed")
	return pdata, err
}

// Serve handles message requests recieved and retuns an error on a message it cant
// handle.
func (b *BaseSystem) Serve(data []byte, tx Stream) error {
	b.log.Log(octo.LOGINFO, "goclient.BaseSystem", "Serve", "Started : %+q", data)

	messages, err := b.parser.Parse(data)
	if err != nil {
		b.log.Log(octo.LOGERROR, "goclient.BaseSystem", "Serve", "Completed : Parse Error : %+q", err.Error())
		return err
	}

	for _, message := range messages {
		b.log.Log(octo.LOGINFO, "goclient.BaseSystem", "Serve", "Serving Message : %+s", message)

		command := strings.ToLower(string(message.Name))
		if handler, ok := b.handlers[command]; ok {
			if err := handler(message, tx); err != nil {
				b.log.Log(octo.LOGERROR, "goclient.BaseSystem", "Serve", "Completed : Command[%q] : %+q", command, err.Error())
				return err
			}
		} else {
			b.log.Log(octo.LOGERROR, "goclient.BaseSystem", "Serve", "Completed : Command[%q] : %+q", command, consts.ErrRequestUnsearvable)
			return consts.ErrRequestUnsearvable
		}
	}

	b.log.Log(octo.LOGINFO, "goclient.BaseSystem", "Serve", "Completed")
	return nil
}
