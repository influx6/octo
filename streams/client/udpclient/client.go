package udpclient

import (
	"bufio"
	"bytes"
	"errors"
	"net"
	"net/url"
	"sync"
	"time"

	"github.com/influx6/octo"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/messages/jsoni"
	"github.com/influx6/octo/netutils"
	"github.com/influx6/octo/streams/client"
	"github.com/influx6/octo/utils"
)

// Version determines the ip type used for generating the udp ip.
type Version int

// contains the set of ip versions for which is used to generate
// ip value.
const (
	Ver0 Version = iota
	Ver4
	Ver6
)

// Attr defines a struct which holds configuration options for the UDP
// client.
type Attr struct {
	MaxReconnets int
	MaxDrops     int
	Version      Version
	ClientAddr   string
	Addr         string
	Clusters     []string
	Authenticate bool
}

// UDPPod defines a UDP implementation which connects
// to a provided UDP endpoint for making requests.
type UDPPod struct {
	attr        Attr
	udpVersion  string
	localAddr   string
	instruments octo.Instrumentation
	pub         *client.Pub
	servers     []*srvAddr
	curAddr     *srvAddr
	bm          bytes.Buffer
	wg          sync.WaitGroup
	system      client.SystemServer
	encoding    octo.MessageEncoding
	cnl         sync.Mutex
	doClose     bool
	started     bool
	conn        *UDPConn
}

// New returns a new instance of the websocket pod.
func New(insts octo.Instrumentation, attr Attr) (*UDPPod, error) {
	if attr.MaxDrops <= 0 {
		attr.MaxDrops = consts.MaxTotalConnectionFailure
	}

	if attr.MaxReconnets <= 0 {
		attr.MaxReconnets = consts.MaxTotalReconnection
	}

	var pod UDPPod
	pod.attr = attr
	pod.instruments = insts
	pod.pub = client.NewPub()
	pod.localAddr = netutils.GetAddr(attr.ClientAddr)

	switch attr.Version {
	case Ver0:
		pod.udpVersion = "udp"
	case Ver4:
		pod.udpVersion = "udp4"
	case Ver6:
		pod.udpVersion = "udp6"
	default:
		return nil, errors.New("Version number not supported for udp. Only Ver0, Ver4, Ver6 allowed")
	}

	// Prepare all server registering and validate paths.
	if err := pod.prepareServers(); err != nil {
		return nil, err
	}

	return &pod, nil
}

