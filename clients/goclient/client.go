package goclient

import "github.com/influx6/octo"

// MessageEncoding defines an interface which exposes the ability to encode and
// decode data recieved from server.
type MessageEncoding interface {
	Encode(interface{}) ([]byte, error)
	Decode([]byte) (interface{}, error)
}

// Stream defines an interface that provides a means of delivery giving messages
// to another encapsulated endpoint.
type Stream interface {
	Close() error
	Send(interface{}, bool) error
}

// Connection defines a interface which provides a means by which connections
// are made and processed.
type Connection interface {
	Stream
	Listen(System, MessageEncoding) error
	Register(octo.StateHandlerType, interface{})
}

// System defines a interface which exposes a method to handle/process a giving
// byte slice and recieves the core pipe for response.
type System interface {
	octo.Credentials
	Serve(interface{}, Stream) error
}
