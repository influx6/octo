package tcpclient

import (
	"bufio"
	"bytes"
	"errors"
	"net"
	"net/url"
	"sync"
	"time"

	"github.com/influx6/octo"
	"github.com/influx6/octo/clients/goclient"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/netutils"
	uuid "github.com/satori/go.uuid"
)

// Attr defines a struct which holds configuration options for the TCP
// client.
type Attr struct {
	MaxReconnets int
	MaxDrops     int
	Addr         string
	Clusters     []string
	SocketAttr   TCPAttr
}

// TCPPod defines a TCP implementation which connects
// to a provided TCP endpoint for making requests.
type TCPPod struct {
	attr        Attr
	servers     []*srvAddr
	curAddr     *srvAddr
	bm          bytes.Buffer
	wg          sync.WaitGroup
	system      goclient.System
	cnl         sync.Mutex
	doClose     bool
	conn        *TCPConn
	cml         sync.Mutex
	connects    []goclient.StateHandler
	disconnects []goclient.StateHandler
	closes      []goclient.StateHandler
	errors      []goclient.ErrorStateHandler
}

// New returns a new instance of the TCP pod.
func New(logs octo.Logs, attr Attr) (*TCPPod, error) {
	var pod TCPPod
	pod.attr = attr

	// Prepare all server registering and validate paths.
	if err := pod.prepareServers(); err != nil {
		return nil, err
	}

	return &pod, nil
}

// Listen calls the connection to be create and begins serving requests.
func (w *TCPPod) Listen(sm goclient.System) error {
	w.system = sm

	if err := w.reconnect(); err != nil {
		return err
	}

	return nil
}

// Close closes the TCP connection.
func (w *TCPPod) Close() error {
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
func (w *TCPPod) Register(tm goclient.StateHandlerType, hmi interface{}) {
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

// Send delivers the giving message to the underline TCP connection.
func (w *TCPPod) Send(data []byte, flush bool) error {
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
func (w *TCPPod) acceptRequests() {
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
func (w *TCPPod) prepareServers() error {

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
func (w *TCPPod) getNextServer() error {
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
func (w *TCPPod) reconnect() error {
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

	conn, err := NewTCPConn(addr, w.attr.SocketAttr)
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
func (w *TCPPod) shouldClose() bool {
	w.cnl.Lock()
	if w.doClose {
		w.cnl.Unlock()
		return true
	}
	w.cnl.Unlock()

	return false
}

// isClose returns true/false if the connection is already closed.
func (w *TCPPod) isClose() bool {
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
func (w *TCPPod) notify(n goclient.StateHandlerType, err error) {
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

// TCPConn defines a interface for a type which connects to
// a tcp endpoint and provides read and write capabilities.
type TCPConn struct {
	conn          net.Conn
	writer        *bufio.Writer
	reader        *bufio.Reader
	cl            sync.Mutex
	lastBlockSize int
}

// NewTCPConn returns a new instance of a TCPConn.
func NewTCPConn(addr string) (*TCPConn, error) {
	ip, port, _ := net.SplitHostPort(addr)
	if ip == "" || ip == consts.AnyIP {
		if realIP, err := netutils.GetMainIP(); err == nil {
			addr = net.JoinHostPort(realIP, port)
		}
	}

	conn, err := net.DialTimeout("tcp", addr, consts.MaxWaitTime)
	if err != nil {
		return nil, err
	}

	return &TCPConn{
		conn:          conn,
		writer:        bufio.NewWriter(conn),
		reader:        bufio.NewReader(conn),
		lastBlockSize: 512,
	}, nil
}

// SetDeadline sets the base deadline of the connection.
func (t *TCPConn) SetDeadline(tl time.Time) {
	t.cl.Lock()
	t.conn.SetDeadline(tl)
	t.cl.Unlock()
}

// SetWriteDeadline sets the read deadline of the connection.
func (t *TCPConn) SetWriteDeadline(tl time.Time) {
	t.cl.Lock()
	t.conn.SetWriteDeadline(tl)
	t.cl.Unlock()
}

// SetReadDeadline sets the read deadline of the connection.
func (t *TCPConn) SetReadDeadline(tl time.Time) {
	t.cl.Lock()
	t.conn.SetReadDeadline(tl)
	t.cl.Unlock()
}

// Write writes the current available data from the pipeline.
func (t *TCPConn) Write(data []byte, flush bool) error {
	if t.isClosed() {
		return ErrClosedConnection
	}

	var err error

	t.cl.Lock()
	{
		_, err = t.writer.Write(data)
		if err == nil && flush {
			err = t.writer.Flush()
		}
	}
	t.cl.Unlock()

	return err
}

// Close ends and disposes of the internal connection, closing it and
// all reads and writers.
func (t *TCPConn) Close() error {
	var err error

	t.cl.Lock()
	{
		if t.conn == nil {
			t.cl.Unlock()
			return nil
		}

		err = t.conn.Close()

		t.conn = nil
		t.writer = nil
		t.reader = nil
	}
	t.cl.Unlock()

	return err
}

// ErrClosedConnection is returned when the giving client connection
// has being closed.
var ErrClosedConnection = errors.New("Connection Closed")

// Read reads the current available data from the pipeline.
func (t *TCPConn) Read() ([]byte, error) {
	if t.isClosed() {
		return nil, ErrClosedConnection
	}

	block := make([]byte, t.lastBlockSize)

	var n int
	var err error

	// t.cl.Lock()
	{
		n, err = t.reader.Read(block)
		if err != nil {
			// t.cl.Unlock()
			return nil, err
		}

		if n == len(block) && len(block) < consts.MaxDataWrite {
			t.lastBlockSize = len(block) * 2
		}

		if n < len(block)/2 && len(block) > consts.MaxDataWrite {
			t.lastBlockSize = len(block) / 2
		}
	}
	// t.cl.Unlock()

	return block[:n], nil
}

// isClosed returns true/false if the connection has been closed.
func (t *TCPConn) isClosed() bool {
	var closed bool

	t.cl.Lock()
	{
		closed = (t.conn == nil)
	}
	t.cl.Unlock()

	return closed
}
