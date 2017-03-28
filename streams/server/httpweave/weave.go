// Package httpweave provides a http layer for communicate with a low-level tcp
// server through http requests.
package httpweave

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/influx6/octo"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/netutils"
	"github.com/influx6/octo/utils"
	uuid "github.com/satori/go.uuid"
)

// WeaveAttr defines a attribute struct for defining options for the WebWeaveServer
// struct.
type WeaveAttr struct {
	Addr          string
	TargetAddr    string
	Headers       http.Header
	Transformer   octo.TCPTransformer
	Auth          octo.Authenticator
	TLSConfig     *tls.Config
	TCPTLSConfig  *tls.Config
	Notifications chan []byte
}

// WeaveServer defines a struct implements the http.ServeHTTP interface which
// handles servicing http requests for websockets.
type WeaveServer struct {
	Attr        WeaveAttr
	instruments octo.Instrumentation
	info        octo.Contact
	server      *http.Server
	listener    net.Listener
	mux         *WeaveServerMux
	wg          sync.WaitGroup
	rl          sync.Mutex
	running     bool
	doClose     bool
	closer      chan struct{}
}

// New returns a new instance of a WeaveServer.
func New(instrument octo.Instrumentation, attr WeaveAttr) *WeaveServer {
	ip, port, _ := net.SplitHostPort(attr.Addr)
	if ip == "" || ip == consts.AnyIP {
		if realIP, err := netutils.GetMainIP(); err == nil {
			attr.Addr = net.JoinHostPort(realIP, port)
		}
	}

	var suuid = uuid.NewV4().String()

	var ws WeaveServer
	ws.closer = make(chan struct{})
	ws.instruments = instrument
	ws.info = octo.Contact{
		SUUID:  suuid,
		UUID:   suuid,
		Addr:   attr.Addr,
		Remote: attr.Addr,
	}

	return &ws
}

// Listen begins the initialization of the websocket server.
func (s *WeaveServer) Listen() error {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpweave.WeaveServer.Listen", "Started")

	if s.isRunning() {
		s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpweave.WeaveServer.Listen", "Completed")
		return nil
	}

	if s.Attr.Notifications == nil {
		err := errors.New("WeaveServer expects you to provide a channel for notifications")
		s.instruments.Log(octo.LOGERROR, s.info.UUID, "httpweave.WeaveServer.Listen", "Initialize net.Listener failed : Error : %s", err.Error())
		return err
	}

	listener, err := netutils.MakeListener("tcp", s.Attr.Addr, s.Attr.TLSConfig)
	if err != nil {
		s.instruments.Log(octo.LOGERROR, s.info.UUID, "httpweave.WeaveServer.Listen", "Initialize net.Listener failed : Error : %s", err.Error())
		return err
	}

	server, tlListener, err := netutils.NewHTTPServer(listener, s, s.Attr.TLSConfig)
	if err != nil {
		s.instruments.Log(octo.LOGERROR, s.info.UUID, "httpweave.WeaveServer.Listen", "Initialize net.Listener failed : Error : %s", err.Error())
		return err
	}

	s.server = server
	s.listener = tlListener
	s.mux = NewWeaveServerMux(s.instruments, s.Attr.Auth, s.Attr.Transformer, s.info, s.Attr.TargetAddr, s.Attr.TCPTLSConfig)

	s.rl.Lock()
	{
		s.running = true
		s.wg.Add(1)
	}
	s.rl.Unlock()

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpweave.WeaveServer.Listen", "Completed")
	return nil
}

// Close ends the websocket connection and ensures all requests are finished.
func (s *WeaveServer) Close() error {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpweave.WeaveServer.Close", "Start")
	if !s.isRunning() {
		return nil
	}

	s.rl.Lock()
	{
		s.running = false
		s.doClose = true
		s.wg.Done()
	}
	s.rl.Unlock()

	if err := s.server.Close(); err != nil {
		s.instruments.Log(octo.LOGERROR, s.info.UUID, "httpweave.WeaveServer.Close", "Completed : %+q", err.Error())
	}

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpweave.WeaveServer.Close", "Completed")
	return nil
}

// Wait awaits the closure of the giving client.
func (s *WeaveServer) Wait() {
	s.wg.Wait()
}

// shouldClose returns true/false if the client should close.
func (s *WeaveServer) shouldClose() bool {
	s.rl.Lock()
	defer s.rl.Unlock()
	return s.doClose
}

// isRunning returns true/false if the client is still running.
func (s *WeaveServer) isRunning() bool {
	s.rl.Lock()
	defer s.rl.Unlock()
	return s.running
}

