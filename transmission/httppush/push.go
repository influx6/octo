// Package httppush provides a server which implements server push system provided
// by the new go http package.
package httppush

import (
	"bytes"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"sync"

	"github.com/influx6/faux/context"
	"github.com/influx6/octo"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/netutils"
	"github.com/influx6/octo/parsers/byteutils"
	"github.com/influx6/octo/parsers/jsonparser"
	"github.com/influx6/octo/systems/jsonsystem"
	uuid "github.com/satori/go.uuid"
)

// PushAttr defines a attribute struct for defining options for the WebPushServer
// struct.
type PushAttr struct {
	Addr          string
	Headers       http.Header
	Credential    octo.AuthCredential
	TLSConfig     *tls.Config
	Notifications chan []byte
}

// PushServer defines a struct implements the http.ServeHTTP interface which
// handles servicing http requests for websockets.
type PushServer struct {
	Attr     PushAttr
	log      octo.Logs
	info     octo.Info
	server   *http.Server
	listener net.Listener
	wg       sync.WaitGroup
	primary  *octo.BaseSystem
	system   octo.System
	rl       sync.Mutex
	running  bool
	doClose  bool
}

// New returns a new instance of a PushServer.
func New(attr PushAttr) *PushServer {
	ip, port, _ := net.SplitHostPort(attr.Addr)
	if ip == "" || ip == consts.AnyIP {
		if realIP, err := netutils.GetMainIP(); err == nil {
			attr.Addr = net.JoinHostPort(realIP, port)
		}
	}

	var suuid = uuid.NewV4().String()

	var ws PushServer
	ws.info = octo.Info{
		SUUID:  suuid,
		UUID:   suuid,
		Addr:   attr.Addr,
		Remote: attr.Addr,
		Local:  attr.Addr,
	}

	return &ws
}

// Credentials return the giving credentails of the provided server.
func (s *PushServer) Credentials() octo.AuthCredential {
	return s.Attr.Credential
}

// Listen begins the initialization of the websocket server.
func (s *PushServer) Listen(system octo.System) error {
	s.log.Log(octo.LOGINFO, s.info.UUID, "httppush.PushServer.Listen", "Started")

	if s.isRunning() {
		s.log.Log(octo.LOGINFO, s.info.UUID, "httppush.PushServer.Listen", "Completed")
		return nil
	}

	listener, err := netutils.MakeListener("tcp", s.Attr.Addr, s.Attr.TLSConfig)
	if err != nil {
		s.log.Log(octo.LOGERROR, s.info.UUID, "httppush.PushServer.Listen", "Initialize net.Listener failed : Error : %s", err.Error())
		return err
	}

	server, tlListener, err := netutils.NewHTTPServer(listener, s, s.Attr.TLSConfig)
	if err != nil {
		s.log.Log(octo.LOGERROR, s.info.UUID, "httppush.PushServer.Listen", "Initialize net.Listener failed : Error : %s", err.Error())
		return err
	}

	s.system = system
	s.primary = octo.NewBaseSystem(system, jsonparser.JSON, s.log, jsonsystem.BaseHandlers(), jsonsystem.AuthHandlers(s))

	s.rl.Lock()
	{
		s.running = true
		s.server = server
		s.listener = tlListener
		s.wg.Add(1)
	}
	s.rl.Unlock()

	s.log.Log(octo.LOGINFO, s.info.UUID, "httppush.PushServer.Listen", "Completed")
	return nil
}

