// Package http provides a server which implements server push system provided
// by the new go http package.
package http

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dimfeld/httptreemux"
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
	SkipCORS     bool
	Headers      http.Header
	TLSConfig    *tls.Config
	Auth         octo.AuthCredential
}

// BasicServer defines a struct implements the http.ServeHTTP interface which
// handles servicing http requests for websockets.
type BasicServer struct {
	pub         *stream.Pub
	Attr        BasicAttr
	instruments octo.Instrumentation
	info        octo.Contact
	server      *http.Server
	listener    net.Listener
	wg          sync.WaitGroup
	basic       *BasicServeHTTP
	tree        *httptreemux.TreeMux
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
	ws.pub = stream.NewPub()
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

// Register registers the handler for a given handler.
func (s *BasicServer) Register(tm stream.StateHandlerType, hmi interface{}) {
	s.pub.Register(tm, hmi)
}

// Listen begins the initialization of the websocket server.
func (s *BasicServer) Listen(system stream.System) error {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "http.BasicServer.Listen", "Started")

	if s.isRunning() {
		s.instruments.Log(octo.LOGINFO, s.info.UUID, "http.BasicServer.Listen", "Completed")
		return nil
	}

	listener, err := netutils.MakeListener("tcp", s.Attr.Addr, s.Attr.TLSConfig)
	if err != nil {
		s.instruments.Log(octo.LOGERROR, s.info.UUID, "http.BasicServer.Listen", "Initialize net.Listener failed : Error : %s", err.Error())
		return err
	}

	server, tlListener, err := netutils.NewHTTPServer(listener, s, s.Attr.TLSConfig)
	if err != nil {
		s.instruments.Log(octo.LOGERROR, s.info.UUID, "http.BasicServer.Listen", "Initialize net.Listener failed : Error : %s", err.Error())
		return err
	}

	s.basic = NewBasicServeHTTP(BasicServeAttr{
		Authenticate: s.Attr.Authenticate,
		SkipCORS:     s.Attr.SkipCORS,
		Instruments:  s.instruments,
		Pub:          s.pub,
		Info:         s.info,
		Auth:         s,
		System:       system,
	})

	s.rl.Lock()
	{
		s.running = true
		s.server = server
		s.listener = tlListener
		s.wg.Add(1)
	}
	s.rl.Unlock()

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "http.BasicServer.Listen", "Completed")
	return nil
}

// ServeHTTP implements the http.Handler.ServeHTTP interface method to handle http request
// converted to websockets request.
func (s *BasicServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "http.BasicServer.ServeHTTP", "Started")

	if s.shouldClose() {
		s.instruments.Log(octo.LOGINFO, s.info.UUID, "http.BasicServer.ServeHTTP", "Completed")
		return
	}

	// Call basic to treat the request.
	s.basic.ServeHTTP(w, r)

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "http.BasicServer.ServeHTTP", "Completed")
}

// Close ends the websocket connection and ensures all requests are finished.
func (s *BasicServer) Close() error {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "http.BasicServer.Close", "Start")
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
		s.instruments.Log(octo.LOGERROR, s.info.UUID, "http.BasicServer.Close", "Completed : %+q", err.Error())
	}

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "http.BasicServer.Close", "Completed")
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

// BasicServeAttr defines a attribute which holds configuration fields for a
// BasicServeHTTP.
type BasicServeAttr struct {
	Authenticate bool
	SkipCORS     bool
	Instruments  octo.Instrumentation
	Info         octo.Contact
	Auth         octo.Credentials
	System       stream.System
	Pub          *stream.Pub
}

// BasicServeHTTP provides a structure which implements the http.Handler and provides
// the core server which implements the needed functionality to use the streams.System
// interface. It is provided as a seperate module to allow flexibility with user
// created server and routes for http requests.
type BasicServeHTTP struct {
	Attr        BasicServeAttr
	primary     *jsoni.SxConversations
	instruments octo.Instrumentation
}

