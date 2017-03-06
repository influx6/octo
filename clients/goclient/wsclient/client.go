package wsclient

import (
	"bytes"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/influx6/octo"
	"github.com/influx6/octo/clients/goclient"
	"github.com/influx6/octo/consts"
	uuid "github.com/satori/go.uuid"
)

// Attr defines a struct which holds configuration options for the websocket
// client.
type Attr struct {
	MaxReconnets int
	MaxDrops     int
	Addr         string
	Clusters     []string
	SocketAttr   WebsocketAttr
}

// WebSocketPod defines a websocket implementation which connects
// to a provided websocket endpoint for making requests.
type WebSocketPod struct {
	attr        Attr
	servers     []*srvAddr
	curAddr     *srvAddr
	bm          bytes.Buffer
	wg          sync.WaitGroup
	system      goclient.System
	cnl         sync.Mutex
	doClose     bool
	conn        *WebsocketConn
	cml         sync.Mutex
	connects    []goclient.StateHandler
	disconnects []goclient.StateHandler
	closes      []goclient.StateHandler
	errors      []goclient.ErrorStateHandler
}

// New returns a new instance of the websocket pod.
func New(logs octo.Logs, attr Attr) (*WebSocketPod, error) {
	var pod WebSocketPod
	pod.attr = attr

	// Prepare all server registering and validate paths.
	if err := pod.prepareServers(); err != nil {
		return nil, err
	}

	return &pod, nil
}

// Listen calls the connection to be create and begins serving requests.
func (w *WebSocketPod) Listen(sm goclient.System) error {
	w.system = sm

	if err := w.reconnect(); err != nil {
		return err
	}

	return nil
}

// Close closes the websocket connection.
func (w *WebSocketPod) Close() error {
	if w.isClose() {
		return consts.ErrClosedConnection
	}

	w.notify(goclient.ClosedHandler, nil)

	w.cnl.Lock()
	w.doClose = true
	w.cnl.Unlock()

	if err := w.conn.Close(); err != nil {
		w.notify(goclient.ErrorHandler, err)

		w.cnl.Lock()
		w.conn = nil
		w.cnl.Unlock()
		return err
	}

	w.wg.Wait()

	w.cnl.Lock()
	w.conn = nil
	w.cnl.Unlock()

	return nil
}

// Register registers the handler for a given handler.
func (w *WebSocketPod) Register(tm goclient.StateHandlerType, hmi interface{}) {
	var hms goclient.StateHandler
	var hme goclient.ErrorStateHandler

	switch ho := hmi.(type) {
	case goclient.StateHandler:
		hms = ho
	case goclient.ErrorStateHandler:
		hme = ho
	}

	// If the type does not match then return
	if hme == nil && tm == goclient.ErrorHandler {
		return
	}

	// If the type does not match then return
	if hms == nil && tm != goclient.ErrorHandler {
		return
	}

	switch tm {
	case goclient.ConnectHandler:
		w.cml.Lock()
		w.connects = append(w.connects, hms)
		w.cml.Unlock()
	case goclient.DisconnectHandler:
		w.cml.Lock()
		w.disconnects = append(w.disconnects, hms)
		w.cml.Unlock()
	case goclient.ErrorHandler:
		w.cml.Lock()
		w.errors = append(w.errors, hme)
		w.cml.Unlock()
	case goclient.ClosedHandler:
		w.cml.Lock()
		w.closes = append(w.closes, hms)
		w.cml.Unlock()
	}
}

// Send delivers the giving message to the underline websocket connection.
func (w *WebSocketPod) Send(data []byte, flush bool) error {
	w.cnl.Lock()
	if w.curAddr != nil && !w.curAddr.connected && !w.curAddr.reconnecting {
		w.cnl.Unlock()
		w.bm.Write(data)
		return nil
	}
	w.cnl.Unlock()

	return w.conn.Write(data, flush)
}

// acceptRequests processing the connections for incoming messages.
func (w *WebSocketPod) acceptRequests() {
	w.wg.Add(1)
	defer w.wg.Done()

	for !w.shouldClose() {
		w.conn.SetReadDeadline(time.Now().Add(consts.WSReadTimeout))

		data, err := w.conn.Read()
		if err != nil {
			w.notify(goclient.ErrorHandler, err)

			if err == consts.ErrAbitraryCloseConnection || err == consts.ErrClosedConnection || err == consts.ErrUnstableRead {
				go w.reconnect()
				return
			}

			continue
		}

		w.system.Serve(data, w)
	}
}

