package goclient

// StateHandlerType defines a int type to specific a handler type for registry.
type StateHandlerType int

// contains the value to reference the handler to be registered for.
const (
	ConnectHandler StateHandlerType = iota
	DisconnectHandler
	ClosedHandler
	ErrorHandler
)

// Contact defines a basic information regarding a specific connection.
type Contact struct {
	UUID string
	Addr string
}

// StateHandler defines a function which is called for the change of state of a
// connection eg closed, connected, disconnected.
type StateHandler func(Contact)

// ErrorStateHandler defines a function which is called for the error that occurs
// during connections.
type ErrorStateHandler func(Contact, error)

// Pod defines an interface that provides a means of delivery giving messages
// to another encapsulated endpoint.
type Pod interface {
	Close() error
	Send([]byte, bool) error
}

// Connection defines a interface which provides a means by which connections
// are made and processed.
type Connection interface {
	Pod
	Listen(System) error
	Register(StateHandlerType, interface{})
}

// System defines a interface which exposes a method to handle/process a giving
// byte slice and recieves the core pipe for response.
type System interface {
	Serve([]byte, Pod)
}
