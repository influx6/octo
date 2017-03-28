package client

import "github.com/influx6/octo"

// Stream defines an interface that provides a means of delivery giving messages
// to another encapsulated endpoint.
type Stream interface {
	Close() error
	Send(interface{}, bool) error
}

// Server defines a interface which exposes a method to handle/process a giving
// byte slice and recieves the core pipe for response.
type Server interface {
	Serve([]byte, Stream) error
}

// SystemServer defines a interface which exposes its credentials and servicing
// methods for usage.
type SystemServer interface {
	Server
	octo.Credentials
}

// System defines a interface which provides a means by which connections
// are made and processed.
type System interface {
	Stream
	Listen(SystemServer, octo.MessageEncoding) error
	Register(octo.StateHandlerType, interface{})
}
