// Package websocket provides a simple package that implements a websocket protocol
// transmission for the octo.TranmissionProtocol interface. Which allows a uniform
// response cycle with a websocket based connection.
package websocket

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/influx6/faux/context"
	"github.com/influx6/octo"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/netutils"
	"github.com/influx6/octo/parsers/byteutils"
	"github.com/influx6/octo/parsers/jsonparser"
	"github.com/influx6/octo/transmission"
	"github.com/influx6/octo/transmission/systems/jsonsystem"
	"github.com/influx6/octo/utils"
	uuid "github.com/satori/go.uuid"
)

// RequestOriginValidator defines a function which validates a request origin for
// a websocket request.
type RequestOriginValidator func(*http.Request) bool

// SocketAttr defines a attribute struct for defining options for the WebsocketServer
// struct.
type SocketAttr struct {
	Addr               string
	Authenticate       bool
	Credential         octo.AuthCredential
	TLSConfig          *tls.Config
	MessageCompression bool
	OriginValidator    RequestOriginValidator
	Headers            http.Header
}

// SocketServer defines a struct implements the http.ServeHTTP interface which
// handles servicing http requests for websockets.
type SocketServer struct {
	Attr        SocketAttr
	instruments octo.Instrumentation
	info        octo.Info
	base        *BaseSocketServer
	server      *http.Server
	listener    net.Listener
	wg          sync.WaitGroup
	rl          sync.Mutex
	running     bool
	doClose     bool
}

// New returns a new instance of a SocketServer.
func New(attr SocketAttr, instruments octo.Instrumentation) *SocketServer {
	ip, port, _ := net.SplitHostPort(attr.Addr)
	if ip == "" || ip == consts.AnyIP {
		if realIP, err := netutils.GetMainIP(); err == nil {
			attr.Addr = net.JoinHostPort(realIP, port)
		}
	}

	var suuid = uuid.NewV4().String()

	var ws SocketServer
	ws.instruments = instruments
	ws.info = octo.Info{
		SUUID:  suuid,
		UUID:   suuid,
		Addr:   attr.Addr,
		Remote: attr.Addr,
	}

	return &ws
}

// Listen begins the initialization of the websocket server.
func (s *SocketServer) Listen(system transmission.System) error {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "websocket.SocketServer.Listen", "Started")

	if s.isRunning() {
		s.instruments.Log(octo.LOGINFO, s.info.UUID, "websocket.SocketServer.Listen", "Completed")
		return nil
	}

	listener, err := netutils.MakeListener("tcp", s.Attr.Addr, s.Attr.TLSConfig)
	if err != nil {
		s.instruments.Log(octo.LOGERROR, s.info.UUID, "websocket.SocketServer.Listen", "Initialize net.Listener failed : Error : %s", err.Error())
		return err
	}

	server, tlListener, err := netutils.NewHTTPServer(listener, s, s.Attr.TLSConfig)
	if err != nil {
		s.instruments.Log(octo.LOGERROR, s.info.UUID, "websocket.SocketServer.Listen", "Initialize net.Listener failed : Error : %s", err.Error())
		return err
	}

	s.base = NewBaseSocketServer(BaseSocketAttr{}, s.instruments, s.info, s, system)

	s.rl.Lock()
	{
		s.wg.Add(1)
		s.running = true
		s.server = server
		s.listener = tlListener
	}
	s.rl.Unlock()

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "websocket.SocketServer.Listen", "Completed")
	return nil
}

// Credential returns the crendentials for the giving server.
func (s *SocketServer) Credential() octo.AuthCredential {
	return s.Attr.Credential
}

// Close ends the websocket connection and ensures all requests are finished.
func (s *SocketServer) Close() error {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "websocket.SocketServer.Close", "Start")
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
		s.instruments.Log(octo.LOGERROR, s.info.UUID, "websocket.SocketServer.Close", "Completed : %+q", err.Error())
	}

	// Close all clients connections.
	s.base.Close()

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "websocket.SocketServer.Close", "Completed")
	return nil
}

// Wait awaits the closure of the giving client.
func (s *SocketServer) Wait() {
	s.wg.Wait()
}

// shouldClose returns true/false if the client should close.
func (s *SocketServer) shouldClose() bool {
	s.rl.Lock()
	defer s.rl.Unlock()
	return s.doClose
}

// isRunning returns true/false if the client is still running.
func (s *SocketServer) isRunning() bool {
	s.rl.Lock()
	defer s.rl.Unlock()
	return s.running
}