// NewBasicServeHTTP returns a new instance of the BasicServeHTTP object.
func NewBasicServeHTTP(attr BasicServeAttr) *BasicServeHTTP {
	primary := jsoni.NewSxConversations(attr.System, server.CloseServer{}, server.ContactServer{}, server.ConversationServer{}, &server.AuthServer{Credentials: attr.Auth})

	return &BasicServeHTTP{
		Attr:        attr,
		primary:     primary,
		instruments: attr.Instruments,
	}
}

// Contact returns the info object associated with this server.
func (s *BasicServeHTTP) Contact() octo.Contact {
	return s.Attr.Info
}

// ServeHTTP implements the http.Handler.ServeHTTP interface method to handle http request
// converted to websockets request.
func (s *BasicServeHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.instruments.Log(octo.LOGINFO, s.Attr.Info.UUID, "http.BasicServeHTTP.ServeHTTP", "Started")

	if !s.Attr.SkipCORS {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Max-Age", "86400")
	}

	switch strings.ToLower(r.Method) {
	case "options":
		w.WriteHeader(http.StatusNoContent)
		return
	default:
		s.instruments.Log(octo.LOGINFO, s.Attr.Info.UUID, "http.BasicServeHTTP.ServeHTTP", "New Request : %q : %q", r.Method, r.URL.Path)
	}

	w.Header().Add("Connection", "keep-alive")

	var data bytes.Buffer

	if r.Body != nil {
		io.Copy(&data, r.Body)
		r.Body.Close()
	}

	var cuuid = uuid.NewV4().String()

	var basic BasicTransmission
	basic.pub = s.Attr.Pub
	basic.ctx = context.NewGoogleContext(r.Context())
	basic.info = octo.Contact{
		UUID:   cuuid,
		SUUID:  s.Attr.Info.SUUID,
		Local:  s.Attr.Info.Addr,
		Addr:   r.RemoteAddr,
		Remote: r.RemoteAddr,
	}

	basic.instruments = s.instruments
	basic.system = s.Attr.System
	basic.Request = r
	basic.Writer = w
	basic.server = s

	basic.pub.Notify(stream.ConnectHandler, basic.info, &basic, nil)

	if err := basic.authenticate(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		s.instruments.Log(octo.LOGERROR, s.Attr.Info.UUID, "http.BasicServeHTTP.ServeHTTP", "Authentication : Fails : Error : %+s", err)
		s.instruments.Log(octo.LOGINFO, s.Attr.Info.UUID, "http.BasicServeHTTP.ServeHTTP", "Completed")
		return
	}

	s.instruments.Log(octo.LOGTRANSMITTED, s.Attr.Info.UUID, "http.BasicServeHTTP.ServeHTTP", "Started : %+q", data.Bytes())
	s.instruments.Log(octo.LOGTRANSMITTED, s.Attr.Info.UUID, "http.BasicServeHTTP.ServeHTTP", "Completed")

	if err := s.primary.Serve(data.Bytes(), &basic); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		s.instruments.Log(octo.LOGERROR, s.Attr.Info.UUID, "http.BasicServeHTTP.ServeHTTP", "BasicServer System : Error : %+s", err)
	}

	basic.pub.Notify(stream.DisconnectHandler, basic.info, &basic, nil)
	s.instruments.Log(octo.LOGINFO, s.Attr.Info.UUID, "http.BasicServeHTTP.ServeHTTP", "Completed")
}

//================================================================================

