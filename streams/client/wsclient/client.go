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
	"github.com/influx6/octo/netutils"
	"github.com/influx6/octo/utils"
)

// Attr defines a struct which holds configuration options for the websocket
// client.
type Attr struct {
	MaxReconnets int
	MaxDrops     int
	Authenticate bool
	Addr         string
	Clusters     []string

	EnableCompression bool
	Headers           map[string]string
	Dialer            *websocket.Dialer
}

// WebSocketPod defines a websocket implementation which connects
// to a provided websocket endpoint for making requests.
type WebSocketPod struct {
	attr        Attr
	instruments octo.Instrumentation
	pub         *octo.Pub
	servers     []*srvAddr
	curAddr     *srvAddr
	bm          bytes.Buffer
	wg          sync.WaitGroup
	system      goclient.System
	encoding    goclient.MessageEncoding
	cnl         sync.Mutex
	doClose     bool
	started     bool
	conn        *WebsocketConn
}

// New returns a new instance of the websocket pod.
func New(insts octo.Instrumentation, attr Attr) (*WebSocketPod, error) {
	if attr.MaxDrops <= 0 {
		attr.MaxDrops = consts.MaxTotalConnectionFailure
	}

	if attr.MaxReconnets <= 0 {
		attr.MaxReconnets = consts.MaxTotalReconnection
	}

	var pod WebSocketPod
	pod.attr = attr
	pod.instruments = insts
	pod.pub = octo.NewPub()

	// Prepare all server registering and validate paths.
	if err := pod.prepareServers(); err != nil {
		return nil, err
	}

	return &pod, nil
}

// Listen calls the connection to be create and begins serving requests.
func (w *WebSocketPod) Listen(sm goclient.System, encoding goclient.MessageEncoding) error {
	w.cnl.Lock()
	if w.started {
		return nil
	}
	w.cnl.Unlock()

	w.system = sm
	w.encoding = encoding

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

	w.notify(octo.ClosedHandler, nil)

	w.cnl.Lock()
	w.doClose = true
	w.cnl.Unlock()

	if err := w.conn.Close(); err != nil {
		w.notify(octo.ErrorHandler, err)

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
func (w *WebSocketPod) Register(tm octo.StateHandlerType, hmi interface{}) {
	w.pub.Register(tm, hmi)
}

// notify calls the giving callbacks for each different type of state.
func (w *WebSocketPod) notify(n octo.StateHandlerType, err error) {
	var cm octo.Contact

	if w.curAddr != nil {
		cm = w.curAddr.contact
	}

	w.pub.Notify(n, cm, err)
}

// Send delivers the giving message to the underline websocket connection.
func (w *WebSocketPod) Send(data interface{}, flush bool) error {

	// Encode outside of lock to reduce contention.
	dataBytes, err := w.encoding.Encode(data)
	if err != nil {
		return err
	}

	w.cnl.Lock()
	if w.curAddr != nil && !w.curAddr.connected && !w.curAddr.reconnecting {
		w.cnl.Unlock()
		w.bm.Write(dataBytes)
		return nil
	}
	w.cnl.Unlock()

	if w.bm.Len() > 0 {
		w.conn.Write(w.bm.Bytes(), false)
		w.bm.Reset()
	}

	return w.conn.Write(dataBytes, flush)
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
	contact      octo.Contact
}

// prepareServers registered all provided address from the attribute as cycling
// items in a server lists for reducing load on a new server.
func (w *WebSocketPod) prepareServers() error {

	// Add the main addr if provided.
	if w.attr.Addr != "" {
		w.attr.Addr = netutils.GetAddr(w.attr.Addr)

		ep, err := url.Parse(w.attr.Addr)
		// fmt.Printf("Preping: %q -> %+q <- %q", w.attr.Addr, ep, err)
		if err != nil {
			return err
		}

		w.servers = append(w.servers, &srvAddr{
			index:   0,
			ep:      ep,
			addr:    w.attr.Addr,
			contact: utils.NewContact(w.attr.Addr),
		})
	}

	total := len(w.servers)

	for _, addr := range w.attr.Clusters {
		addr = netutils.GetAddr(addr)

		ep, err := url.Parse(addr)
		if err != nil {
			return err
		}

		w.servers = append(w.servers, &srvAddr{
			index:   total,
			ep:      ep,
			addr:    addr,
			contact: utils.NewContact(addr),
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
		if w.attr.MaxDrops > 0 && srv.drops >= w.attr.MaxDrops {
			w.servers[index] = nil
			continue
		}

		// If the Maximum allow reconnection reached, nil and continue.
		if w.attr.MaxReconnets > 0 && srv.recons >= w.attr.MaxReconnets {
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
	if w.attr.Authenticate && w.attr.Headers != nil {
		if _, ok := w.attr.Headers["Authorization"]; !ok {
			return consts.ErrNoAuthorizationHeader
		}
	}

	w.cnl.Lock()
	if w.started {
		w.notify(octo.DisconnectHandler, nil)
	}
	w.cnl.Unlock()

	if err := w.getNextServer(); err != nil {
		return err
	}

	var addr string

	w.cnl.Lock()
	{
		w.curAddr.reconnecting = true
		w.curAddr.connected = false
		w.curAddr.recons++

		if w.curAddr.ep != nil {
			addr = w.curAddr.ep.String()
		} else {
			addr = w.curAddr.addr
		}
	}
	w.cnl.Unlock()

	// fmt.Printf("NewConn: %q\n", w.attr.Addr)

	conn, err := NewWebsocketConn(addr, WebsocketAttr{
		Headers:           w.attr.Headers,
		Dialer:            w.attr.Dialer,
		EnableCompression: w.attr.EnableCompression,
	})

	// fmt.Printf("NewConn: %q -> %+q\n", w.attr.Addr, err)

	if err != nil {
		w.notify(octo.DisconnectHandler, err)
		w.curAddr.drops++
		return w.reconnect()
	}

	w.cnl.Lock()
	w.started = true
	w.conn = conn
	w.curAddr.connected = true
	w.curAddr.reconnecting = false
	w.cnl.Unlock()

	w.notify(octo.ConnectHandler, nil)

	go w.acceptRequests()

	return nil
}

// acceptRequests processing the connections for incoming messages.
func (w *WebSocketPod) acceptRequests() {
	w.wg.Add(1)
	defer w.wg.Done()

	for !w.shouldClose() {
		w.conn.SetReadDeadline(time.Now().Add(consts.WSReadTimeout))

		// fmt.Printf("StartWSSocket:Reading Accept: \n")
		data, err := w.conn.Read()
		// fmt.Printf("Accept::Err: %+q\n", err)
		if err != nil {
			w.notify(octo.ErrorHandler, err)

			if err == consts.ErrAbitraryCloseConnection || err == consts.ErrClosedConnection || err == consts.ErrUnstableRead {
				go w.reconnect()
				return
			}

			continue
		}

		val, err := w.encoding.Decode(data)
		if err != nil {
			w.notify(octo.ErrorHandler, err)
			continue
		}

		if err := w.system.Serve(val, w); err != nil {
			w.notify(octo.ErrorHandler, err)
			continue
		}
	}
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

//================================================================================

// WebsocketAttr defines a struct which holds configuration options for the websocket
// client.
type WebsocketAttr struct {
	EnableCompression bool
	Headers           map[string]string
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
	for key, val := range attr.Headers {
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

		// Send Close message to websocket.
		t.conn.WriteControl(websocket.CloseMessage, nil, time.Now().Add(consts.WSReadTimeout))

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
