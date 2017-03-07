package tcpclient

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"errors"
	"net"
	"net/url"
	"sync"
	"time"

	"github.com/influx6/octo"
	"github.com/influx6/octo/clients/goclient"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/netutils"
	"github.com/influx6/octo/parsers/blockparser"
	"github.com/influx6/octo/parsers/byteutils"
	"github.com/influx6/octo/utils"
)

// Attr defines a struct which holds configuration options for the TCP
// client.
type Attr struct {
	MaxReconnets int
	MaxDrops     int
	Addr         string
	Clusters     []string
	Authenticate bool
	TLSConfig    *tls.Config
}

// TCPPod defines a TCP implementation which connects
// to a provided TCP endpoint for making requests.
type TCPPod struct {
	pub         *octo.Pub
	attr        Attr
	instruments octo.Instrumentation
	servers     []*srvAddr
	curAddr     *srvAddr
	bm          bytes.Buffer
	wg          sync.WaitGroup
	system      goclient.System
	encoding    goclient.MessageEncoding
	base        *goclient.BaseSystem
	cnl         sync.Mutex
	doClose     bool
	started     bool
	conn        *TCPConn
}

// New returns a new instance of the TCP pod.
func New(insts octo.Instrumentation, attr Attr) (*TCPPod, error) {
	var pod TCPPod
	pod.pub = octo.NewPub()
	pod.attr = attr
	pod.instruments = insts

	if attr.MaxDrops <= 0 {
		attr.MaxDrops = consts.MaxTotalConnectionFailure
	}

	if attr.MaxDrops <= 0 {
		attr.MaxReconnets = consts.MaxTotalReconnection
	}

	// Prepare all server registering and validate paths.
	if err := pod.prepareServers(); err != nil {
		return nil, err
	}

	return &pod, nil
}

// Listen calls the connection to be create and begins serving requests.
func (w *TCPPod) Listen(sm goclient.System, encoding goclient.MessageEncoding) error {
	w.cnl.Lock()
	if w.started {
		w.cnl.Unlock()
		return nil
	}
	w.cnl.Unlock()

	w.system = sm
	w.encoding = encoding

	// w.base = goclient.NewBaseSystem(
	// 	blockparser.Blocks,
	// 	w.instruments,
	// 	blocksystem.BaseHandlers(),
	// 	blocksystem.AuthHandlers(sm),
	// )

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
func (w *TCPPod) Register(tm octo.StateHandlerType, hmi interface{}) {
	w.pub.Register(tm, hmi)
}

// notify calls the giving callbacks for each different type of state.
func (w *TCPPod) notify(n octo.StateHandlerType, err error) {
	var cm octo.Contact

	if w.curAddr != nil {
		cm = w.curAddr.contact
	}

	w.pub.Notify(n, cm, err)
}

// Send delivers the giving message to the underline TCP connection.
func (w *TCPPod) Send(data interface{}, flush bool) error {

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
	lastAttempt  time.Time
	contact      octo.Contact
}

// prepareServers registered all provided address from the attribute as cycling
// items in a server lists for reducing load on a new server.
func (w *TCPPod) prepareServers() error {

	// Add the main addr if provided.
	if w.attr.Addr != "" {
		ep, _ := url.Parse(w.attr.Addr)

		w.servers = append(w.servers, &srvAddr{
			index:   0,
			ep:      ep,
			addr:    netutils.GetAddr(w.attr.Addr),
			contact: utils.NewContact(netutils.GetAddr(w.attr.Addr)),
		})
	}

	total := len(w.servers)

	for _, addr := range w.attr.Clusters {

		ep, _ := url.Parse(addr)

		w.servers = append(w.servers, &srvAddr{
			index:   total,
			ep:      ep,
			addr:    netutils.GetAddr(addr),
			contact: utils.NewContact(netutils.GetAddr(w.attr.Addr)),
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
		// fmt.Printf("Src: %#v\n", srv)
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
func (w *TCPPod) reconnect() error {
	w.notify(octo.DisconnectHandler, nil)

	if err := w.getNextServer(); err != nil {
		return err
	}

	var addr string
	addr = w.curAddr.addr

	w.cnl.Lock()
	{
		w.curAddr.reconnecting = true
		w.curAddr.connected = false
		w.curAddr.recons++
		w.curAddr.lastAttempt = time.Now()
	}
	w.cnl.Unlock()

	var newConf *tls.Config

	if w.attr.TLSConfig != nil {
		newConf = w.attr.TLSConfig.Clone()
	}

	conn, err := NewTCPConn(addr, newConf)
	if err != nil {
		w.notify(octo.DisconnectHandler, err)

		w.cnl.Lock()
		{
			w.curAddr.connected = false
			w.curAddr.reconnecting = false
			w.curAddr.drops++
		}
		w.cnl.Unlock()

		return w.reconnect()
	}

	w.cnl.Lock()
	{
		w.conn = conn
		w.curAddr.connected = true
		w.curAddr.reconnecting = false
	}
	w.cnl.Unlock()

	w.notify(octo.ConnectHandler, nil)

	if err := w.initConnectionSetup(); err != nil {
		w.notify(octo.ErrorHandler, err)
		return err
	}

	go w.acceptRequests()

	return nil
}

// initConnectionSetup defines a function to initiate the connection
// procedure generated for using tcp with octo.TCPServers.
func (w *TCPPod) initConnectionSetup() error {
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
		data, err := w.conn.Read()
		if err != nil {
			return err
		}

		cmds, err := blockparser.Blocks.Parse(data)
		if err != nil {
			return err
		}

		if len(cmds) == 0 {
			return consts.ErrParseError
		}

		cmd := cmds[0]

		if !bytes.Equal(cmd.Name, consts.AuthRequest) {
			return consts.ErrInvalidRequestForState
		}

		credentialData, err := utils.AuthCredentialToJSON(w.system.Credential())
		if err != nil {
			return err
		}

		// cmdData, _, err := utils.NewCommandByte(consts.AuthResponse, credentialData)
		cmdData := byteutils.MakeByteMessage(consts.AuthResponse, credentialData)

		if err := w.conn.Write(cmdData, true); err != nil {
			return err
		}
	}

	// Await OK response, else deny connectivity. If we are permitted then,
	// return as sucessfull.
	{
		data, err := w.conn.Read()
		if err != nil {
			return err
		}

		cmds, err := blockparser.Blocks.Parse(data)
		if err != nil {
			return err
		}

		if len(cmds) == 0 {
			return consts.ErrParseError
		}

		cmd := cmds[0]

		if !bytes.Equal(cmd.Name, consts.AuthroizationGranted) {
			return consts.ErrAuthorizationFailed
		}
	}

	return nil
}

// acceptRequests processing the connections for incoming messages.
func (w *TCPPod) acceptRequests() {
	w.wg.Add(1)
	defer w.wg.Done()

	for !w.shouldClose() {
		w.conn.SetReadDeadline(time.Now().Add(consts.WSReadTimeout))

		data, err := w.conn.Read()
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
func NewTCPConn(addr string, tlsConf *tls.Config) (*TCPConn, error) {
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

	newConn, err := netutils.UpgradeConnToTLS(conn, tlsConf)
	if err != nil {
		return nil, err
	}

	return &TCPConn{
		conn:          newConn,
		writer:        bufio.NewWriter(newConn),
		reader:        bufio.NewReader(newConn),
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