// BasicTransmission defines a struct to hold the request and response object.
// All HTTP Request are expected to use the 'Authorization' header which will contain
// the `SCHEME TOKEN:APIKEY:APIDATA'` which will then be used for authentication if
// the
type BasicTransmission struct {
	pub         *stream.Pub
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
	t.instruments.Log(octo.LOGINFO, t.info.UUID, "http.BasicTransmission.authenticate", "Started")
	if !t.server.Attr.Authenticate {
		t.instruments.Log(octo.LOGINFO, t.info.UUID, "http.BasicTransmission.authenticate", "Completed")
		return nil
	}

	authorizationHeader := t.Request.Header.Get("Authorization")
	if len(authorizationHeader) == 0 {
		err := errors.New("'Authorization' header needed for authentication")
		t.pub.Notify(stream.ErrorHandler, t.info, t, err)
		return err
	}

	credentials, err := utils.ParseAuthorization(authorizationHeader)
	if err != nil {
		t.instruments.Log(octo.LOGERROR, t.info.UUID, "http.BasicTransmission.authenticate", "Completed : Error : %+q", err)
		t.pub.Notify(stream.ErrorHandler, t.info, t, err)
		return err
	}

	if err := t.system.Authenticate(credentials); err != nil {
		t.instruments.Log(octo.LOGERROR, t.info.UUID, "http.BasicTransmission.authenticate", "Completed : Error : %+q", err)
		t.pub.Notify(stream.ErrorHandler, t.info, t, err)
		return err
	}

	t.instruments.Log(octo.LOGINFO, t.info.UUID, "http.BasicTransmission.authenticate", "Completed")
	return nil
}

// SendAll pipes the giving data down the provided pipeline.
// This function call the BasicTransmission.Send function internally,
// as multiple requests is not supported.
func (t *BasicTransmission) SendAll(data []byte, flush bool) error {
	t.instruments.Log(octo.LOGINFO, t.info.UUID, "http.BasicTransmission.SendAll", "Started")
	if err := t.Send(data, flush); err != nil {
		t.instruments.Log(octo.LOGERROR, t.info.UUID, "http.BasicTransmission.SendAll", "Completed : %s", err.Error())
		return err
	}

	t.instruments.Log(octo.LOGINFO, t.info.UUID, "http.BasicTransmission.SendAll", "Completed")
	return nil
}

// Send pipes the giving data down the provided pipeline.
func (t *BasicTransmission) Send(data []byte, flush bool) error {
	t.instruments.Log(octo.LOGINFO, t.info.UUID, "http.BasicTransmission.Send", "Started")

	t.instruments.Log(octo.LOGTRANSMISSION, t.info.UUID, "http.BasicTransmission.Send", "Started : %+q", data)
	t.buffer.Write(data)
	t.instruments.Log(octo.LOGTRANSMISSION, t.info.UUID, "http.BasicTransmission.Send", "Completed")

	if !flush {
		t.instruments.Log(octo.LOGINFO, t.info.UUID, "http.BasicTransmission.Send", "Completed")
		return nil
	}

	if _, err := t.Writer.Write(t.buffer.Bytes()); err != nil {
		t.instruments.Log(octo.LOGERROR, t.info.UUID, "http.BasicTransmission.Send", "Completed : %s", err.Error())
		return err
	}

	t.instruments.Log(octo.LOGINFO, t.info.UUID, "http.BasicTransmission.Send", "Completed")
	return nil
}

// Contact returns the giving information for the internal client and server.
func (t *BasicTransmission) Contact() (octo.Contact, octo.Contact) {
	return t.info, t.server.Attr.Info
}

// Ctx returns the context that is related to this object.
func (t *BasicTransmission) Ctx() context.Context {
	return t.ctx
}

// Close ends the internal conneciton.
func (t *BasicTransmission) Close() error {
	t.instruments.Log(octo.LOGINFO, t.info.UUID, "http.BasicTransmission.Close", "Started")
	t.instruments.Log(octo.LOGINFO, t.info.UUID, "http.BasicTransmission.Close", "Completed")
	return nil
}

//================================================================================

// SSEMaster defines a structure which manages multiple SSE(Server-Side Event)
// and ensures data is being delivered to all.
type SSEMaster struct {
	base        octo.Contact
	auth        octo.Authenticator
	instruments octo.Instrumentation
	pub         *stream.Pub
	waiter      sync.WaitGroup
	header      map[string]string
	data        chan []byte
	closer      chan struct{}
	newClient   chan chan []byte
	closeClient chan chan []byte
	channels    map[chan []byte]bool
	cl          sync.Mutex
	closed      bool
}

