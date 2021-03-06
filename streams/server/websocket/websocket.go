// Package websocket provides a simple package that implements a websocket protocol
// transmission for the octo.TranmissionProtocol interface. Which allows a uniform
// response cycle with a websocket based connection.
package websocket

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/influx6/faux/context"
	"github.com/influx6/octo"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/netutils"

	"github.com/influx6/octo/messages/jsoni"
	jsoniserver "github.com/influx6/octo/messages/jsoni/server"
	"github.com/influx6/octo/streams/server"

	"github.com/influx6/octo/utils"
	uuid "github.com/satori/go.uuid"
)

// RequestOriginValidator defines a function which validates a request origin for
// a websocket request.
type RequestOriginValidator func(*http.Request) bool

// SocketAttr defines a attribute struct for defining options for the WebsocketServer
// struct.
type SocketAttr struct {
	Authenticate       bool
	MessageCompression bool
	Addr               string
	SkipCORS           bool
	TLSConfig          *tls.Config
	Headers            map[string]string
	Credential         octo.AuthCredential
	OriginValidator    RequestOriginValidator
}

// SocketServer defines a struct implements the http.ServeHTTP interface which
// handles servicing http requests for websockets.
type SocketServer struct {
	pub         *server.Pub
	Attr        SocketAttr
	headers     http.Header
	instruments octo.Instrumentation
	info        octo.Contact
	base        *BaseSocketServer
	server      *http.Server
	listener    net.Listener
	wg          sync.WaitGroup
	rl          sync.Mutex
	running     bool
	doClose     bool
}

// New returns a new instance of a SocketServer.
func New(instruments octo.Instrumentation, attr SocketAttr) *SocketServer {
	ip, port, _ := net.SplitHostPort(attr.Addr)
	if ip == "" || ip == consts.AnyIP {
		if realIP, err := netutils.GetMainIP(); err == nil {
			attr.Addr = net.JoinHostPort(realIP, port)
		}
	}

	var suuid = uuid.NewV4().String()

	headers := make(http.Header)

	for key, val := range attr.Headers {
		headers.Add(key, val)
	}

	var ws SocketServer
	ws.Attr = attr
	ws.headers = headers
	ws.pub = server.NewPub()
	ws.instruments = instruments
	ws.info = octo.Contact{
		SUUID:  suuid,
		UUID:   suuid,
		Addr:   attr.Addr,
		Remote: attr.Addr,
	}

	return &ws
}

// Listen begins the initialization of the websocket server.
func (s *SocketServer) Listen(system server.System) error {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "websocket.SocketServer.Listen", "Started : Addr[%q]", s.Attr.Addr)

	if s.isRunning() {
		s.instruments.Log(octo.LOGINFO, s.info.UUID, "websocket.SocketServer.Listen", "Completed")
		return nil
	}

	listener, err := netutils.MakeListener("tcp", s.Attr.Addr, s.Attr.TLSConfig)
	if err != nil {
		s.instruments.Log(octo.LOGERROR, s.info.UUID, "websocket.SocketServer.Listen", "Initialize net.Listener failed : Error : %s", err.Error())
		return err
	}

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "websocket.SocketServer.Listen", "Listener : %+q", listener.Addr().String())

	server, tlListener, err := netutils.NewHTTPServer(listener, s, s.Attr.TLSConfig)
	if err != nil {
		s.instruments.Log(octo.LOGERROR, s.info.UUID, "websocket.SocketServer.Listen", "New HTTPServer failed : Error : %s", err.Error())
		return err
	}

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "websocket.SocketServer.Listen", "Server : %+q", server.Addr)

	s.base = NewBaseSocketServer(s.instruments, BaseSocketAttr{
		Headers:            s.headers,
		Authenticate:       s.Attr.Authenticate,
		OriginValidator:    s.Attr.OriginValidator,
		MessageCompression: s.Attr.MessageCompression,
		SkipCORS:           s.Attr.SkipCORS,
		Pub:                s.pub,
	}, s.info, s, system)

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
	SkipCORS           bool
	Headers            http.Header
	OriginValidator    RequestOriginValidator
	Pub                *server.Pub
}

