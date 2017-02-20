package octo

import (
	"github.com/influx6/faux/context"
)

// Contains sets of log levels usable in logging operation details.
const (
	LOGINFO         string = "INFO"
	LOGDEBUG        string = "DEBUG"
	LOGERROR        string = "ERROR"
	LOGTRANSMISSION string = "TRANSMISSION"
)

// Logs defines an interface which provides the capability of structures to meet the
// interface for logging data details.
type Logs interface {
	Log(level string, namespace string, message string, items ...interface{})
}

// System defines a interface for processing systems which handle request
// connections from a server.
type System interface {
	Authenticate(AuthCredential) error
	Serve(data []byte, tx Transmission) error
}

// Info defines specific data related to a giving source.
type Info struct {
	UUID   string `json:"uuid"`
	SUUID  string `json:"suuid"`
	Addr   string `json:"addr"`
	Remote string `json:"remote"`
}

// AuthCredential defines a struct which holds credentails related to
// the client connecting to the provider.
type AuthCredential struct {
	APIKey string `json:"api_key"`
	Token  string `json:"token"`
}

// Transmission defines an interface which exposes methods to transmit data over
// a giving transmit.
type Transmission interface {
	Close() error
	Info() (Info, Info)
	Ctx() context.Context
	Send(data []byte, flush bool) error
	SendAll(data []byte, flush bool) error
	// Cluster(addr string, data []byte) error
}

// Transmit defines a structure which is used to deliver
// specific data from a giving System with the Transmission
// through which it responds.
type Transmit struct {
	Data []byte
	UUID string
}