// NewSSEMaster returns a new instance of a SSEMaster.
func NewSSEMaster(inst octo.Instrumentation, base octo.Contact, auth octo.Authenticator, header map[string]string) *SSEMaster {
	var master SSEMaster
	master.base = base
	master.auth = auth
	master.header = header
	master.instruments = inst
	master.pub = stream.NewPub()
	master.data = make(chan []byte, 0)
	master.closer = make(chan struct{}, 0)
	master.newClient = make(chan chan []byte, 0)
	master.closeClient = make(chan chan []byte, 0)
	master.channels = make(map[chan []byte]bool, 0)

	// Spin up the management routine.
	go master.manage()

	return &master
}

// Register registers the handler for a given handler.
func (s *SSEMaster) Register(tm stream.StateHandlerType, hmi interface{}) {
	s.pub.Register(tm, hmi)
}

// Close ends all connections and awaits all stopping of clients gorountines.
func (s *SSEMaster) Close() error {
	s.instruments.Log(octo.LOGINFO, s.base.UUID, "http.SSEMaster.Close", "Started")
	s.cl.Lock()
	if s.closed {
		s.cl.Unlock()
		s.instruments.Log(octo.LOGINFO, s.base.UUID, "http.SSEMaster.Close", "Completed")
		return consts.ErrClosedConnection
	}

	close(s.closer)

	s.cl.Lock()
	s.closed = true
	s.cl.Unlock()

	s.waiter.Wait()

	s.instruments.Log(octo.LOGINFO, s.base.UUID, "http.SSEMaster.Close", "Completed")
	return nil
}

// manage runs the underline core
func (s *SSEMaster) manage() {
	{
	mloop:
		for {
			select {
			case <-time.After(5 * time.Second):
				continue

			case <-s.closer:
				break mloop

			case c, ok := <-s.closeClient:
				if !ok {
					break mloop
				}

				delete(s.channels, c)
				continue

			case c, ok := <-s.newClient:
				if !ok {
					break mloop
				}

				s.channels[c] = true
				continue

			case data, ok := <-s.data:
				if !ok {
					break mloop
				}

				for client := range s.channels {
					select {
					case client <- data:
						// Data sent to client
					case <-time.After(3 * time.Second):
						// Data not consumed by client
					}
				}
			}
		}
	}
}

// SendAll delivers data to all subscribed clients.
func (s *SSEMaster) SendAll(data []byte, flush bool) error {
	s.instruments.Log(octo.LOGINFO, s.base.UUID, "http.SSEMaster.SendAll", "Started : {%+q}", data)

	if err := s.Send(data, flush); err != nil {
		s.instruments.Log(octo.LOGERROR, s.base.UUID, "http.SSEMaster.SendAll", "Completed : %+q", err)
		return err
	}

	s.instruments.Log(octo.LOGINFO, s.base.UUID, "http.SSEMaster.SendAll", "Completed")
	return nil
}

// Send delivers a giving data to all clients within the connected
func (s *SSEMaster) Send(data []byte, flush bool) error {
	s.instruments.Log(octo.LOGINFO, s.base.UUID, "http.SSEMaster.Send", "Started : {%+q}", data)
	s.cl.Lock()
	if s.closed {
		s.cl.Unlock()
		return consts.ErrClosedConnection
	}

	s.data <- data

	s.instruments.Log(octo.LOGINFO, s.base.UUID, "http.SSEMaster.Send", "Completed")
	return nil
}

