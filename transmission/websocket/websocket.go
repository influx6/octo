// Package websocket provides a simple package that implements a websocket protocol
// transmission for the octo.TranmissionProtocol interface. Which allows a uniform
// response cycle with a websocket based connection.
package websocket

import (
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/influx6/faux/context"
	"github.com/influx6/octo"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/netutils"
	"github.com/influx6/octo/parsers/byteutils"
	"github.com/influx6/octo/parsers/jsonparser"
	"github.com/influx6/octo/systems/jsonsystem"
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
	Headers            http.Header
	Credential         octo.AuthCredential
	TLSConfig          *tls.Config
	OriginValidator    RequestOriginValidator
	MessageCompression bool
}

// SocketServer defines a struct implements the http.ServeHTTP interface which
// handles servicing http requests for websockets.
type SocketServer struct {
	Attr     SocketAttr
	log      octo.Logs
	info     octo.Info
	system   octo.System
	base     *octo.BaseSystem
	upgrader websocket.Upgrader
	server   *http.Server
	listener net.Listener
	wg       sync.WaitGroup
	cl       sync.Mutex
	clients  []*Client
	rl       sync.Mutex
	running  bool
	doClose  bool
}

// New returns a new instance of a SocketServer.
func New(attr SocketAttr) *SocketServer {
	ip, port, _ := net.SplitHostPort(attr.Addr)
	if ip == "" || ip == consts.AnyIP {
		if realIP, err := netutils.GetMainIP(); err == nil {
			attr.Addr = net.JoinHostPort(realIP, port)
		}
	}

	var suuid = uuid.NewV4().String()

	var ws SocketServer
	ws.info = octo.Info{
		SUUID:  suuid,
		UUID:   suuid,
		Addr:   attr.Addr,
		Remote: attr.Addr,
	}

	ws.upgrader = websocket.Upgrader{
		ReadBufferSize:    consts.MaxBufferSize,
		WriteBufferSize:   consts.MaxBufferSize,
		CheckOrigin:       attr.OriginValidator,
		EnableCompression: attr.MessageCompression,
	}

	return &ws
}

// Listen begins the initialization of the websocket server.
func (s *SocketServer) Listen(system octo.System) error {
	s.log.Log(octo.LOGINFO, s.info.UUID, "websocket.SocketServer.Listen", "Started")

	if s.isRunning() {
		s.log.Log(octo.LOGINFO, s.info.UUID, "websocket.SocketServer.Listen", "Completed")
		return nil
	}

	listener, err := netutils.MakeListener("tcp", s.Attr.Addr, s.Attr.TLSConfig)
	if err != nil {
		s.log.Log(octo.LOGERROR, s.info.UUID, "websocket.SocketServer.Listen", "Initialize net.Listener failed : Error : %s", err.Error())
		return err
	}

	s.system = system
	s.base = octo.NewBaseSystem(system, jsonparser.JSON, s.log, jsonsystem.BaseHandlers(), jsonsystem.AuthHandlers(s))

	server, tlListener, err := netutils.NewHTTPServer(listener, s, s.Attr.TLSConfig)
	if err != nil {
		s.log.Log(octo.LOGERROR, s.info.UUID, "websocket.SocketServer.Listen", "Initialize net.Listener failed : Error : %s", err.Error())
		return err
	}

	s.rl.Lock()
	{
		s.running = true
		s.server = server
		s.listener = tlListener
		s.wg.Add(1)
	}
	s.rl.Unlock()

	s.log.Log(octo.LOGINFO, s.info.UUID, "websocket.SocketServer.Listen", "Completed")
	return nil
}

// Credential returns the crendentials for the giving server.
func (s *SocketServer) Credential() octo.AuthCredential {
	return s.Attr.Credential
}

// Clients returns the clients list of the socketServer.
func (s *SocketServer) Clients() []*Client {
	s.log.Log(octo.LOGINFO, s.info.UUID, "websocket.SocketServer.Clients", "Started")
	s.cl.Lock()
	defer s.cl.Unlock()

	s.log.Log(octo.LOGINFO, s.info.UUID, "websocket.SocketServer.Clients", "Completed")
	return s.clients[0:]
}

// ServeHTTP implements the http.ServeHTTP interface method to handle http request
// converted to websockets request.
func (s *SocketServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.log.Log(octo.LOGINFO, s.info.UUID, "websocket.SocketServer.ServeHTTP", "Started")

	if s.shouldClose() {
		s.log.Log(octo.LOGINFO, s.info.UUID, "websocket.SocketServer.ServeHTTP", "Completed")
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, s.Attr.Headers)
	if err != nil {
		s.log.Log(octo.LOGINFO, s.info.UUID, "websocket.SocketServer.ServeHTTP", "WebSocket Upgrade : %+q : %+q", r.RemoteAddr, err.Error())
		return
	}

	var cuuid = uuid.NewV4().String()

	var client Client
	client.Conn = conn
	client.system = s.system
	client.info = octo.Info{
		UUID:   cuuid,
		SUUID:  s.info.SUUID,
		Addr:   r.RemoteAddr,
		Remote: r.RemoteAddr,
	}

	if err := client.Listen(); err != nil {
		s.log.Log(octo.LOGINFO, s.info.UUID, "websocket.SocketServer.ServeHTTP", "WebSocket Upgrade : %+q : %+q", r.RemoteAddr, err.Error())
		return
	}

	s.cl.Lock()
	{
		s.clients = append(s.clients, &client)
	}
	s.cl.Unlock()

	client.Wait()

	s.log.Log(octo.LOGINFO, s.info.UUID, "websocket.SocketServer.ServeHTTP", "Completed")
}