// ServeHTTP implements the http.ServeHTTP interface method to handle http request
// converted to websockets request.
func (s *SocketServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "websocket.SocketServer.ServeHTTP", "Started")

	if s.shouldClose() {
		s.instruments.Log(octo.LOGINFO, s.info.UUID, "websocket.SocketServer.ServeHTTP", "Completed")
		return
	}

	s.base.ServeHTTP(w, r)

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "websocket.SocketServer.ServeHTTP", "Completed")
}

//================================================================================

// BaseSocketAttr defines a attribute struct for defining options for the WebsocketServer
// struct.
type BaseSocketAttr struct {
	Authenticate       bool
	MessageCompression bool
	Headers            http.Header
	OriginValidator    RequestOriginValidator
}

// BaseSocketServer defines the struct which implements the core functionality of
// the websocket request handler and implements the http.Handler interface.
type BaseSocketServer struct {
	MessageCompression bool
	Attr               BaseSocketAttr
	instruments        octo.Instrumentation
	info               octo.Info
	system             transmission.System
	base               *transmission.BaseSystem
	cl                 sync.Mutex
	clients            []*Client
	upgrader           websocket.Upgrader
}

// NewBaseSocketServer returns a new instance of a BaseSocketServer.
func NewBaseSocketServer(attr BaseSocketAttr, instruments octo.Instrumentation, info octo.Info, credentials octo.Credentials, system transmission.System) *BaseSocketServer {
	var base BaseSocketServer
	base.instruments = instruments
	base.Attr = attr
	base.info = info
	base.system = system

	base.base = transmission.NewBaseSystem(
		system,
		jsonparser.JSON,
		instruments,
		jsonsystem.BaseHandlers(),
		jsonsystem.AuthHandlers(credentials, system),
	)

	base.upgrader = websocket.Upgrader{
		ReadBufferSize:    consts.MaxBufferSize,
		WriteBufferSize:   consts.MaxBufferSize,
		CheckOrigin:       attr.OriginValidator,
		EnableCompression: attr.MessageCompression,
	}

	return &base
}

// Clients returns the client list of the BaseSocketServer object.
func (s *BaseSocketServer) Clients() []*Client {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "websocket.BaseSocketServer.Clients", "Started")
	s.cl.Lock()
	defer s.cl.Unlock()

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "websocket.BasicSocketServer.Clients", "Completed")
	return s.clients[0:]
}

// Info returns the giving info struct for the giving socket server.
func (s *BaseSocketServer) Info() octo.Info {
	return s.info
}

// Close ends the websocket connection and ensures all requests are finished.
func (s *BaseSocketServer) Close() error {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "websocket.BaseSocketServer.Close", "Started")

	s.cl.Lock()
	defer s.cl.Lock()

	for _, client := range s.clients {
		go client.Close()
	}

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "websocket.BaseSocketServer.Close", "Completed")
	return nil
}

// ServeHTTP defines a method to serve and handle websocket requests.
func (s *BaseSocketServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "websocket.BaseSocketServer.ServeHTTP", "Started")

	var index int

	s.cl.Lock()
	{
		index = len(s.clients)
	}
	s.cl.Unlock()

	var cuuid = uuid.NewV4().String()

	var client Client
	client.server = s
	client.instruments = s.instruments
	client.index = index
	client.Request = r
	client.system = s.system
	client.primary = s.base
	client.info = octo.Info{
		UUID:   cuuid,
		SUUID:  s.info.SUUID,
		Addr:   r.RemoteAddr,
		Remote: r.RemoteAddr,
	}

	if err := client.authorizationByHeader(); err != nil {
		s.instruments.Log(octo.LOGINFO, s.info.UUID, "websocket.BaseSocketServer.ServeHTTP", "WebSocket Upgrade : %+q : %+q", r.RemoteAddr, err.Error())
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, s.Attr.Headers)
	if err != nil {
		s.instruments.Log(octo.LOGINFO, s.info.UUID, "websocket.BaseSocketServer.ServeHTTP", "WebSocket Upgrade : %+q : %+q", r.RemoteAddr, err.Error())
		return
	}

	client.Conn = conn

	if err := client.Listen(); err != nil {
		s.instruments.Log(octo.LOGINFO, s.info.UUID, "websocket.BaseSocketServer.ServeHTTP", "WebSocket Upgrade : %+q : %+q", r.RemoteAddr, err.Error())
		return
	}

	s.cl.Lock()
	{
		s.clients = append(s.clients, &client)
	}
	s.cl.Unlock()

	client.Wait()

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "websocket.BaseSocketServer.ServeHTTP", "Completed")
}