// ServeHTTP generates a new client for the server sent events based on the
func (s *SSEMaster) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.instruments.Log(octo.LOGINFO, s.base.UUID, "http.SSEMaster.ServeHTTP", "Started : %q", r.RemoteAddr)

	// Add headers.
	for key, value := range s.header {
		w.Header().Add(key, value)
	}

	var contact octo.Contact
	contact.UUID = uuid.NewV4().String()
	contact.SUUID = s.base.SUUID
	contact.Addr = r.RemoteAddr
	contact.Local = s.base.Addr
	contact.Remote = r.RemoteAddr

	data := make(chan []byte)

	s.newClient <- data

	// Create new sse client.
	newClient := NewSSEClient(s.instruments, s.auth, s.pub, s, contact, data, s.closer)

	s.pub.Notify(stream.ConnectHandler, contact, newClient, nil)

	s.waiter.Add(1)
	defer s.waiter.Done()

	// Start serving the requests.
	newClient.ServeHTTP(w, r)

	// Remove giving channel from channel list.
	s.closeClient <- data

	s.pub.Notify(stream.DisconnectHandler, contact, newClient, nil)

	s.instruments.Log(octo.LOGINFO, s.base.UUID, "http.SSEMaster.ServeHTTP", "Completed")
}

//================================================================================

// SSEClient defines a struct which implements the http.Handler interface for
// handling http server-sent requests.
type SSEClient struct {
	Data          bytes.Buffer
	pub           *stream.Pub
	master        *SSEMaster
	info          octo.Contact
	notifications chan []byte
	closer        chan struct{}
	auth          octo.Authenticator
	instruments   octo.Instrumentation
}

// NewSSEClient returns a new instance of a SSEServer.
func NewSSEClient(instruments octo.Instrumentation, auth octo.Authenticator, pub *stream.Pub, sm *SSEMaster, info octo.Contact, notifications chan []byte, closer chan struct{}) *SSEClient {
	return &SSEClient{
		pub:           pub,
		master:        sm,
		auth:          auth,
		notifications: notifications,
		closer:        closer,
		info:          info,
		instruments:   instruments,
	}
}

// Close does nothing.
func (s *SSEClient) Close() error {
	return nil
}

// SendAll delivers data to all subscribed clients.
func (s *SSEClient) SendAll(data []byte, flush bool) error {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "http.SSEClient.SendAll", "Started : {%+q}", data)

	if err := s.master.SendAll(data, flush); err != nil {
		s.instruments.Log(octo.LOGERROR, s.info.UUID, "http.SSEClient.SendAll", "Completed : %+q", err)
		return err
	}

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "http.SSEClient.SendAll", "Completed")
	return nil
}

// Send delivers a giving data to all clients within the connected
func (s *SSEClient) Send(data []byte, flush bool) error {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "http.SSEClient.Send", "Started : {%+q}", data)

	s.notifications <- data

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "http.SSEClient.Send", "Completed")
	return nil
}

// authenticate runs the authentication procedure to authenticate that the connection
// was valid.
func (s *SSEClient) authenticate(request *http.Request) error {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "http.SSEClient.authenticate", "Started")
	if s.auth == nil {
		s.instruments.Log(octo.LOGINFO, s.info.UUID, "http.SSEClient.authenticate", "Completed : No Authentication Required")
		return nil
	}

	authorizationHeader := request.Header.Get("Authorization")
	if len(authorizationHeader) == 0 {
		err := errors.New("'Authorization' header needed for authentication")
		s.instruments.Log(octo.LOGERROR, s.info.UUID, "http.SSEClient.authenticate", "Completed : Error : %+q", err)

		s.pub.Notify(stream.ErrorHandler, s.info, s, err)
		return err
	}

	credentials, err := utils.ParseAuthorization(authorizationHeader)
	if err != nil {
		s.instruments.Log(octo.LOGERROR, s.info.UUID, "http.SSEClient.authenticate", "Completed : Error : %+q", err)

		s.pub.Notify(stream.ErrorHandler, s.info, s, err)
		return err
	}

	if err := s.auth.Authenticate(credentials); err != nil {
		s.instruments.Log(octo.LOGERROR, s.info.UUID, "http.SSEClient.authenticate", "Completed : Error : %+q", err)

		s.pub.Notify(stream.ErrorHandler, s.info, s, err)
		return err
	}

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "http.SSEClient.authenticate", "Completed")
	return nil
}