// BaseSocketServer defines the struct which implements the core functionality of
// the websocket request handler and implements the http.Handler interface.
type BaseSocketServer struct {
	MessageCompression bool
	Attr               BaseSocketAttr
	instruments        octo.Instrumentation
	info               octo.Contact
	base               *jsoni.SxConversations
	auth               octo.Authenticator
	upgrader           websocket.Upgrader
	cl                 sync.Mutex
	clients            map[string]*Client
}

// NewBaseSocketServer returns a new instance of a BaseSocketServer.
func NewBaseSocketServer(instruments octo.Instrumentation, attr BaseSocketAttr, info octo.Contact, credentials octo.Credentials, system server.System) *BaseSocketServer {
	var base BaseSocketServer
	base.Attr = attr
	base.info = info
	base.instruments = instruments
	base.clients = make(map[string]*Client)

	base.auth = system
	base.base = jsoni.NewSxConversations(system, jsoniserver.CloseServer{}, jsoniserver.ContactServer{}, jsoniserver.ConversationServer{}, &jsoniserver.AuthServer{
		Credentials: credentials,
	})

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

	var clients []*Client

	s.cl.Lock()
	for _, item := range s.clients {
		clients = append(clients, item)
	}
	s.cl.Unlock()

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "websocket.BasicSocketServer.Clients", "Completed")
	return clients
}

// Contact returns the giving info struct for the giving socket server.
func (s *BaseSocketServer) Contact() octo.Contact {
	return s.info
}

// Close ends the websocket connection and ensures all requests are finished.
func (s *BaseSocketServer) Close() error {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "websocket.BaseSocketServer.Close", "Started")

	s.cl.Lock()
	defer s.cl.Unlock()

	for _, client := range s.clients {
		go client.Close()
	}

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "websocket.BaseSocketServer.Close", "Completed")
	return nil
}

// ServeHTTP defines a method to serve and handle websocket requests.
func (s *BaseSocketServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "websocket.BaseSocketServer.ServeHTTP", "Started")

	if !s.Attr.SkipCORS {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Max-Age", "86400")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
	}

	var cuuid = uuid.NewV4().String()

	var client Client
	client.info = octo.Contact{
		UUID:   cuuid,
		SUUID:  s.info.SUUID,
		Addr:   r.RemoteAddr,
		Remote: r.RemoteAddr,
	}

	client.pub = s.Attr.Pub
	client.server = s
	client.Request = r
	client.auth = s.auth
	client.base = s.base
	client.instruments = s.instruments
	client.closer = make(chan struct{}, 0)

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
		s.clients[client.info.UUID] = &client
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
	pub           *server.Pub
	Request       *http.Request
	instruments   octo.Instrumentation
	info          octo.Contact
	base          *jsoni.SxConversations
	auth          octo.Authenticator
	wg            sync.WaitGroup
	sg            sync.WaitGroup
	server        *BaseSocketServer
	cl            sync.Mutex
	closer        chan struct{}
	running       bool
	doClose       bool
	authenticated bool
}

// authenticate runs the authentication procedure to authenticate that the connection
// was valid.
func (c *Client) authenticate() error {
	c.instruments.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.authenticate", "Started")

	if !c.server.Attr.Authenticate {
		c.instruments.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.authenticate", "Completed")
		return nil
	}

	if len(c.Request.Header.Get("Authorization")) == 0 {
		return c.authorizationByRequest()
	}

	c.instruments.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.authenticate", "Completed")
	return c.authorizationByHeader()
}

