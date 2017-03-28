// Package http provides a server which implements server push system provided
// by the new go http package.
package http

import (
	"bytes"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"

	"github.com/influx6/faux/context"
	"github.com/influx6/octo"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/messages/jsoni"
	"github.com/influx6/octo/messages/jsoni/server"
	"github.com/influx6/octo/netutils"
	stream "github.com/influx6/octo/streams/server"
	"github.com/influx6/octo/utils"
	uuid "github.com/satori/go.uuid"
)

// BasicAttr defines a attribute struct for defining options for the WebBasicServer
// struct.
type BasicAttr struct {
	Addr         string
	Authenticate bool
	Headers      http.Header
	TLSConfig    *tls.Config
	Auth         octo.AuthCredential
}

// BasicServer defines a struct implements the http.ServeHTTP interface which
// handles servicing http requests for websockets.
type BasicServer struct {
	Attr        BasicAttr
	instruments octo.Instrumentation
	info        octo.Contact
	server      *http.Server
	listener    net.Listener
	wg          sync.WaitGroup
	basic       *BasicServeHTTP
	rl          sync.Mutex
	running     bool
	doClose     bool
}

// New returns a new instance of a BasicServer.
func New(instruments octo.Instrumentation, attr BasicAttr) *BasicServer {
	ip, port, _ := net.SplitHostPort(attr.Addr)
	if ip == "" || ip == consts.AnyIP {
		if realIP, err := netutils.GetMainIP(); err == nil {
			attr.Addr = net.JoinHostPort(realIP, port)
		}
	}

	var suuid = uuid.NewV4().String()

	var ws BasicServer
	ws.Attr = attr
	ws.instruments = instruments
	ws.info = octo.Contact{
		SUUID:  suuid,
		UUID:   suuid,
		Addr:   attr.Addr,
		Remote: attr.Addr,
		Local:  attr.Addr,
	}

	return &ws
}

// Credential return the giving credentails of the provided server.
func (s *BasicServer) Credential() octo.AuthCredential {
	return s.Attr.Auth
}

// Listen begins the initialization of the websocket server.
func (s *BasicServer) Listen(system stream.System) error {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpbasic.BasicServer.Listen", "Started")

	if s.isRunning() {
		s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpbasic.BasicServer.Listen", "Completed")
		return nil
	}

	listener, err := netutils.MakeListener("tcp", s.Attr.Addr, s.Attr.TLSConfig)
	if err != nil {
		s.instruments.Log(octo.LOGERROR, s.info.UUID, "httpbasic.BasicServer.Listen", "Initialize net.Listener failed : Error : %s", err.Error())
		return err
	}

	server, tlListener, err := netutils.NewHTTPServer(listener, s, s.Attr.TLSConfig)
	if err != nil {
		s.instruments.Log(octo.LOGERROR, s.info.UUID, "httpbasic.BasicServer.Listen", "Initialize net.Listener failed : Error : %s", err.Error())
		return err
	}

	s.basic = NewBasicServeHTTP(s.Attr.Authenticate, s.instruments, s.info, s, system)

	s.rl.Lock()
	{
		s.running = true
		s.server = server
		s.listener = tlListener
		s.wg.Add(1)
	}
	s.rl.Unlock()

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpbasic.BasicServer.Listen", "Completed")
	return nil
}

// ServeHTTP implements the http.Handler.ServeHTTP interface method to handle http request
// converted to websockets request.
func (s *BasicServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpbasic.BasicServer.ServeHTTP", "Started")

	if s.shouldClose() {
		s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpbasic.BasicServer.ServeHTTP", "Completed")
		return
	}

	// Call basic to treat the request.
	s.basic.ServeHTTP(w, r)

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpbasic.BasicServer.ServeHTTP", "Completed")
}

// Close ends the websocket connection and ensures all requests are finished.
func (s *BasicServer) Close() error {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpbasic.BasicServer.Close", "Start")
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
		s.instruments.Log(octo.LOGERROR, s.info.UUID, "httpbasic.BasicServer.Close", "Completed : %+q", err.Error())
	}

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpbasic.BasicServer.Close", "Completed")
	return nil
}

