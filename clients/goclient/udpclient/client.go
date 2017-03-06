package UDPConn

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

// Attr defines a struct which holds configuration options for the UDP
// client.
type Attr struct {
	MaxReconnets int
	MaxDrops     int
	Version      string
	ClientAddr   string
	Addr         string
	Clusters     []string
}

// UDPPod defines a UDP implementation which connects
// to a provided UDP endpoint for making requests.
type UDPPod struct {
	attr        Attr
	servers     []*srvAddr
	curAddr     *srvAddr
	bm          bytes.Buffer
	wg          sync.WaitGroup
	system      goclient.System
	cnl         sync.Mutex
	doClose     bool
	conn        *UDPConn
	cml         sync.Mutex
	connects    []goclient.StateHandler
	disconnects []goclient.StateHandler
	closes      []goclient.StateHandler
	errors      []goclient.ErrorStateHandler
}

// New returns a new instance of the UDP pod.
func New(logs octo.Logs, attr Attr) (*UDPPod, error) {
	var pod UDPPod
	pod.attr = attr

	// Prepare all server registering and validate paths.
	if err := pod.prepareServers(); err != nil {
		return nil, err
	}

	return &pod, nil
}

// Listen calls the connection to be create and begins serving requests.
func (w *UDPPod) Listen(sm goclient.System) error {
	w.system = sm

	if err := w.reconnect(); err != nil {
		return err
	}

	return nil
}

// Close closes the UDP connection.
func (w *UDPPod) Close() error {
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
func (w *UDPPod) Register(tm goclient.StateHandlerType, hmi interface{}) {
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

// Send delivers the giving message to the underline UDP connection.
func (w *UDPPod) Send(data []byte, flush bool) error {
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
func (w *UDPPod) acceptRequests() {
	w.wg.Add(1)
	defer w.wg.Done()

	for !w.shouldClose() {
		w.conn.SetReadDeadline(time.Now().Add(consts.WSReadTimeout))

		data, addr, err := w.conn.Read()
		if err != nil {
			w.notify(goclient.ErrorHandler, err)

			if err == consts.ErrAbitraryCloseConnection || err == consts.ErrClosedConnection || err == consts.ErrUnstableRead {
				go w.reconnect()
				return
			}

			continue
		}

		w.system.Serve(data, &ulink{UDPPod: w, targetAddr: addr})
	}
}

type ulink struct {
	*UDPPod
	targetAddr *net.UDPAddr
}

// Send delivers the giving message to the underline addr.
func (l *ulink) Send(data []byte, flush bool) error {
	l.UDPPod.cnl.Lock()
	if l.UDPPod.curAddr != nil && !l.UDPPod.curAddr.connected && !l.UDPPod.curAddr.reconnecting {
		l.UDPPod.cnl.Unlock()
		l.UDPPod.bm.Write(data)
		return nil
	}
	l.UDPPod.cnl.Unlock()

	return l.UDPPod.conn.WriteUDP(data, l.targetAddr)
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
func (w *UDPPod) prepareServers() error {

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
func (w *UDPPod) getNextServer() error {
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
func (w *UDPPod) reconnect() error {
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

	conn, err := NewUDPConn(w.attr.Version, w.attr.ClientAddr, addr)
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
func (w *UDPPod) shouldClose() bool {
	w.cnl.Lock()
	if w.doClose {
		w.cnl.Unlock()
		return true
	}
	w.cnl.Unlock()

	return false
}

// isClose returns true/false if the connection is already closed.
func (w *UDPPod) isClose() bool {
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
func (w *UDPPod) notify(n goclient.StateHandlerType, err error) {
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

// ErrClosedConnection is returned when the giving client connection
// has being closed.
var ErrClosedConnection = errors.New("Connection Closed")

// UDPConn defines a interface for a type which connects to
// a UDP endpoint and provides read and write capabilities.
type UDPConn struct {
	cl   sync.Mutex
	conn *net.UDPConn
	bw   *bufio.Writer
}

// NewUDPConn returns a new instance of a UDPConn.
func NewUDPConn(version string, userAddr string, serverAddr string) (*UDPConn, error) {
	udpServerAddr, err := net.ResolveUDPAddr(version, netutils.GetAddr(serverAddr))
	if err != nil {
		return nil, err
	}

	udpLocalAddr, err := net.ResolveUDPAddr(version, netutils.GetAddr(userAddr))
	if err != nil {
		return nil, err
	}

	var conn *net.UDPConn

	conn, err = net.DialUDP(version, udpLocalAddr, udpServerAddr)
	if err != nil {
		return nil, err
	}

	var udp UDPConn
	udp.conn = conn
	udp.bw = bufio.NewWriter(conn)

	return &udp, nil
}

// SetDeadline sets the base deadline of the connection.
func (t *UDPConn) SetDeadline(tl time.Time) {
	t.cl.Lock()
	t.conn.SetDeadline(tl)
	t.cl.Unlock()
}

// SetWriteDeadline sets the read deadline of the connection.
func (t *UDPConn) SetWriteDeadline(tl time.Time) {
	t.cl.Lock()
	t.conn.SetWriteDeadline(tl)
	t.cl.Unlock()
}

// SetReadDeadline sets the read deadline of the connection.
func (t *UDPConn) SetReadDeadline(tl time.Time) {
	t.cl.Lock()
	t.conn.SetReadDeadline(tl)
	t.cl.Unlock()
}

// Read reads the current available data from the pipeline.
func (t *UDPConn) Read() ([]byte, *net.UDPAddr, error) {
	if t.isClosed() {
		return nil, nil, ErrClosedConnection
	}

	block := make([]byte, 6085)

	t.cl.Lock()
	n, addr, err := t.conn.ReadFromUDP(block)
	if err != nil {
		t.cl.Unlock()
		return nil, nil, err
	}
	t.cl.Unlock()

	return block[:n], addr, nil
}

// Write writes the current available data from the pipeline.
func (t *UDPConn) Write(data []byte, flush bool) error {
	if t.isClosed() {
		return ErrClosedConnection
	}

	t.cl.Lock()
	t.bw.Write(data)
	t.cl.Unlock()

	if flush {
		t.bw.Flush()
	}

	return nil
}

// WriteUDP writes the current available data from the pipeline.
func (t *UDPConn) WriteUDP(data []byte, addr *net.UDPAddr) error {
	if t.isClosed() {
		return ErrClosedConnection
	}

	// Do we still have data pending for write, flush writer.
	t.cl.Lock()
	if t.bw.Available() > 0 {
		t.bw.Flush()
	}

	// Allow user to set details as desired.

	if addr == nil {
		_, err := t.conn.Write(data)
		t.cl.Unlock()

		return err
	}

	_, err := t.conn.WriteToUDP(data, addr)
	t.cl.Unlock()

	return err
}

// Close ends and disposes of the internal connection, closing it and
// all reads and writers.
func (t *UDPConn) Close() error {
	t.cl.Lock()
	if t.conn == nil {
		t.cl.Unlock()
		return ErrClosedConnection
	}

	err := t.conn.Close()
	t.conn = nil

	t.cl.Unlock()

	return err
}

// isClosed returns true/false if the connection has been closed.
func (t *UDPConn) isClosed() bool {
	var closed bool
	t.cl.Lock()
	{
		closed = (t.conn == nil)
	}
	t.cl.Unlock()
	return closed
}