// srvAddr defines a struct for storing addr details.
type srvAddr struct {
	index        int
	drops        int
	recons       int
	ep           *url.URL
	addr         string
	connected    bool
	reconnecting bool
	contact      goclient.Contact
}

// prepareServers registered all provided address from the attribute as cycling
// items in a server lists for reducing load on a new server.
func (w *WebSocketPod) prepareServers() error {

	// Add the main addr if provided.
	if w.attr.Addr != "" {
		ep, err := url.Parse(w.attr.Addr)
		if err != nil {
			return err
		}

		w.servers = append(w.servers, &srvAddr{
			index:   0,
			ep:      ep,
			addr:    w.attr.Addr,
			contact: goclient.Contact{Addr: ep.String(), UUID: uuid.NewV4().String()},
		})
	}

	total := len(w.servers)

	for _, addr := range w.attr.Clusters {
		ep, err := url.Parse(addr)
		if err != nil {
			return err
		}

		w.servers = append(w.servers, &srvAddr{
			index:   total,
			ep:      ep,
			addr:    addr,
			contact: goclient.Contact{Addr: ep.String(), UUID: uuid.NewV4().String()},
		})

		total++
	}

	return nil
}

// getNextServer gets the next server in the lists setting it as the main server
// if the giving server has not reach the maximumum reconnection limit.
func (w *WebSocketPod) getNextServer() error {
	var indx int
	var chosen *srvAddr

	// Run through the servers and attempt to find another.
	for index, srv := range w.servers {
		if srv == nil {
			continue
		}

		// If the MaxTotalConnectionFailure is reached, nil this server has bad.
		if w.attr.MaxDrops <= 0 && srv.drops >= w.attr.MaxDrops {
			w.servers[index] = nil
			continue
		}

		// If the Maximum allow reconnection reached, nil and continue.
		if w.attr.MaxReconnets <= 0 && srv.recons >= w.attr.MaxReconnets {
			w.servers[index] = nil
			continue
		}

		chosen = srv
		indx = index
		break
	}

	if chosen == nil {
		w.servers = nil
		return consts.ErrNoServerFound
	}

	// Set the new server for usage.
	w.cnl.Lock()
	w.curAddr = chosen
	w.cnl.Unlock()

	w.servers = append(w.servers, chosen)
	w.servers = append(w.servers[:indx], w.servers[indx+1:]...)
	return nil
}

// reconnect attempts to retrieve a new server after a failure to connect and then
// begins message passing.
func (w *WebSocketPod) reconnect() error {
	w.notify(goclient.DisconnectHandler, nil)

	if err := w.getNextServer(); err != nil {
		return err
	}

	var addr string

	w.cnl.Lock()
	w.curAddr.reconnecting = true
	w.curAddr.connected = false
	addr = w.curAddr.ep.String()
	w.cnl.Unlock()

	conn, err := NewWebsocketConn(addr, w.attr.SocketAttr)
	if err != nil {
		w.notify(goclient.DisconnectHandler, err)
		return w.reconnect()
	}

	w.cnl.Lock()
	w.conn = conn
	w.curAddr.connected = true
	w.curAddr.reconnecting = false
	w.cnl.Unlock()

	w.notify(goclient.ConnectHandler, nil)

	w.wg.Add(1)
	go w.acceptRequests()

	return nil
}

// shouldClose returns true/false if the giving connection should close.
func (w *WebSocketPod) shouldClose() bool {
	w.cnl.Lock()
	if w.doClose {
		w.cnl.Unlock()
		return true
	}
	w.cnl.Unlock()

	return false
}

// isClose returns true/false if the connection is already closed.
func (w *WebSocketPod) isClose() bool {
	w.cnl.Lock()
	{
		if w.conn == nil {
			w.cnl.Unlock()
			return true
		}
	}
	w.cnl.Unlock()

	return false
}

