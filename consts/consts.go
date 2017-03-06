package consts

import (
	"errors"
	"time"
)

// Contains the set of constant values usable in data transmissions.
const (
	CTRL  = "\r\n"
	AnyIP = "0.0.0.0"
	Zero  = "\x00"
)

// contains a giving set of constants for usage in other packages.
const (
	KeepAlivePeriod           = 5 * time.Minute
	ReadTimeout               = 5 * time.Second
	WriteTimeout              = 5 * time.Second
	ReadTempTimeout           = 3 * time.Second
	WriteTempTimeout          = 4 * time.Second
	MinTempSleep              = 10 * time.Millisecond
	MaxSleepTime              = 2 * time.Second
	MinSleepTime              = 10 * time.Millisecond
	FlushDeline               = 2 * time.Second
	ConnectDeadline           = 4 * time.Second
	MaxWaitTime               = 3 * time.Second
	MaxWaitReadTime           = 5 * time.Second
	OverMaxWaitReadTime       = 30 * time.Second
	MaxWaitWriteTime          = 5 * time.Second
	MinDataSize               = 512
	MaxConnections            = (64 * 1024)
	MaxPayload                = (1024 * 1024)
	MaxBufferSize             = (1024 * 1024)
	MaxDataWrite              = 6048
	MaxAcceptableEOF          = 10
	MaxAcceptableReadTimeout  = 5
	MaxTotalReconnection      = 5
	MaxTotalConnectionFailure = 5
	WSReadTimeout             = 30 * time.Second
	WSWriteTimeout            = 20 * time.Second
)

// Contains set variables for use in connection packages.
var (
	TLSTimeout  = float64(500&time.Millisecond) / float64(time.Second)
	AuthTimeout = float64(2*TLSTimeout) / float64(time.Second)
)

// Contains the set of possible request and response headers.
// Each has it's request and response version.
var (
	CTRLLine             = []byte(CTRL)
	PING                 = []byte("PING")
	PONG                 = []byte("PONG")
	ClientInfoRequest    = []byte("CLINFO")
	ClientInfoResponse   = []byte("CLINFORES")
	InfoRequest          = []byte("INFO")
	InfoResponse         = []byte("INFORES")
	AuthRequest          = []byte("AUTH")
	AuthResponse         = []byte("AUTHCRED")
	ClusterRequest       = []byte("CLUSTERS")
	ClusterResponse      = []byte("CLUSTERRES")
	ClusterDistRequest   = []byte("CLUSTERDISTRI")
	ClusterPostOK        = []byte("CLUSTERSOK")
	AuthroizationDenied  = []byte("AuthDenied")
	AuthroizationGranted = []byte("AuthGranted")
	OK                   = []byte("OK")
)

// contains the set of errors used by the package.
var (
	ErrConnClosed         = errors.New("Connection Closed")
	ErrUnsupported        = errors.New("Functionality is unsupported")
	ErrTimeoutOverReached = errors.New("Maximum timeout allowed reached")

	ErrUnstableRead  = errors.New("Connection read was unstable")
	ErrUnstableWrite = errors.New("Connection write was unstable")

	ErrNoServerFound = errors.New("Available server not found")

	// ErrRequestUnsearvable defines the error returned when a request can not
	// be handled.
	ErrRequestUnsearvable = errors.New("Request Unserveable")

	// ErrAuthorizationFailed  is the error returned when the giving credentials
	// fail to authenticate.
	ErrAuthorizationFailed = errors.New("Invalid Credentials: Authorization Failed")

	// ErrClosedConnection is returned when the giving client connection
	// has being closed.
	ErrClosedConnection = errors.New("Connection Closed")

	ErrAbitraryCloseConnection = errors.New("Connection Closed abitrarily")
)
