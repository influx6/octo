package consts

import "errors"

// Contains the set of constant values usable in data transmissions.
const (
	CTRL = "\r\n"
)

// Contains the set of possible request and response headers.
// Each has it's request and response version.
var (
	CTRLLine           = []byte(CTRL)
	ClientInfoRequest  = []byte("CLINFO")
	ClientInfoResponse = []byte("CLINFORES")
	InfoRequest        = []byte("INFO")
	InfoResponse       = []byte("INFORES")
	AuthRequest        = []byte("AUTH")
	AuthResponse       = []byte("AUTHCRED")
	ClusterRequest     = []byte("CLUSTERS")
	ClusterResponse    = []byte("CLUSTERRES")
	OK                 = []byte("OK")
)

// contains the set of errors used by the package.
var (
	ErrConnClosed = errors.New("Connection Closed")
)
