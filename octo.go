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

// Parser defines a interface which exposes a method to parse a provided input
// returning a giving value(interface type) or an error.
type Parser interface {
	Parse([]byte) ([]Command, error)
}

// Logs defines an interface which provides the capability of structures to meet the
// interface for logging data details.
type Logs interface {
	Log(level string, namespace string, function string, message string, items ...interface{})
}

// TransmissionProtocol defines a type which accepts a system and encapsulates the transmission
// and exchange of data using the System has the processing unit.
type TransmissionProtocol interface {
	Close() error
	Listen(System) error
}

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

// Info defines specific data related to a giving source.
type Info struct {
	UUID   string `json:"uuid"`
	SUUID  string `json:"suuid"`
	Addr   string `json:"addr"`
	Remote string `json:"remote"`
	Local  string `json:"local"`
}

// AuthCredential defines a struct which holds credentails related to
// the client connecting to the provider.
type AuthCredential struct {
	Scheme string `json:"scheme"`
	Key    string `json:"key"`
	Token  string `json:"token"`
	Data   []byte `json:"data"`
}

// Credentials defines a type which exposes a method to return the credentials
// for the giving entity.
type Credentials interface {
	Credential() AuthCredential
}

// Transmission defines an interface which exposes methods to transmit data over
// a giving transmit.
type Transmission interface {
	Close() error
	Info() (Info, Info)
	Ctx() context.Context
	Send(data []byte, flush bool) error
	SendAll(data []byte, flush bool) error
}

// ReceivedTransmit defines a structure which is used to deliver
// specific data from a giving System with the Transmission
// through which it responds.
type ReceivedTransmit struct {
	Data []byte
	UUID string
}

// SentTransmit defines a structure which is used to deliver
// specific data from a giving System with the Transmission
// through which it responds.
type SentTransmit struct {
	Data []byte
	UUID string
}