// authenticateByRequest attempts to authenticate through the first message sent on
// the websocket connection.
func (c *Client) authorizationByRequest() error {
	c.instruments.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.authenticateByRequest", "Started")
	if !c.server.Attr.Authenticate {
		c.instruments.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.authenticateByRequest", "Completed")
		return nil
	}

	var buf []byte
	var err error

	var cmd jsoni.CommandMessage
	cmd.Name = string(consts.AuthRequest)

	if buf, err = jsoni.Parser.Encode(cmd); err != nil {
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "websocket.Client.authenticateByRequest", "Completed : Error : %q", err.Error())
		c.pub.Notify(server.ErrorHandler, c.info, c, err)
		return nil
	}

	if err := c.Send(buf, true); err != nil {
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "websocket.Client.authenticateByRequest", "Completed : Failed to Deliver : Error : %q", err.Error())
		c.pub.Notify(server.ErrorHandler, c.info, c, err)
		return nil
	}

	{

		var authResponse jsoni.AuthMessage
		var messageType int
		var message []byte

		{
			var failedReads int
			var err error

		authloop:
			for {
				c.Conn.SetReadDeadline(time.Now().Add(consts.ReadTimeout))

				messageType, message, err = c.Conn.ReadMessage()
				if err != nil {
					c.instruments.Log(octo.LOGERROR, c.info.UUID, "websocket.Client.authenticateByRequest", "Completed : Read Error : %+q", err.Error())
					c.Conn.SetReadDeadline(time.Time{})

					// If we have passed acceptable reads thresholds then fail.
					if failedReads >= consts.MaxAcceptableReadFails {
						c.instruments.Log(octo.LOGERROR, c.info.UUID, "websocket.Client.authenticateByRequest", "Completed : Max Read Failure : %+q", err.Error())
						return consts.ErrReadError
					}

					c.pub.Notify(server.ErrorHandler, c.info, c, err)
					failedReads++
					continue authloop
				}

				break authloop
			}

		}

		c.instruments.Log(octo.LOGTRANSMITTED, c.info.UUID, "websocket.Client.authenticateByRequest", "Type: %d, Message: %+q", messageType, message)
		c.instruments.Log(octo.LOGTRANSMITTED, c.info.UUID, "websocket.Client.authenticateByRequest", "Completed")

		if err := json.NewDecoder(bytes.NewBuffer(message)).Decode(&authResponse); err != nil {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "websocket.Client.authenticateByRequest", "Completed : %+q", err.Error())
			c.pub.Notify(server.ErrorHandler, c.info, c, err)
			return err
		}

		if authResponse.Name != string(consts.AuthResponse) {
			err := errors.New("Invalid Request received")
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "websocket.Client.authenticateByRequest", "Completed : %+q", err.Error())
			c.pub.Notify(server.ErrorHandler, c.info, c, err)
			return err
		}

		if merr := c.auth.Authenticate(authResponse.Data); merr != nil {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "websocket.Client.authenticateByRequest", "Completed : %+q", merr.Error())

			if block, cerr := jsoni.Parser.Encode(jsoni.CommandMessage{
				Name: string(consts.AuthroizationDenied),
				Data: []byte(merr.Error()),
			}); cerr == nil {
				c.pub.Notify(server.ErrorHandler, c.info, c, cerr)

				if serr := c.Send(block, true); serr != nil {
					c.instruments.Log(octo.LOGERROR, c.info.UUID, "websocket.Client.authenticateByRequest", "Failed to deliver denied message :  %+q", serr.Error())
				}
			}

			return merr
		}

		if block, cerr := jsoni.Parser.Encode(jsoni.CommandMessage{
			Name: string(consts.AuthroizationGranted),
		}); cerr == nil {
			c.pub.Notify(server.ErrorHandler, c.info, c, cerr)
			if serr := c.Send(block, true); serr != nil {
				c.instruments.Log(octo.LOGERROR, c.info.UUID, "websocket.Client.authenticateByRequest", "Completed : %+q", serr.Error())
			}
		}

		c.authenticated = true
	}

	c.instruments.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.authenticateByRequest", "Completed")
	return nil
}

// authorizationByHeader runs the authentication procedure to authenticate the connection
// by using the Authorization header present in the request object was valid.
func (c *Client) authorizationByHeader() error {
	c.instruments.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.authenticateByHeader", "Started")
	if !c.server.Attr.Authenticate {
		c.instruments.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.authenticateByHeader", "Completed")
		return nil
	}

	authorizationHeader := c.Request.Header.Get("Authorization")
	if len(authorizationHeader) == 0 {
		err := errors.New("'Authorization' header needed for authentication")
		c.pub.Notify(server.ErrorHandler, c.info, c, err)
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "websocket.Client.authenticateByHeader", "Completed : Error : %+q", err)
		return err
	}

	credentials, err := utils.ParseAuthorization(authorizationHeader)
	if err != nil {
		c.pub.Notify(server.ErrorHandler, c.info, c, err)
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "websocket.Client.authenticateByHeader", "Completed : Error : %+q", err)
		return err
	}

	if err := c.auth.Authenticate(credentials); err != nil {
		c.pub.Notify(server.ErrorHandler, c.info, c, err)
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "websocket.Client.authenticateByHeader", "Completed : Error : %+q", err)
		return err
	}

	c.authenticated = true
	c.instruments.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.authenticateByHeader", "Completed")
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

	c.instruments.NotifyEvent(octo.Event{
		Type:       octo.DataWrite,
		Client:     c.info.UUID,
		Server:     c.info.SUUID,
		LocalAddr:  c.info.Local,
		RemoteAddr: c.info.Remote,
		Data:       octo.NewDataInstrument(data, err),
		Details:    map[string]interface{}{},
	})

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

	// Close ping cycle
	c.closer <- struct{}{}

	c.pub.Notify(server.DisconnectHandler, c.info, c, nil)

	c.sg.Wait()

	c.pub.Notify(server.ClosedHandler, c.info, c, nil)

	if err := c.Conn.Close(); err != nil {
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "websocket.Client.Close", "Closing Websocket Connection : Error : %q", err.Error())
	}

	c.server.cl.Lock()
	{
		delete(c.server.clients, c.info.UUID)
	}
	c.server.cl.Unlock()

	return nil
}