// ServeHTTP implements the http.Handler.ServeHTTP interface method to handle http request
// converted to websockets request.
func (s *PushServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.log.Log(octo.LOGINFO, s.info.UUID, "httppush.PushServer.ServeHTTP", "Started")

	if s.shouldClose() {
		s.log.Log(octo.LOGINFO, s.info.UUID, "httppush.PushServer.ServeHTTP", "Completed")
		return
	}

	pusherResponse, ok := w.(http.Pusher)
	if !ok {
		http.Error(w, "HTTP Push not supported", http.StatusInternalServerError)
		return
	}

	defer r.Body.Close()

	var data bytes.Buffer

	if r.Body != nil {
		io.Copy(&data, r.Body)
	}

	var cuuid = uuid.NewV4().String()
	var pusher PushTransmission
	pusher.ctx = context.NewGoogleContext(r.Context())
	pusher.Pusher = pusherResponse
	pusher.Request = r
	pusher.Writer = w
	pusher.server = s
	pusher.info = octo.Info{
		UUID:   cuuid,
		SUUID:  s.info.SUUID,
		Local:  s.info.Addr,
		Addr:   r.RemoteAddr,
		Remote: r.RemoteAddr,
	}

	rem, err := s.primary.ServeBase(data.Bytes(), &pusher)
	if err != nil {
		s.log.Log(octo.LOGERROR, s.info.UUID, "httppush.PushServer.acceptRequests", "PushServer System : Fails Parsing : Error : %+s", err)

		if err := s.system.Serve(data.Bytes(), &pusher); err != nil {
			s.log.Log(octo.LOGERROR, s.info.UUID, "httppush.PushServer.ServeHTTP", "PushServer System : Fails Parsing : Error : %+s", err)
		}
	}

	// Handle remaining messages and pass it to user system.
	if err := s.system.Serve(byteutils.JoinMessages(rem...), &pusher); err != nil {
		s.log.Log(octo.LOGERROR, s.info.UUID, "httppush.PushServer.ServeHTTP", "PushServer System : Fails Parsing : Error : %+s", err)
	}

	s.log.Log(octo.LOGINFO, s.info.UUID, "httppush.PushServer.ServeHTTP", "Completed")
}

// Credential returns the crendentials for the giving server.
func (s *PushServer) Credential() octo.AuthCredential {
	return s.Attr.Credential
}

// Close ends the websocket connection and ensures all requests are finished.
func (s *PushServer) Close() error {
	s.log.Log(octo.LOGINFO, s.info.UUID, "httppush.PushServer.Close", "Start")
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
		s.log.Log(octo.LOGERROR, s.info.UUID, "httppush.PushServer.Close", "Completed : %+q", err.Error())
	}

	s.log.Log(octo.LOGINFO, s.info.UUID, "httppush.PushServer.Close", "Completed")
	return nil
}

// Wait awaits the closure of the giving client.
func (s *PushServer) Wait() {
	s.wg.Wait()
}

// shouldClose returns true/false if the client should close.
func (s *PushServer) shouldClose() bool {
	s.rl.Lock()
	defer s.rl.Unlock()
	return s.doClose
}

// isRunning returns true/false if the client is still running.
func (s *PushServer) isRunning() bool {
	s.rl.Lock()
	defer s.rl.Unlock()
	return s.running
}

//================================================================================

// PushTransmission defines a struct to hold the request and response object.
type PushTransmission struct {
	Request *http.Request
	Writer  http.ResponseWriter
	Pusher  http.Pusher
	ctx     context.Context
	server  *PushServer
	info    octo.Info
	log     octo.Logs
	buffer  bytes.Buffer
}

// SendAll pipes the giving data down the provided pipeline.
// This function call the PushTransmission.Send function internally,
// as multiple requests is not supported.
func (t *PushTransmission) SendAll(data []byte, flush bool) error {
	return t.Send(data, flush)
}

// Send pipes the giving data down the provided pipeline.
func (t *PushTransmission) Send(data []byte, flush bool) error {
	t.buffer.Write(data)

	if !flush {
		return nil
	}

	return nil
}

// Info returns the giving information for the internal client and server.
func (t *PushTransmission) Info() (octo.Info, octo.Info) {
	return t.info, t.server.info
}

// Ctx returns the context that is related to this object.
func (t *PushTransmission) Ctx() context.Context {
	return t.ctx
}

// Close ends the internal conneciton.
func (t *PushTransmission) Close() error {
	return nil
}