// Close ends the websocket connection and ensures all requests are finished.
func (s *SocketServer) Close() error {
	s.log.Log(octo.LOGINFO, s.info.UUID, "websocket.SocketServer.Close", "Start")
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
		s.log.Log(octo.LOGERROR, s.info.UUID, "websocket.SocketServer.Close", "Completed : %+q", err.Error())
	}

	// Close all clients connections.
	s.cl.Lock()
	{
		for _, client := range s.clients {
			go client.Close()
		}
	}
	s.cl.Unlock()

	s.log.Log(octo.LOGINFO, s.info.UUID, "websocket.SocketServer.Close", "Completed")
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

//================================================================================

// Client defines a struct to defines a websocket client connection for managing
// each request client.
type Client struct {
	*websocket.Conn
	log     octo.Logs
	info    octo.Info
	system  octo.System
	primary *octo.BaseSystem
	wg      sync.WaitGroup
	sg      sync.WaitGroup
	server  *SocketServer
	cl      sync.Mutex
	running bool
	doClose bool
}

// Wait awaits the closure of the giving client.
func (c *Client) Wait() {
	c.wg.Wait()
}

// SendAll delivers a binary data to all websocket connections.
func (c *Client) SendAll(data []byte, flush bool) error {
	c.log.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.SendAll", "Started")

	if err := c.Send(data, flush); err != nil {
		c.log.Log(octo.LOGERROR, c.info.UUID, "websocket.Client.SendAll", "Started : Error : %s", err.Error())
		return err
	}

	clients := c.server.Clients()
	for _, client := range clients {
		if client == c {
			continue
		}

		if err := client.Send(data, flush); err != nil {
			c.log.Log(octo.LOGERROR, c.info.UUID, "websocket.Client.SendAll", "Started : %q : Error : %s", client.info.UUID, err.Error())
		}
	}

	c.log.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.SendAll", "Started")
	return nil
}

// Send delivers a binary data to the websocket connection.
func (c *Client) Send(data []byte, flush bool) error {
	c.log.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.Send", "Started")

	if c.shouldClose() {
		err := errors.New("Connection will be closed")
		c.log.Log(octo.LOGERROR, c.info.UUID, "websocket.Client.Send", "Completed : Error : %s", err.Error())
		return err
	}

	c.sg.Add(1)
	defer c.sg.Done()

	c.log.Log(octo.LOGTRANSMISSION, c.info.UUID, "websocket.Client.Send", "Started : %+q", data)
	err := c.Conn.WriteMessage(websocket.BinaryMessage, data)
	c.log.Log(octo.LOGTRANSMISSION, c.info.UUID, "websocket.Client.Send", "Completed")

	if err != nil {
		c.log.Log(octo.LOGERROR, c.info.UUID, "websocket.Client.Send", "Started : %q : Error : %s", c.info.UUID, err.Error())
		return err
	}

	c.log.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.Send", "Started")
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
		c.log.Log(octo.LOGERROR, c.info.UUID, "websocket.Client.Close", "Closing Websocket Connection : Error : %q", err.Error())
	}

	return nil
}

// Listen starts the giving client and begins reading the websocket connection.
func (c *Client) Listen() error {
	c.log.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.Listen", "Start : %#v", c.info)

	if c.system == nil || c.primary == nil {
		err := errors.New("Client system/Primary systems are not set")
		c.log.Log(octo.LOGERROR, c.info.UUID, "websocket.Client.Listen", "Websocket Unable to Listen : Error : %q", err.Error())
		return err
	}

	c.wg.Add(1)
	go c.acceptRequests()

	c.log.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.Listen", "Completed")
	return nil
}

// acceptRequests handles the processing of requests from the server.
func (c *Client) acceptRequests() {
	c.log.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.handleRequests", "Started")
	defer c.wg.Done()

	for c.isRunning() {

		messageType, message, err := c.Conn.ReadMessage()
		if err != nil {
			c.log.Log(octo.LOGERROR, c.info.UUID, "websocket.Client.handleRequests", "Error : %q", err.Error())
			if err == io.EOF || err == websocket.ErrBadHandshake || err == websocket.ErrCloseSent {
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
			c.log.Log(octo.LOGERROR, c.info.UUID, "websocket.Server.acceptRequests", "Websocket Base System : Fails Parsing : Error : %+s", err)

			if err := c.system.Serve(data, &tx); err != nil {
				c.log.Log(octo.LOGERROR, c.info.UUID, "websocket.Server.acceptRequests", "Websocket Base System : Fails Parsing : Error : %+s", err)
			}
		}

		// Handle remaining messages and pass it to user system.
		if rem != nil {
			if err := c.system.Serve(byteutils.JoinMessages(rem...), &tx); err != nil {
				c.log.Log(octo.LOGERROR, c.info.UUID, "websocket.Server.acceptRequests", "Websocket Base System : Fails Parsing : Error : %+s", err)
			}
		}

		if c.shouldClose() {
			break
		}
	}

	c.log.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.acceptRequests", "Completed")
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

// Transmission defines a struct for handling responses from a octo.System object.
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