// Listen calls the connection to be create and begins serving requests.
func (w *UDPPod) Listen(sm client.SystemServer, encoding octo.MessageEncoding) error {
	w.cnl.Lock()
	if w.started {
		w.cnl.Unlock()
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
func (w *UDPPod) Close() error {
	if w.isClose() {
		return consts.ErrClosedConnection
	}

	w.notify(client.ClosedHandler, nil)

	w.cnl.Lock()
	w.doClose = true
	w.cnl.Unlock()

	if err := w.conn.Close(); err != nil {
		w.notify(client.ErrorHandler, err)

		w.cnl.Lock()
		w.conn = nil
		w.started = false
		w.cnl.Unlock()
		return err
	}

	w.wg.Wait()

	w.cnl.Lock()
	w.conn = nil
	w.started = false
	w.cnl.Unlock()

	return nil
}

// Register registers the handler for a given handler.
func (w *UDPPod) Register(tm client.StateHandlerType, hmi interface{}) {
	w.pub.Register(tm, hmi)
}

// notify calls the giving callbacks for each different type of state.
func (w *UDPPod) notify(n client.StateHandlerType, err error) {
	var cm octo.Contact

	if w.curAddr != nil {
		cm = w.curAddr.contact
	}

	w.pub.Notify(n, cm, w, err)
}

// SendWithAddr delivers the giving message to the underline websocket connection.
func (w *UDPPod) SendWithAddr(addr *net.UDPAddr, data interface{}, flush bool) error {

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

	if !flush {
		w.bm.Write(dataBytes)
		return nil
	}

	// fmt.Printf("Buffer: %+q\n", w.bm.Bytes())

	if w.bm.Len() > 0 {
		w.conn.Write(w.bm.Bytes(), false)
		w.bm.Reset()
	}

	return w.conn.WriteUDP(dataBytes, addr)
}

// Send delivers the giving message to the underline websocket connection.
func (w *UDPPod) Send(data interface{}, flush bool) error {

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

	if !flush {
		w.bm.Write(dataBytes)
		return nil
	}

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
func (w *UDPPod) prepareServers() error {

	// Add the main addr if provided.
	if w.attr.Addr != "" {
		w.attr.Addr = netutils.GetAddr(w.attr.Addr)

		ep, _ := url.Parse(w.attr.Addr)

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

		ep, _ := url.Parse(addr)

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
func (w *UDPPod) getNextServer() error {
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
func (w *UDPPod) reconnect() error {

	// Validate that if authentication is required, that atleast the
	// scheme is not empty, since we can validate the other fields as
	// they are not set and will change based on the scheme.
	cred := w.system.Credential()
	if w.attr.Authenticate && cred.Scheme == "" {
		return consts.ErrNonEmptyCredentailFieldsRequired
	}

	w.cnl.Lock()
	if w.started {
		w.notify(client.DisconnectHandler, nil)
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

	conn, err := NewUDPConn(w.udpVersion, w.localAddr, addr)
	if err != nil {
		w.notify(client.DisconnectHandler, err)
		w.curAddr.drops++
		return w.reconnect()
	}

	w.cnl.Lock()
	w.started = true
	w.conn = conn
	w.curAddr.connected = true
	w.curAddr.reconnecting = false
	w.cnl.Unlock()

	w.notify(client.ConnectHandler, nil)

	if err := w.initConnectionSetup(); err != nil {
		return err
	}

	go w.acceptRequests()

	return nil
}

// initConnectionSetup defines a function to initiate the connection
// procedure generated for using tcp with octo.TCPServers.
func (w *UDPPod) initConnectionSetup() error {
	if w.shouldClose() {
		return nil
	}

	// If we are expected to authenticate then we need to deliver \
	// authentication details.
	if !w.attr.Authenticate {
		return nil
	}

	// Attempt to negotiate authentication with server, we wont allow multiple
	// messages but only handle one request and if it does not match then fail.
	{
		cmdData, err := jsoni.Parser.Encode(jsoni.AuthMessage{
			Name: string(consts.AuthResponse),
			Data: w.system.Credential(),
		})

		if err != nil {
			return err
		}

		if err := w.conn.Write(cmdData, true); err != nil {
			return err
		}
	}

	// Await OK response, else deny connectivity. If we are permitted then,
	// return as sucessfull.
	{
		data, _, err := w.conn.Read()
		if err != nil {
			return err
		}

		cmds, err := jsoni.Parser.Decode(data)
		if err != nil {
			return err
		}

		commands, ok := cmds.([]jsoni.CommandMessage)
		if !ok {
			return consts.ErrParseError
		}

		if len(commands) == 0 {
			return consts.ErrEmptyMessage
		}

		cmd := commands[0]
		if cmd.Name != string(consts.AuthroizationGranted) {
			return consts.ErrAuthorizationFailed
		}
	}

	return nil
}

// acceptRequests processing the connections for incoming messages.
func (w *UDPPod) acceptRequests() {
	w.wg.Add(1)
	defer w.wg.Done()

	for !w.shouldClose() {
		w.conn.SetReadDeadline(time.Now().Add(consts.WSReadTimeout))

		// fmt.Printf("StartWSSocket:Reading Accept: \n")
		data, addr, err := w.conn.Read()
		// fmt.Printf("Accept::Err: %+q\n", err)
		if err != nil {
			w.notify(client.ErrorHandler, err)

			if err == consts.ErrAbitraryCloseConnection || err == consts.ErrClosedConnection || err == consts.ErrUnstableRead {
				go w.reconnect()
				return
			}

			continue
		}

		val, err := w.encoding.Decode(data)
		if err != nil {
			w.notify(client.ErrorHandler, err)
			continue
		}

		if err := w.system.Serve(val, &ulink{UDPPod: w, targetAddr: addr}); err != nil {
			w.notify(client.ErrorHandler, err)
			continue
		}
	}
}

// ulink defines a wrapper to provide a struct which implements the
// goclient.Stream appropriate for use with requests.
type ulink struct {
	*UDPPod
	targetAddr *net.UDPAddr
}

// Send delivers the giving message to the underline addr.
func (w *ulink) Send(data interface{}, flush bool) error {
	return w.SendWithAddr(w.targetAddr, data, flush)
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