// Listen starts the giving client and begins reading the websocket connection.
func (c *Client) Listen() error {
	c.instruments.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.Listen", "Start : %#v", c.info)

	if c.base == nil {
		err := errors.New("Client system is not set")
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "websocket.Client.Listen", "Websocket Unable to Listen : Error : %q", err.Error())
		return err
	}

	c.cl.Lock()
	{
		c.running = true
	}
	c.cl.Unlock()

	if err := c.authenticate(); err != nil {
		c.instruments.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.Listen", "WebSocket Authentication : %+q : %+q", c.Request.RemoteAddr, err.Error())
		return c.Close()
	}

	c.pub.Notify(server.ConnectHandler, c.info, c, nil)

	c.wg.Add(1)
	go c.acceptRequests()

	c.instruments.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.Listen", "Completed")
	return nil
}

// cyclePings runs the continous ping and pong response received from the client.
func (c *Client) cyclePings() {
	c.instruments.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.cyclePings", "Started")
	defer c.wg.Done()

	ticker := time.NewTicker(consts.MaxPingInterval)

	{
	cycleloop:
		for {
			select {
			case <-c.closer:
				ticker.Stop()
				break cycleloop
			case _, ok := <-ticker.C:
				if !ok {
					break cycleloop
				}

				c.Conn.WriteMessage(websocket.PingMessage, nil)
			}
		}

	}

	c.instruments.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.cyclePings", "Completed")
}

// acceptRequests handles the processing of requests from the server.
func (c *Client) acceptRequests() {
	c.instruments.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.acceptRequests", "Started")
	defer c.wg.Done()

	c.Conn.SetPongHandler(func(appData string) error {
		c.Conn.SetReadDeadline(time.Now().Add(consts.MaxPingPongWait))
		return nil
	})

	c.wg.Add(1)

	// Initialize ping handlers to maintain communication.
	go c.cyclePings()

	for c.isRunning() {
		if c.shouldClose() {
			break
		}

		c.Conn.SetReadDeadline(time.Now().Add(consts.MaxPingPongWait))

		messageType, message, err := c.Conn.ReadMessage()
		c.instruments.Log(octo.LOGTRANSMITTED, c.info.UUID, "websocket.Client.acceptRequests", "Type: %d, Message: %+q", messageType, message)
		c.instruments.Log(octo.LOGTRANSMITTED, c.info.UUID, "websocket.Client.acceptRequests", "Completed")

		if err != nil {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "websocket.Client.acceptRequests", "Error : %q", err.Error())

			c.Conn.WriteMessage(websocket.CloseMessage, nil)
			// if err == io.EOF || err == websocket.ErrBadHandshake || err == websocket.ErrCloseSent {
			// 	go c.Close()
			// 	break
			// }

			// if messageType == -1 && strings.Contains(err.Error(), "websocket: close") {
			go c.Close()
			break
			// }
		}

		c.Conn.SetReadDeadline(time.Time{})

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

		if err := c.base.Serve(data, &tx); err != nil {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "websocket.Server.acceptRequests", "Websocket System : Fails Serving : Error : %+s", err)

			// Send Close message.
			c.Conn.WriteMessage(websocket.CloseMessage, nil)

			// Close connection
			go c.Close()
			return
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

// Contact returns the giving information for the internal client and server.
func (t *Transmission) Contact() (octo.Contact, octo.Contact) {
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