//================================================================================

// Client defines a struct to defines a websocket client connection for managing
// each request client.
type Client struct {
	*websocket.Conn
	Request     *http.Request
	instruments octo.Instrumentation
	info        octo.Info
	system      transmission.System
	primary     *transmission.BaseSystem
	wg          sync.WaitGroup
	sg          sync.WaitGroup
	server      *BaseSocketServer
	cl          sync.Mutex
	index       int
	running     bool
	doClose     bool
}

// authenticate runs the authentication procedure to authenticate that the connection
// was valid.
func (c *Client) authorizationByRequest() error {
	c.instruments.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.authenticate", "Started")
	if !c.server.Attr.Authenticate {
		c.instruments.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.authenticate", "Completed")
		return nil
	}

	var cmd octo.Command
	cmd.Name = consts.AuthRequest

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(cmd); err != nil {
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "websocket.Client.authenticate", "Completed : Error : %q", err.Error())
		return nil
	}

	if err := c.Send(buf.Bytes(), true); err != nil {
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "websocket.Client.authenticate", "Completed : Error : %q", err.Error())
		return nil
	}

	c.instruments.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.authenticate", "Completed")
	return nil
}

// authorizationByHeader runs the authentication procedure to authenticate the connection
// by using the Authorization header present in the request object was valid.
func (c *Client) authorizationByHeader() error {
	c.instruments.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.authenticate", "Started")
	if !c.server.Attr.Authenticate {
		c.instruments.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.authenticate", "Completed")
		return nil
	}

	authorizationHeader := c.Request.Header.Get("Authorization")
	if len(authorizationHeader) == 0 {
		err := errors.New("'Authorization' header needed for authentication")
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "websocket.Client.authenticate", "Completed : Error : %+q", err)
		return err
	}

	credentials, err := utils.ParseAuthorization(authorizationHeader)
	if err != nil {
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "websocket.Client.authenticate", "Completed : Error : %+q", err)
		return err
	}

	if err := c.system.Authenticate(credentials); err != nil {
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "websocket.Client.authenticate", "Completed : Error : %+q", err)
		return err
	}

	c.instruments.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.authenticate", "Completed")
	return nil
}

// Wait awaits the closure of the giving client.
func (c *Client) Wait() {
	c.wg.Wait()
}

// SendAll delivers a binary data to all websocket connections.
func (c *Client) SendAll(data []byte, flush bool) error {
	c.instruments.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.SendAll", "Started")

	if err := c.Send(data, flush); err != nil {
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "websocket.Client.SendAll", "Started : Error : %s", err.Error())
		return err
	}

	clients := c.server.Clients()
	for _, client := range clients {
		if client == c {
			continue
		}

		if err := client.Send(data, flush); err != nil {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "websocket.Client.SendAll", "Started : %q : Error : %s", client.info.UUID, err.Error())
		}
	}

	c.instruments.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.SendAll", "Started")
	return nil
}

// Send delivers a binary data to the websocket connection.
func (c *Client) Send(data []byte, flush bool) error {
	c.instruments.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.Send", "Started")

	if c.shouldClose() {
		err := errors.New("Connection will be closed")
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "websocket.Client.Send", "Completed : Error : %s", err.Error())
		return err
	}

	c.sg.Add(1)
	defer c.sg.Done()

	c.instruments.Log(octo.LOGTRANSMISSION, c.info.UUID, "websocket.Client.Send", "Started : %+q", data)
	err := c.Conn.WriteMessage(websocket.BinaryMessage, data)
	c.instruments.Log(octo.LOGTRANSMISSION, c.info.UUID, "websocket.Client.Send", "Completed")

	if err != nil {
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "websocket.Client.Send", "Started : %q : Error : %s", c.info.UUID, err.Error())
		return err
	}

	c.instruments.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.Send", "Started")
	return nil
}