// Wait awaits the closure of the giving client.
func (s *BasicServer) Wait() {
	s.wg.Wait()
}

// shouldClose returns true/false if the client should close.
func (s *BasicServer) shouldClose() bool {
	s.rl.Lock()
	defer s.rl.Unlock()
	return s.doClose
}

// isRunning returns true/false if the client is still running.
func (s *BasicServer) isRunning() bool {
	s.rl.Lock()
	defer s.rl.Unlock()
	return s.running
}

//================================================================================

// BasicServeHTTP provides a structure which implements the http.Handler and provides
// the core server which implements the needed functionality to use the streams.System
// interface. It is provided as a seperate module to allow flexibility with user
// created server and routes for http requests.
type BasicServeHTTP struct {
	primary      *jsoni.SxConversations
	system       stream.System
	instruments  octo.Instrumentation
	info         octo.Contact
	authenticate bool
}

// NewBasicServeHTTP returns a new instance of the BasicServeHTTP object.
func NewBasicServeHTTP(authenticate bool, inst octo.Instrumentation, info octo.Contact, auth octo.Credentials, system stream.System) *BasicServeHTTP {
	primary := jsoni.NewSxConversations(system, &server.ContactServer{}, &server.ConversationServer{}, &server.AuthServer{Credentials: auth})

	return &BasicServeHTTP{
		authenticate: authenticate,
		primary:      primary,
		system:       system,
		instruments:  inst,
		info:         info,
	}
}

// Contact returns the info object associated with this server.
func (s *BasicServeHTTP) Contact() octo.Contact {
	return s.info
}

// ServeHTTP implements the http.Handler.ServeHTTP interface method to handle http request
// converted to websockets request.
func (s *BasicServeHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpbasic.BasicServeHTTP.ServeHTTP", "Started")

	w.Header().Add("Connection", "keep-alive")

	defer r.Body.Close()

	var data bytes.Buffer

	if r.Body != nil {
		io.Copy(&data, r.Body)
	}

	var cuuid = uuid.NewV4().String()
	var basic BasicTransmission
	basic.ctx = context.NewGoogleContext(r.Context())
	basic.info = octo.Contact{
		UUID:   cuuid,
		SUUID:  s.info.SUUID,
		Local:  s.info.Addr,
		Addr:   r.RemoteAddr,
		Remote: r.RemoteAddr,
	}

	basic.instruments = s.instruments
	basic.system = s.system
	basic.Request = r
	basic.Writer = w
	basic.server = s

	if err := basic.authenticate(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		s.instruments.Log(octo.LOGERROR, s.info.UUID, "httpbasic.BasicServeHTTP.ServeHTTP", "Authentication : Fails : Error : %+s", err)
		s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpbasic.BasicServeHTTP.ServeHTTP", "Completed")
		return
	}

	s.instruments.Log(octo.LOGTRANSMITTED, s.info.UUID, "httpbasic.BasicServeHTTP.ServeHTTP", "Started : %+q", data.Bytes())
	s.instruments.Log(octo.LOGTRANSMITTED, s.info.UUID, "httpbasic.BasicServeHTTP.ServeHTTP", "Completed")

	if err := s.primary.Serve(data.Bytes(), &basic); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		s.instruments.Log(octo.LOGERROR, s.info.UUID, "httpbasic.BasicServeHTTP.ServeHTTP", "BasicServer System : Error : %+s", err)
	}

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpbasic.BasicServeHTTP.ServeHTTP", "Completed")
}

//================================================================================

// BasicTransmission defines a struct to hold the request and response object.
// All HTTP Request are expected to use the 'Authorization' header which will contain
// the `SCHEME TOKEN:APIKEY:APIDATA'` which will then be used for authentication if
// the
type BasicTransmission struct {
	Request     *http.Request
	Writer      http.ResponseWriter
	ctx         context.Context
	server      *BasicServeHTTP
	system      stream.System
	info        octo.Contact
	instruments octo.Instrumentation
	buffer      bytes.Buffer
}

