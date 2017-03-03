package octo

import (
	"fmt"

	"github.com/influx6/faux/context"
)

// Contains sets of log levels usable in logging operation details.
const (
	LOGINFO         string = "INFO"
	LOGDEBUG        string = "DEBUG"
	LOGERROR        string = "ERROR"
	LOGTRANSMISSION string = "TRANSMISSION"
	LOGTRANSMITTED  string = "TRANSMITTED"
)

//================================================================================

// Command defines a struct which holds the giving operation expected to be performed,
// It provides the name and data of command expected.
type Command struct {
	Name []byte   `json:"name"`
	Data [][]byte `json:"data"`
}

// String returns a stringified version of the giving message.
func (c Command) String() string {
	return fmt.Sprintf("{ Command: %+q, Data: %+q }", c.Name, c.Data)
}

//================================================================================

// Parser defines a interface which exposes a method to parse a provided input
// returning a giving value(interface type) or an error.
type Parser interface {
	Parse([]byte) ([]Command, error)
}

//================================================================================

// Instrumentation defines an interface for needed features that provided logging,
// metric measurements and time tracking, these instrumentation details allows us
// to measure the internal operations of systems.
type Instrumentation interface {
	Logs
	TimeInstrumentation
	DataInstrumentation
	GoRoutineInstrumentation
	ConnectionInstrumentation
}

// NewInstrumentation returns a new InstrumentationBase instance which unifies
// the instrumentation provides for providing a unified instrumentation object.
func NewInstrumentation(log Logs, times TimeInstrumentation, datas DataInstrumentation, gor GoRoutineInstrumentation, conns ConnectionInstrumentation) Instrumentation {
	return instrumentationBase{
		Logs:                      log,
		TimeInstrumentation:       times,
		DataInstrumentation:       datas,
		GoRoutineInstrumentation:  gor,
		ConnectionInstrumentation: conns,
	}
}

// instrumentationBase defines a struct which provides a implementation for the
// Instrumentation interface.
type instrumentationBase struct {
	Logs
	TimeInstrumentation
	DataInstrumentation
	GoRoutineInstrumentation
	ConnectionInstrumentation
}

// Logs defines an interface which provides the capability of structures to meet the
// interface for logging data details.
type Logs interface {
	Log(level string, namespace string, function string, message string, items ...interface{})
}

// ConnectionInstrumentation defines an interface which provides a instrumentation
// for reporting connection based event.
type ConnectionInstrumentation interface {
	NewConnection(context string, in Info, meta []byte)
	NewDisconnection(context string, in Info, meta []byte)
	NewAuthentication(context string, in Info, status bool, data []byte)
}

// GoRoutineInstrumentation provides a basic instrumentation interface for measuring the
// total used and resolved goroutines for tracking leakages and go-routine usage.
type GoRoutineInstrumentation interface {
	IncrementGoRoutinesFor(context string, meta []byte)
	DecrementGoRoutinesFor(context string, meta []byte)
}

// DataInstrumentation provides a instrumentation for measuring reads, writes
// operations
type DataInstrumentation interface {
	NewWrites(context string, meta []byte, data []byte)
	NewReads(context string, meta []byte, data []byte)
}

// TimeInstrumentation defines an interface which provides a instrumentation for
// reporting connection based event.
type TimeInstrumentation interface {
	Start(context string, op string, uuid string)
	End(context string, op string, uuid string)
}

//================================================================================

// TransmissionProtocol defines a type which accepts a system and encapsulates the transmission
// and exchange of data using the System has the processing unit.
type TransmissionProtocol interface {
	Close() error
	Listen(System) error
}

//================================================================================

// TransmissionServer defines a type which exposes a method to service a provided
// data.
type TransmissionServer interface {
	Serve(data []byte, tx Transmission) error
}

// SelectiveTransmissionServer defines a tye which exposes the capability to
// test if the data lies within it's jurisdiction for service.
type SelectiveTransmissionServer interface {
	TransmissionServer
	CanServe(data []byte) bool
}

// SelectiveServers defines a slice of TransmissionServer and exposes a method to
// use this has a single server where if an element returns an error then that
// error is .
type SelectiveServers []SelectiveTransmissionServer

// Serve delivers the data and Transmission to the first capable server by calling
// each servers to individually attests to their  capability to serve the request.
func (s SelectiveServers) Serve(data []byte, tx Transmission) error {
	for _, server := range s {
		if !server.CanServe(data) {
			continue
		}

		return server.Serve(data, tx)
	}

	return ErrRequestUnsearvable
}

//================================================================================

// Authenticator defines a interface type which exposes a method which handles
// the processing of credential authentication.
type Authenticator interface {
	Authenticate(AuthCredential) error
}

// System defines a interface for processing systems which handle request
// connections from a server.
type System interface {
	Authenticator
	TransmissionServer
}

//================================================================================

// Info defines specific data related to a giving source.
type Info struct {
	UUID   string `json:"uuid"`
	SUUID  string `json:"suuid"`
	Addr   string `json:"addr"`
	Remote string `json:"remote"`
	Local  string `json:"local"`
}

//================================================================================

// AuthCredential defines a struct which holds credentails related to
// the client connecting to the provider.
type AuthCredential struct {
	Scheme string `json:"scheme"`
	Key    string `json:"key"`
	Token  string `json:"token"`
	Data   []byte `json:"data"`
}

//================================================================================

// Credentials defines a type which exposes a method to return the credentials
// for the giving entity.
type Credentials interface {
	Credential() AuthCredential
}

//================================================================================

// Transmission defines an interface which exposes methods to transmit data over
// a giving transmit.
type Transmission interface {
	Close() error
	Info() (Info, Info)
	Ctx() context.Context
	Send(data []byte, flush bool) error
	SendAll(data []byte, flush bool) error
}

//================================================================================
