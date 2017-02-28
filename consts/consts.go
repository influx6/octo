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
	ReadTimeout      = 7 * time.Second
	MinTempSleep     = 10 * time.Millisecond
	MaxSleepTime     = 2 * time.Second
	MinSleepTime     = 10 * time.Millisecond
	FlushDeline      = 2 * time.Second
	ConnectDeadline  = 4 * time.Second
	MaxWaitTime      = 3 * time.Second
	MaxWaitReadTime  = 5 * time.Second
	MaxWaitWriteTime = 5 * time.Second
	MinDataSize      = 512
	MaxConnections   = (64 * 1024)
	MaxPayload       = (1024 * 1024)
	MaxBufferSize    = (1024 * 1024)
	MaxDataWrite     = 6048
)

// Contains set variables for use in connection packages.
var (
	TLSTimeout  = float64(500&time.Millisecond) / float64(time.Second)
	AuthTimeout = float64(2*TLSTimeout) / float64(time.Second)
)

// Contains the set of possible request and response headers.
// Each has it's request and response version.
var (
	CTRLLine           = []byte(CTRL)
	PING               = []byte("PING")
	PONG               = []byte("PONG")
	ClientInfoRequest  = []byte("CLINFO")
	ClientInfoResponse = []byte("CLINFORES")
	InfoRequest        = []byte("INFO")
	InfoResponse       = []byte("INFORES")
	AuthRequest        = []byte("AUTH")
	AuthResponse       = []byte("AUTHCRED")
	ClusterRequest     = []byte("CLUSTERS")
	ClusterResponse    = []byte("CLUSTERRES")
	ClusterDistRequest = []byte("CLUSTERDISTRI")
	ClusterPostOK      = []byte("CLUSTERSOK")
	OK                 = []byte("OK")
)

// contains the set of errors used by the package.
var (
	ErrConnClosed  = errors.New("Connection Closed")
	ErrUnsupported = errors.New("Functionality is unsupported")

	// ErrAuthorizationFailed  is the error returned when the giving credentials
	// fail to authenticate.
	ErrAuthorizationFailed = errors.New("Invalid Credentials: Authorization Failed")
)