// ServeHTTP implements the http.Handler.ServeHTTP interface method to handle http request
// converted to websockets request.
func (s *WeaveServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpweave.WeaveServer.ServeHTTP", "Started")

	if s.shouldClose() {
		s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpweave.WeaveServer.ServeHTTP", "Completed")
		return
	}

	s.mux.ServeHTTP(w, r)

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpweave.WeaveServer.ServeHTTP", "Completed")
}

//================================================================================

// WeaveServerMux defines a struct which implements the http.Handler interface for
// handling http server-sent requests.
type WeaveServerMux struct {
	Attr               WeaveAttr
	OverrideTargetAddr string // targetAddr to be used regardless of incoming data from client.
	info               octo.Contact
	auth               octo.Authenticator
	instruments        octo.Instrumentation
	transformer        octo.TCPTransformer // if not supplied will send data as is.
	config             *tls.Config
}

// NewWeaveServerMux returns a new instance of a WeaveServerMux which will use the
// provided targetAddr if provided as destination else use that from the incoming
// data from the request.
func NewWeaveServerMux(instruments octo.Instrumentation, auth octo.Authenticator, transfomer octo.TCPTransformer, info octo.Contact, targetAddr string, config *tls.Config) *WeaveServerMux {
	ip, port, _ := net.SplitHostPort(targetAddr)
	if ip == "" || ip == consts.AnyIP {
		if realIP, err := netutils.GetMainIP(); err == nil {
			targetAddr = net.JoinHostPort(realIP, port)
		}
	}

	return &WeaveServerMux{
		config:             config,
		auth:               auth,
		info:               info,
		instruments:        instruments,
		transformer:        transfomer,
		OverrideTargetAddr: targetAddr,
	}
}

// authenticate runs the authentication procedure to authenticate that the connection
// was valid.
func (s *WeaveServerMux) authenticate(request *http.Request) error {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpbasic.WeaveServerMux.authenticate", "Started")
	if s.auth == nil {
		s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpbasic.WeaveServerMux.authenticate", "Completed : No Authentication Required")
		return nil
	}

	authorizationHeader := request.Header.Get("Authorization")

	s.instruments.NotifyEvent(octo.Event{
		Type:   octo.ConnectionAuthenticate,
		Client: s.info.UUID,
		Server: s.info.SUUID,
		Details: map[string]interface{}{
			"addr": request.RemoteAddr,
		},
		Data: octo.NewConnectionInstrument(request.RemoteAddr, s.Attr.Addr),
	})

	if len(authorizationHeader) == 0 {
		return errors.New("'Authorization' header needed for authentication")
	}

	credentials, err := utils.ParseAuthorization(authorizationHeader)
	if err != nil {
		s.instruments.Log(octo.LOGERROR, s.info.UUID, "httpbasic.WeaveServerMux.authenticate", "Completed : Error : %+q", err)
		return err
	}

	if err := s.auth.Authenticate(credentials); err != nil {
		s.instruments.Log(octo.LOGERROR, s.info.UUID, "httpbasic.WeaveServerMux.authenticate", "Completed : Error : %+q", err)
		return err
	}

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpbasic.WeaveServerMux.authenticate", "Completed")
	return nil
}