// Close ends the websocket connection and ensures all requests are finished.
func (c *Client) Close() error {
	if !c.isRunning() {
		return nil
	}

	c.cl.Lock()
	{
		c.running = false
		c.doClose = true
	}
	c.cl.Unlock()

	c.sg.Wait()

	if err := c.Conn.Close(); err != nil {
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "websocket.Client.Close", "Closing Websocket Connection : Error : %q", err.Error())
	}

	c.server.cl.Lock()
	{
		if len(c.server.clients) == 0 {
			c.server.clients = nil
			c.server.cl.Unlock()
			return nil
		}

		if len(c.server.clients) == 1 {
			c.server.clients = nil
			c.server.cl.Unlock()
			return nil
		}

		c.server.clients = append(c.server.clients[:c.index], c.server.clients[c.index+1:]...)
	}
	c.server.cl.Unlock()

	return nil
}

// Listen starts the giving client and begins reading the websocket connection.
func (c *Client) Listen() error {
	c.instruments.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.Listen", "Start : %#v", c.info)

	if c.system == nil || c.primary == nil {
		err := errors.New("Client system/Primary systems are not set")
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "websocket.Client.Listen", "Websocket Unable to Listen : Error : %q", err.Error())
		return err
	}

	c.cl.Lock()
	{
		c.running = true
	}
	c.cl.Unlock()

	c.wg.Add(1)
	go c.acceptRequests()

	c.instruments.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.Listen", "Completed")
	return nil
}

// acceptRequests handles the processing of requests from the server.
func (c *Client) acceptRequests() {
	c.instruments.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.acceptRequests", "Started")
	defer c.wg.Done()

	for c.isRunning() {
		if c.shouldClose() {
			break
		}

		messageType, message, err := c.Conn.ReadMessage()
		c.instruments.Log(octo.LOGTRANSMITTED, c.info.UUID, "websocket.Client.acceptRequests", "Type: %d, Message: %+q", messageType, message)
		c.instruments.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.acceptRequests", "Completed")
		if err != nil {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "websocket.Client.acceptRequests", "Error : %q", err.Error())

			if err == io.EOF || err == websocket.ErrBadHandshake || err == websocket.ErrCloseSent {
				go c.Close()
				break
			}

			if messageType == -1 && strings.Contains(err.Error(), "websocket: close") {
				go c.Close()
				break
			}
		}

		var data []byte
		switch messageType {
		case websocket.BinaryMessage:
			data = message
		case websocket.TextMessage:
			data = message
		case websocket.CloseMessage:
			go c.Close()
			break
		default:
			continue
		}

		var tx Transmission
		tx.ctx = context.New()
		tx.client = c

		rem, err := c.primary.ServeBase(data, &tx)
		if err != nil {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "websocket.Server.acceptRequests", "Websocket Base System : Fails Parsing : Error : %+s", err)

			if err := c.system.Serve(data, &tx); err != nil {
				c.instruments.Log(octo.LOGERROR, c.info.UUID, "websocket.Server.acceptRequests", "Websocket Base System : Fails Parsing : Error : %+s", err)
				go c.Close()
				return
			}
		}

		// Handle remaining messages and pass it to user system.
		if rem != nil {
			if err := c.system.Serve(byteutils.JoinMessages(rem...), &tx); err != nil {
				c.instruments.Log(octo.LOGERROR, c.info.UUID, "websocket.Server.acceptRequests", "Websocket Base System : Fails Parsing : Error : %+s", err)
				go c.Close()
				return
			}
		}
	}

	c.instruments.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.acceptRequests", "Completed")
}

// shouldClose returns true/false if the client should close.
func (c *Client) shouldClose() bool {
	c.cl.Lock()
	defer c.cl.Unlock()
	return c.doClose
}

// isRunning returns true/false if the client is still running.
func (c *Client) isRunning() bool {
	c.cl.Lock()
	defer c.cl.Unlock()
	return c.running
}

//================================================================================

// Transmission defines a struct for handling responses from a transmission.System object.
type Transmission struct {
	client *Client
	ctx    context.Context
}

// SendAll pipes the giving data down the provided pipeline.
func (t *Transmission) SendAll(data []byte, flush bool) error {
	return t.client.SendAll(data, flush)
}

// Send pipes the giving data down the provided pipeline.
func (t *Transmission) Send(data []byte, flush bool) error {
	return t.client.Send(data, flush)
}

// Info returns the giving information for the internal client and server.
func (t *Transmission) Info() (octo.Info, octo.Info) {
	return t.client.info, t.client.server.info
}

// Ctx returns the context that is related to this object.
func (t *Transmission) Ctx() context.Context {
	return t.ctx
}

// Close ends the internal conneciton.
func (t *Transmission) Close() error {
	go t.client.Close()
	return nil
}