// authenticate runs the authentication procedure to authenticate that the connection
// was valid.
func (t *BasicTransmission) authenticate() error {
	t.instruments.Log(octo.LOGINFO, t.info.UUID, "httpbasic.BasicTransmission.authenticate", "Started")
	if !t.server.authenticate {
		t.instruments.Log(octo.LOGINFO, t.info.UUID, "httpbasic.BasicTransmission.authenticate", "Completed")
		return nil
	}

	authorizationHeader := t.Request.Header.Get("Authorization")
	if len(authorizationHeader) == 0 {
		return errors.New("'Authorization' header needed for authentication")
	}

	credentials, err := utils.ParseAuthorization(authorizationHeader)
	if err != nil {
		t.instruments.Log(octo.LOGERROR, t.info.UUID, "httpbasic.BasicTransmission.authenticate", "Completed : Error : %+q", err)
		return err
	}

	if err := t.system.Authenticate(credentials); err != nil {
		t.instruments.Log(octo.LOGERROR, t.info.UUID, "httpbasic.BasicTransmission.authenticate", "Completed : Error : %+q", err)
		return err
	}

	t.instruments.Log(octo.LOGINFO, t.info.UUID, "httpbasic.BasicTransmission.authenticate", "Completed")
	return nil
}

// SendAll pipes the giving data down the provided pipeline.
// This function call the BasicTransmission.Send function internally,
// as multiple requests is not supported.
func (t *BasicTransmission) SendAll(data []byte, flush bool) error {
	t.instruments.Log(octo.LOGINFO, t.info.UUID, "httpbasic.BasicTransmission.SendAll", "Started")
	if err := t.Send(data, flush); err != nil {
		t.instruments.Log(octo.LOGERROR, t.info.UUID, "httpbasic.BasicTransmission.SendAll", "Completed : %s", err.Error())
		return err
	}

	t.instruments.Log(octo.LOGINFO, t.info.UUID, "httpbasic.BasicTransmission.SendAll", "Completed")
	return nil
}

// Send pipes the giving data down the provided pipeline.
func (t *BasicTransmission) Send(data []byte, flush bool) error {
	t.instruments.Log(octo.LOGINFO, t.info.UUID, "httpbasic.BasicTransmission.Send", "Started")

	t.instruments.Log(octo.LOGTRANSMISSION, t.info.UUID, "httpbasic.BasicTransmission.Send", "Started : %+q", data)
	t.buffer.Write(data)
	t.instruments.Log(octo.LOGTRANSMISSION, t.info.UUID, "httpbasic.BasicTransmission.Send", "Completed")

	if !flush {
		t.instruments.Log(octo.LOGINFO, t.info.UUID, "httpbasic.BasicTransmission.Send", "Completed")
		return nil
	}

	if _, err := t.Writer.Write(t.buffer.Bytes()); err != nil {
		t.instruments.Log(octo.LOGERROR, t.info.UUID, "httpbasic.BasicTransmission.Send", "Completed : %s", err.Error())
		return err
	}

	t.instruments.Log(octo.LOGINFO, t.info.UUID, "httpbasic.BasicTransmission.Send", "Completed")
	return nil
}

// Contact returns the giving information for the internal client and server.
func (t *BasicTransmission) Contact() (octo.Contact, octo.Contact) {
	return t.info, t.server.info
}

// Ctx returns the context that is related to this object.
func (t *BasicTransmission) Ctx() context.Context {
	return t.ctx
}

// Close ends the internal conneciton.
func (t *BasicTransmission) Close() error {
	t.instruments.Log(octo.LOGINFO, t.info.UUID, "httpbasic.BasicTransmission.Close", "Started")
	t.instruments.Log(octo.LOGINFO, t.info.UUID, "httpbasic.BasicTransmission.Close", "Completed")
	return nil
}