// ServeHTTP implements the http.Handler.ServeHTTP interface method to handle http request
// converted to websockets request.
func (s *WeaveServerMux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpweave.WeaveServer.ServeHTTP", "Started")

	s.instruments.NotifyEvent(octo.Event{
		Type:   octo.GoroutineOpened,
		Client: s.info.UUID,
		Server: s.info.SUUID,
		Data:   octo.NewGoroutineInstrument(),
		Details: map[string]interface{}{
			"addr":  r.RemoteAddr,
			"uuid":  s.info.UUID,
			"suuid": s.info.SUUID,
		},
	})

	defer s.instruments.NotifyEvent(octo.Event{
		Type:   octo.GoroutineClosed,
		Client: s.info.UUID,
		Server: s.info.SUUID,
		Data:   octo.NewGoroutineInstrument(),
		Details: map[string]interface{}{
			"addr":  r.RemoteAddr,
			"uuid":  s.info.UUID,
			"suuid": s.info.SUUID,
		},
	})

	if err := s.authenticate(r); err != nil {
		s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpweave.WeaveServer.ServeHTTP", "Completed : Error : Failed to autnethicate : %+q", err)
		http.Error(w, "Http Streaming not supported", http.StatusInternalServerError)
		return
	}

	defer r.Body.Close()

	var tcpRequest octo.TCPRequest

	if err := json.NewDecoder(r.Body).Decode(&tcpRequest); err != nil {
		s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpweave.WeaveServer.ServeHTTP", "Completed : Error : Failed to parse : %+q", err)
		http.Error(w, "Expected octo.TCPRequest JSON: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.instruments.NotifyEvent(octo.Event{
		Type:   octo.DataRead,
		Client: s.info.UUID,
		Server: s.info.SUUID,
		Data:   tcpRequest,
		Details: map[string]interface{}{
			"addr":  r.RemoteAddr,
			"uuid":  s.info.UUID,
			"suuid": s.info.SUUID,
		},
	})

	tcpData, err := s.transformer.TransformRequest(tcpRequest)

	s.instruments.NotifyEvent(octo.Event{
		Type:   octo.DataTransform,
		Client: s.info.UUID,
		Server: s.info.SUUID,
		Data:   octo.NewDataInstrument(tcpData, err),
		Details: map[string]interface{}{
			"addr":  r.RemoteAddr,
			"uuid":  s.info.UUID,
			"suuid": s.info.SUUID,
		},
	})

	if err != nil {
		s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpweave.WeaveServer.ServeHTTP", "Completed : Error : Failed to transform tcp request: %+q", err)
		http.Error(w, "Failed to transform octo.TCPRequest for tcp connection: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var targetAddr string

	if s.OverrideTargetAddr != "" {
		targetAddr = s.OverrideTargetAddr
	} else {
		targetAddr = tcpRequest.Addr
	}

	// Create a new TCPClient with the appropriate address and then write the new
	// octo.TCPRequest and read until response and wire to response.
	client, err := NewTCPClient(targetAddr, s.config)
	if err != nil {
		s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpweave.WeaveServer.ServeHTTP", "Completed : Error : Failed to create new tcpclient : %+q", err)
		http.Error(w, "Failed to create tcp client connection: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if werr := client.Write(tcpData, true); werr != nil {
		s.instruments.NotifyEvent(octo.Event{
			Type:   octo.DataWrite,
			Client: s.info.UUID,
			Server: s.info.SUUID,
			Data:   octo.NewDataInstrument(tcpData, werr),
			Details: map[string]interface{}{
				"addr":  r.RemoteAddr,
				"uuid":  s.info.UUID,
				"suuid": s.info.SUUID,
			},
		})

		s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpweave.WeaveServer.ServeHTTP", "Completed : Error : Failed to write data to tcp : %+q", werr)
		http.Error(w, "Failed to Write to tcp connection: "+werr.Error(), http.StatusInternalServerError)
		return
	}

	client.SetReadDeadline(time.Now().Add(consts.OverMaxWaitReadTime))
	// client.SetWriteDeadline(time.Now().Add(consts.MaxWaitWriteTime))

	newData, err := client.Read()
	s.instruments.NotifyEvent(octo.Event{
		Type:   octo.DataRead,
		Client: s.info.UUID,
		Server: s.info.SUUID,
		Data:   octo.NewDataInstrument(newData, err),
		Details: map[string]interface{}{
			"addr": r.RemoteAddr,
		},
	})

	if err != nil {
		s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpweave.WeaveServer.ServeHTTP", "Completed : Error : Failed to read data from tcp client : %+q", err)
		http.Error(w, "Failed to Read data from tcp connection: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Close tcp connection and record instrumentation.
	s.instruments.NotifyEvent(octo.Event{
		Type:   octo.ConnectionDisconnect,
		Client: s.info.UUID,
		Server: s.info.SUUID,
		Data:   octo.NewConnectionInstrument(r.RemoteAddr, s.Attr.Addr),
		Details: map[string]interface{}{
			"error": client.Close(),
		},
	})

	newData = bytes.TrimSuffix(newData, consts.CTRLLine)

	tcpResponse, err := s.transformer.TransformResponse(newData)

	s.instruments.NotifyEvent(octo.Event{
		Type:   octo.DataTransform,
		Client: s.info.UUID,
		Server: s.info.SUUID,
		Data:   tcpResponse,
		Details: map[string]interface{}{
			"addr": r.RemoteAddr,
		},
	})

	if err != nil {

		s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpweave.WeaveServer.ServeHTTP", "Completed : Error : Failed to transform tcp client data : %+q", err)
		http.Error(w, "Failed to Read data from tcp connection: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := json.NewEncoder(w).Encode(tcpResponse); err != nil {
		s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpweave.WeaveServer.ServeHTTP", "Completed : Error : Failed to transform tcp client response : %+q", err)
		http.Error(w, "Failed to jsonify tcp respoonse data from tcp connection: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpweave.WeaveServer.ServeHTTP", "Completed")
}