// notify calls the giving callbacks for each different type of state.
func (w *WebSocketPod) notify(n goclient.StateHandlerType, err error) {
	var cm goclient.Contact

	if w.curAddr != nil {
		cm = w.curAddr.contact
	}

	switch n {
	case goclient.ErrorHandler:
		w.cml.Lock()
		defer w.cml.Unlock()

		for _, handler := range w.errors {
			handler(cm, err)
		}
	case goclient.ConnectHandler:
		w.cml.Lock()
		defer w.cml.Unlock()

		for _, handler := range w.connects {
			handler(cm)
		}
	case goclient.DisconnectHandler:
		w.cml.Lock()
		defer w.cml.Unlock()

		for _, handler := range w.disconnects {
			handler(cm)
		}
	case goclient.ClosedHandler:
		w.cml.Lock()
		defer w.cml.Unlock()

		for _, handler := range w.closes {
			handler(cm)
		}
	}
}

//================================================================================

// WebsocketAttr defines a struct which holds configuration options for the websocket
// client.
type WebsocketAttr struct {
	EnableCompression bool
	Header            map[string]string
	Dialer            *websocket.Dialer
}

// WebsocketConn defines a struct which handles the underline connection procedure
// for communicating through websocket connections.
type WebsocketConn struct {
	addr   string
	attr   WebsocketAttr
	cl     sync.Mutex
	conn   *websocket.Conn
	res    *http.Response
	header http.Header
	bu     bytes.Buffer
}

// NewWebsocketConn returns a new WebsocketConn intance using the provided WebsocketAttr.
func NewWebsocketConn(addr string, attr WebsocketAttr) (*WebsocketConn, error) {
	if attr.Dialer == nil {
		attr.Dialer = &websocket.Dialer{
			Proxy: http.ProxyFromEnvironment,
		}
	}

	header := make(http.Header)
	for key, val := range attr.Header {
		header.Set(key, val)
	}

	var socket WebsocketConn
	socket.attr = attr
	socket.header = header
	socket.addr = addr

	conn, res, err := attr.Dialer.Dial(addr, socket.header)
	if err != nil {
		return nil, err
	}

	if attr.EnableCompression {
		conn.EnableWriteCompression(attr.EnableCompression)
	}

	socket.conn = conn
	socket.res = res

	return &socket, nil
}

// Write writes the byte slice read from the websocket connections.
func (t *WebsocketConn) Write(bm []byte, flush bool) error {
	if t.isClosed() {
		return consts.ErrClosedConnection
	}

	// t.cl.Lock()
	t.bu.Write(bm)

	if flush {
		t.conn.WriteMessage(websocket.BinaryMessage, t.bu.Bytes())
		t.bu.Reset()
	}

	// t.cl.RLock()
	return nil
}

// Read returns the byte slice read from the websocket connections.
func (t *WebsocketConn) Read() ([]byte, error) {
	if t.isClosed() {
		return nil, consts.ErrClosedConnection
	}

	// t.cl.Lock()
	mtype, msg, err := t.conn.ReadMessage()
	if err != nil {
		// t.cl.Unlock()
		return nil, err
	}
	// t.cl.Unlock()

	switch mtype {
	case websocket.BinaryMessage:
		return msg, nil

	case websocket.TextMessage:
		return msg, nil

	case websocket.CloseMessage:
		go t.Close()
		return nil, consts.ErrClosedConnection

	case -1:
		if strings.Contains(err.Error(), "websocket: close") {
			go t.Close()
			return nil, consts.ErrAbitraryCloseConnection
		}
	}

	return nil, consts.ErrUnstableRead
}

// SetWriteDeadline sets the read deadline of the connection.
func (t *WebsocketConn) SetWriteDeadline(tl time.Time) {
	t.cl.Lock()
	t.conn.SetWriteDeadline(tl)
	t.cl.Unlock()
}

// SetReadDeadline sets the read deadline of the connection.
func (t *WebsocketConn) SetReadDeadline(tl time.Time) {
	t.cl.Lock()
	t.conn.SetReadDeadline(tl)
	t.cl.Unlock()
}

// Close ends and disposes of the internal connection, closing it and
// all reads and writers.
func (t *WebsocketConn) Close() error {
	var err error

	t.cl.Lock()
	{
		if t.conn == nil {
			t.cl.Unlock()
			return nil
		}

		err = t.conn.Close()

		t.conn = nil
		t.res = nil
	}
	t.cl.Unlock()

	return err
}

// isClosed returns true/false if the connection has been closed.
func (t *WebsocketConn) isClosed() bool {
	var closed bool

	t.cl.Lock()
	{
		closed = (t.conn == nil)
	}
	t.cl.Unlock()

	return closed
}
