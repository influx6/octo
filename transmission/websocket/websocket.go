package websocket

import (
	"net"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/influx6/faux/context"
	"github.com/influx6/faux/utils"
	"github.com/influx6/octo"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/netutils"
	"github.com/pborman/uuid"
)

// SocketAttr defines a attribute struct for defining options for the WebsocketServer
// struct.
type SocketAttr struct {
	Addr       string
	Credential octo.AuthCredential
	Headers    http.Header
}

// SocketServer defines a struct implements the http.ServeHTTP interface which
// handles servicing http requests for websockets.
type SocketServer struct {
	Attr     SocketAttr
	log      octo.Logs
	info     octo.Info
	upgrader websocket.Upgrader
	system   octo.System
	base     *octo.BaseSystem
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

	var suuid = uuid.NewUUID().String()

	var ws SocketServer
	ws.info = octo.Info{
		SUUID:  suuid,
		UUID:   suuid,
		Addr:   attr.Addr,
		Remote: attr.Addr,
	}

	ws.upgrader = websocket.Upgrader{
		ReadBufferSize:  consts.MaxBufferSize,
		WriteBufferSize: consts.MaxBufferSize,
	}

	return &ws
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

	conn, err := s.upgrader.Upgrade(w, r, s.Attr.Headers)
	if err != nil {
		s.log.Log(octo.LOGINFO, s.info.UUID, "websocket.SocketServer.ServeHTTP", "WebSocket Upgrade : %+q : %+q", r.RemoteAddr, err.Error())
		return
	}

	var cuuid = uuid.NewUUID().String()

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

	c.log.Log(octo.LOGTRANSMISSION, c.info.UUID, "websocket.Client.Send", "Started : %+q", data)
	c.log.Log(octo.LOGTRANSMISSION, c.info.UUID, "websocket.Client.Send", "Completed")

	c.log.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.Send", "Started")
	return nil
}

// Close ends the websocket connection and ensures all requests are finished.
func (c *Client) Close() error {

	return nil
}

// Listen starts the giving client and begins reading the websocket connection.
func (c *Client) Listen() error {
	c.log.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.Listen", "Start : %#v", c.info)

	c.wg.Add(1)

	c.log.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.Listen", "Completed")
	return nil
}

// acceptRequests handles the processing of requests from the server.
func (c *Client) acceptRequests() {
	c.log.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.handleRequests", "Started")
	defer c.wg.Done()

	for {
		messageType, message, err := c.Conn.ReadMessage()
		if err != nil {
			c.log.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.handleRequests", "Error : %q", err.Error())
			if err == websocket.ErrBadHandshake || err == websocket.ErrCloseSent {
				break
			}
		}

		var data []byte
		switch messageType {
		case websocket.BinaryMessage:
			data = message
		case websocket.TextMessage:
			data = message
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
		if err := c.system.Serve(utils.JoinMessages(rem...), &tx); err != nil {
			c.log.Log(octo.LOGERROR, c.info.UUID, "websocket.Server.acceptRequests", "Websocket Base System : Fails Parsing : Error : %+s", err)
		}
	}

	c.log.Log(octo.LOGINFO, c.info.UUID, "websocket.Client.acceptRequests", "Completed")
}

//================================================================================

// Transmission defines a struct for handling responses from a octo.System object.
type Transmission struct {
	client *Client
	ctx    context.Context
}

// SendAll pipes the giving data down the provided pipeline.
func (t *Transmission) SendAll(data []byte, flush bool) error {
	return nil
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
