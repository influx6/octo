package octo

import (
	"fmt"
	"time"
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

// Parser defines a interface which exposes a method to parse a provided input
// returning a giving value(interface type) or an error.
type Parser interface {
	Parse([]byte) ([]Command, error)
}

// Authenticator defines a interface type which exposes a method which handles
// the processing of credential authentication.
type Authenticator interface {
	Authenticate(AuthCredential) error
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

// Instrumentation defines an interface for needed features that provided logging,
// metric measurements and time tracking, these instrumentation details allows us
// to measure the internal operations of systems.
type Instrumentation interface {
	Logs
	DataInstrumentation
	GoRoutineInstrumentation
	ConnectionInstrumentation
}

//================================================================================

// Logs defines an interface which provides the capability of structures to meet the
// interface for logging data details.
type Logs interface {
	Log(level string, namespace string, function string, message string, items ...interface{})
}

//================================================================================

// ConnectionType defines a int type for usage with Connection Insturmentation.
type ConnectionType int

// contains the various type of a ConnectionType.
const (
	ConnectionConnet ConnectionType = iota
	ConnectionDisconnect
	ConnectionAuthenticate
)

// SingleConnectionInstrument defines a struct which stores the different data
// related to any connection/disconnection/authentication event that occurs
// for individual systems.
type SingleConnectionInstrument struct {
	Source string
	Target string
	Meta   map[string]interface{}
}

// ConnectionInstrument defines a grouped collection of data expressing the
// different operations that occur for a connection type operation.
type ConnectionInstrument struct {
	Context          string                       `js:"context"`
	TotalConnections int64                        `json:"total_connections"`
	Connects         []SingleConnectionInstrument `js:"connections"`
	Disconnects      []SingleConnectionInstrument `js:"disconnections"`
	Authentications  []SingleConnectionInstrument `js:"authentications"`
}

// ConnectionInstrumentation defines an interface which provides a instrumentation
// for reporting connection based event.
type ConnectionInstrumentation interface {
	RecordConnectionOp(tp ConnectionType, context string, from string, to string, meta map[string]interface{})
	GetConnectionInstruments() []ConnectionInstrument
}

//================================================================================

// GoroutineOpType defines a int type for usage with Connection Insturmentation.
type GoroutineOpType int

// contains the various value type of GoroutineOpType.
const (
	GoroutineOpened GoroutineOpType = iota
	GoroutineClosed
)

// SingleGoroutineInstrument defines a single instance of a opened/closed goroutine
// including the goroutine stack trace at that point in time.
type SingleGoroutineInstrument struct {
	Line  int
	File  string
	Stack []byte
	Time  time.Time
	Meta  map[string]interface{}
}

// GoroutineInstrument defines a struct which stores the different data collected
// during the operation of a
type GoroutineInstrument struct {
	Total   int64
	Context string
	Opened  []SingleGoroutineInstrument
	Closed  []SingleGoroutineInstrument
}

// GoRoutineInstrumentation provides a basic instrumentation interface for measuring the
// total used and resolved goroutines for tracking leakages and go-routine usage.
type GoRoutineInstrumentation interface {
	RecordGoroutineOp(ty GoroutineOpType, context string, meta map[string]interface{})
	GetGoroutineInstruments() []GoroutineInstrument
}

//================================================================================

// DataOpType defines a int type for usage with Connection Insturmentation.
type DataOpType int

// contains the various value type of DataOpType.
const (
	DataRead DataOpType = iota
	DataWrite
	DataTransform
)

// DataInstrument defines a struct which stores the different data collected
// during operations.
type DataInstrument struct {
	Context         string
	TotalReads      int64
	TotalWrites     int64
	TotalTransforms int64
	Reads           []SingleDataInstrument
	Writes          []SingleDataInstrument
	Transforms      []SingleDataInstrument
}

// SingleDataInstrument defines a single unit of a DataInstrumentation object which
// records the reads, write and any error which occured for such an operation.
type SingleDataInstrument struct {
	Data  []byte
	Error error
	Meta  map[string]interface{}
}

// DataInstrumentation provides a instrumentation for measuring reads, writes
// operations
type DataInstrumentation interface {
	RecordDataOp(op DataOpType, context string, err error, data []byte, meta map[string]interface{})
	GetDataInstruments() []DataInstrument
}

//================================================================================