// ServeHTTP implements the http.Handler.ServeHTTP interface method to handle http request
// converted to websockets request.
func (s *SSEClient) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpsee.SSEClient.ServeHTTP", "Started")

	var data bytes.Buffer

	if r.Body != nil {
		io.Copy(&data, r.Body)
		r.Body.Close()
	}

	s.Data = data

	if err := s.authenticate(r); err != nil {
		s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpsee.WeaveServer.ServeHTTP", "Completed : Error : Failed to autnethicate : %+q", err)
		http.Error(w, "Http Streaming not supported", http.StatusInternalServerError)
		// s.pub.Notify(stream.ErrorHandler, s.info, s, err)
		return
	}

	if err := s.authenticate(r); err != nil {
		s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpsee.SSEClient.ServeHTTP", "Completed : Error : Failed to autnethicate : %+q", err)
		http.Error(w, "Http Streaming not supported", http.StatusInternalServerError)
		// s.pub.Notify(stream.ErrorHandler, s.info, s, err)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpsee.SSEClient.ServeHTTP", "Completed : Error : No Support for http.Flusher")
		http.Error(w, "Http Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Set the headers related to event streaming.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Parse the form and validate if we are expected to send the received
	// data as raw data or not.
	r.ParseForm()
	raw := len(r.Form["raw"]) > 0

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpsee.SSEClient.ServeHTTP", "Init : Message Loop : Client : %+q : %+q", r.RemoteAddr, r.URL.Path)

	closable, ok := w.(http.CloseNotifier)
	if !ok {
		s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpsee.SSEClient.ServeHTTP", "Completed : Error : No Support for http.Flusher")
		http.Error(w, "Close Notification not supported in response", http.StatusInternalServerError)
		return
	}

	closer := closable.CloseNotify()

	{
	messageLoop:
		for {
			select {
			case <-s.closer:
				s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpsee.SSEClient.ServeHTTP", "Closing : Server requests end : Client : %+q : %+q", r.RemoteAddr, r.URL.Path)
				break messageLoop
			case <-closer:
				s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpsee.SSEClient.ServeHTTP", "Closing : Client requests end : Client : %+q : %+q", r.RemoteAddr, r.URL.Path)
				break messageLoop

			case data, ok := <-s.notifications:
				if !ok {
					s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpsee.SSEClient.ServeHTTP", "Closing : Notifications Channel requests end : Client : %+q : %+q", r.RemoteAddr, r.URL.Path)
					break messageLoop
				}

				if !raw {
					s.instruments.NotifyEvent(octo.Event{
						Type:       octo.DataWrite,
						Client:     s.info.UUID,
						Server:     s.info.SUUID,
						LocalAddr:  s.info.Local,
						RemoteAddr: s.info.Remote,
						Data:       octo.NewDataInstrument(data, nil),
						Details:    map[string]interface{}{},
					})

					fmt.Fprintf(w, "%s\n\n", data)

					// Flush data immedaitely to client.
					flusher.Flush()
					continue messageLoop
				}

				s.instruments.NotifyEvent(octo.Event{
					Type:       octo.DataWrite,
					Client:     s.info.UUID,
					Server:     s.info.SUUID,
					LocalAddr:  s.info.Local,
					RemoteAddr: s.info.Remote,
					Data:       octo.NewDataInstrument([]byte(fmt.Sprintf("data: %+q\r\n", data)), nil),
					Details:    map[string]interface{}{},
				})

				fmt.Fprintf(w, "data: %s\n\n", data)

				// Flush data immedaitely to client.
				flusher.Flush()
			}
		}
	}

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpsee.SSEClient.ServeHTTP", "Completed")
}
