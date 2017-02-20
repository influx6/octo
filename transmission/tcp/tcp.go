package tcp

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/influx6/faux/context"
	"github.com/influx6/faux/utils"
	"github.com/influx6/octo"
	"github.com/pborman/uuid"
)

var ctrlLine = []byte("\r\n")
var infoReq = []byte("INFO")
var infoRes = []byte("INFORES")
var authRequest = []byte("AUTH")
var authResponse = []byte("AUTHCRED")
var clusterRequest = []byte("CLUSTERS")
var clusterResponse = []byte("CLUSTERRES")

// Transmission defines a structure which implements the the octo.Transmission
// interface.
type Transmission struct {
	client *Client
	ctx    context.Context
}

// Cluster provides a system to initiate a request for new cluster items.
func (t *Transmission) Cluster(addr string, data []byte) error {
	return nil
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
	return t.client.Info()
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

//================================================================================

// Client defines the tcp struct which implements the octo.Transmission
// interface for communication between a server.
type Client struct {
	clusterClient       bool
	connectionInitiator bool
	closed              bool
	doClosed            bool
	logs                octo.Logs
	conn                net.Conn
	info                octo.Info
	system              octo.System
	server              *Server
	sendg               sync.WaitGroup
	wg                  sync.WaitGroup
	cl                  sync.Mutex
	writer              *bufio.Writer
	buffer              bytes.Buffer
	authCredentials     octo.AuthCredential
}

// Transmission returns a new transmission based on the giving client.
func (c *Client) Transmission() *Transmission {
	return &Transmission{
		client: c,
		ctx:    context.New(),
	}
}

// Close ends the client connections and stops reception/transmission of
// any messages.
func (c *Client) Close() error {
	c.logs.Log(octo.LOGINFO, "tcp.Client.Close", "Started")

	c.cl.Lock()
	if c.closed {
		c.cl.Unlock()
		c.logs.Log(octo.LOGINFO, "tcp.Client.Close", "Completed : Already Closed")
		return nil
	}
	c.cl.Unlock()

	c.logs.Log(octo.LOGINFO, "tcp.Client.Close", "Waiting for all sendRequest to End")
	c.sendg.Wait()

	c.cl.Lock()
	c.closed = true
	c.doClosed = true
	c.cl.Unlock()

	c.logs.Log(octo.LOGINFO, "tcp.Client.Close", "Waiting for all acceptRequests to End")
	c.wg.Wait()

	if err := c.conn.Close(); err != nil {
		c.logs.Log(octo.LOGERROR, "tcp.Client.Close", "Completed : %s", err)
		return err
	}

	c.logs.Log(octo.LOGINFO, "tcp.Client.Close", "Completed")
	return nil
}

// transformTLS transform the internal client connection into a TLS
// connection.
func (c *Client) transformTLS() error {
	c.logs.Log(octo.LOGINFO, "tcp.Client.transformTLS", "Started : %#v", c.info)

	conn := c.conn

	tlsConn := tls.Server(conn, c.server.ServerAttr.TLS)
	ttl := secondsToDuration(tlsTimeout * float64(time.Second))

	var tlsPassed bool

	time.AfterFunc(ttl, func() {
		c.logs.Log(octo.LOGINFO, "tpc.Client.transfromTLS", "TLS Handshake Timeout : Status[%s] : Addr[%s]", tlsPassed, conn.RemoteAddr().String())

		// Once the time has elapsed, close the connection and nil out.
		if !tlsPassed {
			tlsConn.SetReadDeadline(time.Time{})
			tlsConn.Close()
		}
	})

	tlsConn.SetReadDeadline(time.Now().Add(ttl))

	if err := tlsConn.Handshake(); err != nil {
		c.logs.Log(octo.LOGERROR, "tpc.Client.transfromTLS", "TLS Handshake Failed : %s : %+s", conn.RemoteAddr().String(), err)
		tlsConn.SetReadDeadline(time.Time{})
		tlsConn.Close()
		return err
	}

	c.conn = tlsConn

	c.logs.Log(octo.LOGINFO, "tpc.Client.transfromTLS", "Completed")
	return nil
}

func secondsToDuration(seconds float64) time.Duration {
	ttl := seconds * float64(time.Second)
	return time.Duration(ttl)
}

// ErrInvalidResponse defines a response recieved when attempting to read data.
var ErrInvalidResponse = errors.New("Invalid Response")

// ErrAuthInvalidResponse defines a response recieved when attempting to read
// authentication data from the connection.
var ErrAuthInvalidResponse = errors.New("Invalid Response")

// Listen calls the client to begin listening for connection requests.
func (c *Client) Listen() error {
	c.logs.Log(octo.LOGINFO, "tcp.Client.Listen", "New Client Listen: %+q", c.info)

	if c.server.ServerAttr.TLS != nil {
		if err := c.transformTLS(); err != nil {
			c.logs.Log(octo.LOGERROR, "tcp.Client.Listen", "New Client : Initialization Failed : %s", err)
			return err
		}
	}

	if c.clusterClient && c.connectionInitiator {
		if err := c.initInfoNegotiation(); err != nil {
			c.logs.Log(octo.LOGERROR, "tcp.Client.Listen", "New Client : Initialization Failed : %s", err)
			return err
		}

		if err := c.initAuthNegotiation(); err != nil {
			c.logs.Log(octo.LOGERROR, "tcp.Client.Listen", "New Client : Initialization Failed : %s", err)
			return err
		}
	}

	c.wg.Add(1)
	go c.acceptRequests()

	c.logs.Log(octo.LOGERROR, "tcp.Client.Listen", "Complete")
	return nil
}

// SendAll sends the giving data to all clients and clusters the giving
// set of data.
func (c *Client) SendAll(data []byte, flush bool) error {
	c.logs.Log(octo.LOGINFO, "tcp.Client.SendAll", "Data Transmission to All : {%+q}", data)

	if err := c.Send(data, flush); err != nil {
		c.logs.Log(octo.LOGERROR, "tcp.Client.Send", "Completed : %+q", err)
		return err
	}

	for _, cu := range c.server.clients {
		if err := cu.Send(data, flush); err != nil {
			c.logs.Log(octo.LOGERROR, "tcp.Client.Send", "Unable to deliver for %+q : %+q", cu.info, err)
		}
	}

	for _, cu := range c.server.clusters {
		if err := cu.Send(data, flush); err != nil {
			c.logs.Log(octo.LOGERROR, "tcp.Client.Send", "Unable to deliver for %+q : %+q", cu.info, err)
		}
	}

	c.logs.Log(octo.LOGINFO, "tcp.Client.Send", "Completed")
	return nil
}

// ErrDataOversized is delivered when the provide data passes the maximum allowed
// data size.
var ErrDataOversized = errors.New("Data size is to big")

// Send delivers a message into the clients connection stream.
func (c *Client) Send(data []byte, flush bool) error {
	c.logs.Log(octo.LOGINFO, "tcp.Client.Send", "Data Transmission : {%+q}", data)
	if data == nil {
		c.logs.Log(octo.LOGINFO, "tcp.Client.Send", "Completed")
		return nil
	}

	c.sendg.Add(1)
	defer c.sendg.Done()

	if len(data) > maxPayload {
		c.logs.Log(octo.LOGERROR, "tcp.Client.Send", "Completed : %s", ErrDataOversized)
		return ErrDataOversized
	}

	if !bytes.HasSuffix(data, ctrlLine) {
		data = append(data, ctrlLine...)
	}

	if c.writer != nil && c.conn != nil {
		var deadline bool

		if c.writer.Available() < len(data) {
			c.conn.SetWriteDeadline(time.Now().Add(flushDeline))
			deadline = true
		}

		_, err := c.writer.Write(data)
		if err == nil && flush {
			err = c.writer.Flush()
		}

		if deadline {
			c.conn.SetWriteDeadline(time.Time{})
		}

		if err != nil {
			c.logs.Log(octo.LOGERROR, "tcp.Client.Send", "Completed : %s", ErrDataOversized)
			return err
		}
	}

	c.logs.Log(octo.LOGINFO, "tcp.Client.Send", "Completed")
	return nil
}

// Info returns the client and server information.
func (c *Client) Info() (octo.Info, octo.Info) {
	return c.info, c.server.info
}

// acceptRequests beings listening for messages from the giving connection.
func (c *Client) acceptRequests() {
	c.logs.Log(octo.LOGINFO, "tcp.Client.acceptRequests", "Started")
	defer c.wg.Done()

	block := make([]byte, minDataSize)

	for {
		c.cl.Lock()
		if c.closed {
			c.cl.Unlock()
			break
		}
		c.cl.Unlock()

		if c.buffer.Len() > 0 {
			if err := c.Send(c.buffer.Bytes(), true); err != nil {
				c.logs.Log(octo.LOGERROR, "tcp.Client.acceptRequests", "Sending buffer failed : %+s", err)
				continue
			}
		}

		c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))

		n, err := c.conn.Read(block)
		if err != nil {
			c.logs.Log(octo.LOGERROR, "tcp.Client.acceptRequests", "Read Error : %+s", err)
			c.conn.SetReadDeadline(time.Time{})

			c.cl.Lock()
			{
				if !c.doClosed {
					c.cl.Unlock()
					go c.Close()
					return
				}
			}
			c.cl.Unlock()

			break
		}

		c.conn.SetReadDeadline(time.Time{})

		if err := c.system.Serve(block[:n], c.Transmission()); err != nil {
			c.logs.Log(octo.LOGERROR, "tcp.Client.acceptRequests", "Read Error : %+s", err)

			c.cl.Lock()
			{
				if !c.doClosed {
					c.cl.Unlock()
					go c.Close()
					return
				}
			}
			c.cl.Unlock()

			break
		}

		if n == len(block) && len(block) < maxDataWrite {
			block = make([]byte, len(block)*2)
		}

		if n < len(block)/2 && len(block) > maxDataWrite {
			block = make([]byte, len(block)/2)
		}
	}

	c.logs.Log(octo.LOGINFO, "tcp.Client.acceptRequests", "Completed")
}

// handleRequest processes data requests coming in from the client's internal
// connection.
func (c *Client) handleRequest(data []byte, tx octo.Transmission) error {
	c.logs.Log(octo.LOGINFO, "tcp.Client.acceptRequests", "Finished")

	return nil
}

// initClusterNegotiation initiates the negotiation of cluster information.
func (c *Client) initClusterNegotiation() error {
	c.logs.Log(octo.LOGINFO, "tcp.Client.initClusterNegotiation", "Client Cluster Negotiation")

	block := make([]byte, minDataSize)

	c.Send(utils.WrapResponseBlock(clusterRequest, nil), true)

	dataLen, err := c.conn.Read(block)
	if err != nil {
		c.logs.Log(octo.LOGERROR, "tcp.Client.initInfoNegotiation", "Clinet Negotiation : Initialization Failed : %s", err)
		return err
	}

	if dataLen == 0 {
		c.logs.Log(octo.LOGERROR, "tcp.Client.initInfoNegotiation", "Clinet Negotiation : Initialization Failed : %s", ErrInvalidResponse)
		return ErrInvalidResponse
	}

	messages, err := utils.BlockParser.Parse(block[:dataLen])
	if err != nil {
		c.logs.Log(octo.LOGERROR, "tcp.Client.initInfoNegotiation", "Clinet Negotiation : Initialization Failed : %s", err)
		return err
	}

	if len(messages) < 1 {
		c.logs.Log(octo.LOGERROR, "tcp.Client.initInfoNegotiation", "Clinet Negotiation : Initialization Failed : %s : Expected Two Data Packs {INFO}:{CREDENTIALS}", ErrInvalidResponse)
		return ErrInvalidResponse
	}

	if !bytes.Equal(messages[0].Command, clusterRequest) {
		c.logs.Log(octo.LOGERROR, "tcp.Client.initAuthNegotiation", "Clinet Negotiation : Initialization Failed : %s : Expected Two Data Packs {INFO}:{CREDENTIALS}", ErrAuthInvalidResponse)
		return ErrAuthInvalidResponse
	}

	var infos []octo.Info

	for _, message := range messages[0].Data {
		var info octo.Info
		if err := json.Unmarshal(message, &info); err != nil {
			c.logs.Log(octo.LOGERROR, "tcp.Client.initInfoNegotiation", "Clinet Negotiation : Initialization Failed : %s", err)
			return err
		}

		// If we are matching the same server then skip.
		if info.SUUID == c.info.SUUID {
			continue
		}

		infos = append(infos, info)
	}

	// c.Send(utils.WrapResponseBlock(clusterRequest, nil), true)

	c.server.handleClusters(infos)

	c.logs.Log(octo.LOGINFO, "tcp.Client.initClusterNegotiation", "Completed")
	return nil
}

// initInfoNegotiation intiaites the negotiation of client connections.
func (c *Client) initInfoNegotiation() error {
	c.logs.Log(octo.LOGINFO, "tcp.Client.initInfoNegotiation", "Client Negotiation")

	block := make([]byte, minDataSize)

	c.Send(utils.WrapResponseBlock(infoReq, nil), true)

	dataLen, err := c.conn.Read(block)
	if err != nil {
		c.logs.Log(octo.LOGERROR, "tcp.Client.initInfoNegotiation", "Clinet Negotiation : Initialization Failed : %s", err)
		return err
	}

	if dataLen == 0 {
		c.logs.Log(octo.LOGERROR, "tcp.Client.initInfoNegotiation", "Clinet Negotiation : Initialization Failed : %s", ErrInvalidResponse)
		return ErrInvalidResponse
	}

	messages, err := utils.BlockParser.Parse(block[:dataLen])
	if err != nil {
		c.logs.Log(octo.LOGERROR, "tcp.Client.initInfoNegotiation", "Clinet Negotiation : Initialization Failed : %s", err)
		return err
	}

	if len(messages) < 1 {
		c.logs.Log(octo.LOGERROR, "tcp.Client.initInfoNegotiation", "Clinet Negotiation : Initialization Failed : %s : Expected Two Data Packs {INFO}:{CREDENTIALS}", ErrInvalidResponse)
		return ErrInvalidResponse
	}

	if !bytes.Equal(messages[0].Command, infoRes) {
		c.logs.Log(octo.LOGERROR, "tcp.Client.initAuthNegotiation", "Clinet Negotiation : Initialization Failed : %s : Expected Two Data Packs {INFO}:{CREDENTIALS}", ErrAuthInvalidResponse)
		return ErrAuthInvalidResponse
	}

	var info octo.Info

	infoData := bytes.Join(messages[0].Data, []byte(""))
	if err := json.Unmarshal(infoData, &info); err != nil {
		c.logs.Log(octo.LOGERROR, "tcp.Client.initInfoNegotiation", "Clinet Negotiation : Initialization Failed : %s", err)
		return err
	}

	c.info = info

	return nil
}

// initAuthNegotiation intiaites the negotiation of client connections.
func (c *Client) initAuthNegotiation() error {
	c.Send(utils.WrapResponseBlock(authRequest, nil), true)

	block := make([]byte, minDataSize)

	dataLen, err := c.conn.Read(block)
	if err != nil {
		c.logs.Log(octo.LOGERROR, "tcp.Client.initAuthNegotiation", "Clinet Negotiation : Initialization Failed : %s", err)
		return err
	}

	if dataLen == 0 {
		c.logs.Log(octo.LOGERROR, "tcp.Client.initAuthNegotiation", "Clinet Negotiation : Initialization Failed : %s", ErrInvalidResponse)
		return ErrInvalidResponse
	}

	messages, err := utils.BlockParser.Parse(block[:dataLen])
	if err != nil {
		c.logs.Log(octo.LOGERROR, "tcp.Client.initAuthNegotiation", "Clinet Negotiation : Initialization Failed : %s", err)
		return err
	}

	if len(messages) < 1 {
		c.logs.Log(octo.LOGERROR, "tcp.Client.initAuthNegotiation", "Clinet Negotiation : Initialization Failed : %s : Expected Two Data Packs {INFO}:{CREDENTIALS}", ErrInvalidResponse)
		return ErrInvalidResponse
	}

	if !bytes.Equal(messages[0].Command, authResponse) {
		c.logs.Log(octo.LOGERROR, "tcp.Client.initAuthNegotiation", "Clinet Negotiation : Initialization Failed : %s : Expected Two Data Packs {INFO}:{CREDENTIALS}", ErrAuthInvalidResponse)
		return ErrAuthInvalidResponse
	}

	var auth octo.AuthCredential

	authData := bytes.Join(messages[0].Data, []byte(""))
	if err := json.Unmarshal(authData, &auth); err != nil {
		c.logs.Log(octo.LOGERROR, "tcp.Client.initAuthNegotiation", "Clinet Negotiation : Initialization Failed : %s", err)
		return err
	}

	if err := c.system.Authenticate(auth); err != nil {
		c.logs.Log(octo.LOGERROR, "tcp.Client.initAuthNegotiation", "Clinet Negotiation : Authentication Failed : %s", err)
		return err
	}

	return nil
}

//================================================================================

// ServerAttr defines a struct which holds the attributes and config values for a
// tcp.Server.
type ServerAttr struct {
	Addr        string
	ClusterAddr string
	TLS         *tls.Config
}

// Server defines a core structure which manages the intenals
// of the way tcp works.
type Server struct {
	ServerAttr
	running         bool
	clusterAddr     string
	logs            octo.Logs
	info            octo.Info
	listener        net.Listener
	clusterListener net.Listener
	wg              sync.WaitGroup
	cg              sync.WaitGroup
	rl              sync.Mutex
	clients         []*Client
	clusters        []*Client
}

// NewServer returns a new Server which handles connections from clients and
// clusters if allowed.
func NewServer(logs octo.Logs, attr ServerAttr) *Server {
	suuid := uuid.NewUUID().String()

	var s Server
	s.ServerAttr = attr
	s.logs = logs
	s.info = octo.Info{
		Remote: attr.Addr,
		Addr:   attr.Addr,
		UUID:   suuid,
		SUUID:  suuid,
	}

	return &s
}

// Wait causes a wait on the server.
func (s *Server) Wait() {
	s.wg.Wait()
}

// Close returns the error from closing the listener.
func (s *Server) Close() error {
	s.logs.Log(octo.LOGINFO, "tcp.Server.Close", "Started : %#v", s.info)

	s.rl.Lock()
	if !s.running {
		s.rl.Unlock()
		return nil
	}
	s.rl.Unlock()

	s.rl.Lock()
	s.running = false
	s.rl.Unlock()

	s.wg.Wait()

	if err := s.listener.Close(); err != nil {
		s.logs.Log(octo.LOGERROR, "tcp.Server.Close", "Completed : Close Error : %+s", err)
		return err
	}

	return nil
}

// Listen sets up the listener and begins listening for connection requests from
// the listener.
func (s *Server) Listen(system octo.System) error {
	s.logs.Log(octo.LOGINFO, "tcp.Server.Listen", "Started : %#v", s.info)

	s.rl.Lock()
	if s.running {
		s.rl.Unlock()
		s.logs.Log(octo.LOGINFO, "tcp.Server.Listen", "Ended : Already Running : %#v", s.info)
		return nil
	}

	var err error

	s.listener, err = net.Listen("tcp", s.info.Addr)
	if err != nil {
		s.rl.Unlock()
		s.logs.Log(octo.LOGERROR, "tcp.Server.Listen", "Failed to start client listener: %s", err.Error())
		return err
	}

	s.logs.Log(octo.LOGERROR, "tcp.Server.Listen", "New Client Listener : %s", s.listener.Addr())

	if s.clusterAddr != "" {
		s.clusterListener, err = net.Listen("tcp", s.clusterAddr)
		if err != nil {
			s.rl.Unlock()
			s.logs.Log(octo.LOGERROR, "tcp.Server.Listen", "Failed to start cluster listener : %s", err.Error())
			return err
		}

		s.logs.Log(octo.LOGERROR, "tcp.Server.Listen", "New Cluster Listener : %s", s.listener.Addr())
	}

	s.running = true
	s.rl.Unlock()

	if s.clusterListener != nil {
		s.wg.Add(2)

		go s.handleClientConnections(system)
		go s.handleClusterConnections()
	} else {
		s.wg.Add(1)
		go s.handleClientConnections(system)
	}

	s.logs.Log(octo.LOGINFO, "tcp.Server.Listen", "Completed")
	return nil
}

func (s *Server) hasClusterInfo(info octo.Info) bool {
	for _, cu := range s.clusters {
		if cu.info.UUID == info.UUID {
			return true
		}
	}

	return false
}

// handleClusters defines a function to handle connection to clusters.
func (s *Server) handleClusters(infos []octo.Info) {
	var filtered []octo.Info

	for _, info := range infos {
		if s.hasClusterInfo(info) {
			continue
		}

		filtered = append(filtered, info)
	}

	fmt.Printf("Clusters: %#v", filtered)
}

const minTempSleep = 10 * time.Millisecond
const maxSleepTime = 2 * time.Second
const minSleepTime = 10 * time.Millisecond
const flushDeline = 2 * time.Second
const minDataSize = 512
const maxConnections = (64 * 1024)
const maxPayload = (1024 * 1024)
const tlsTimeout = float64(500&time.Millisecond) / float64(time.Second)
const authTimeout = float64(2*tlsTimeout) / float64(time.Second)
const maxDataWrite = 6048

// handleClientConnections handles the connection from client providers.
func (s *Server) handleClientConnections(system octo.System) {
	s.logs.Log(octo.LOGINFO, "tcp.Server.handleClientConnections", "Started")
	defer s.wg.Done()

	sleepTime := minSleepTime

	s.logs.Log(octo.LOGINFO, "tcp.Server.handleClientConnections", "Initiating Accept Loop")
	for {
		// TODO: Is there a way of avoiding this mutex in such a hot path? s.rl.Lock()
		if !s.running {
			s.rl.Unlock()
			return
		}
		s.rl.Unlock()

		conn, err := s.listener.Accept()
		if err != nil {
			s.logs.Log(octo.LOGERROR, "Server.handleClientConnections", "Error : %s", err.Error())
			if opError, ok := err.(*net.OpError); ok {
				if opError.Op == "accept" {
					break
				}
			}

			if tmpError, ok := err.(net.Error); ok && tmpError.Temporary() {
				s.logs.Log(octo.LOGERROR, "Server.handleClientConnections", "Temporary Error : %s : Sleeping %dms", err.Error(), sleepTime/time.Millisecond)
				time.Sleep(sleepTime)
				sleepTime *= 2
				if sleepTime > maxSleepTime {
					sleepTime = minSleepTime
				}

				continue
			}
		}

		s.logs.Log(octo.LOGINFO, "tcp.Server.handleClientConnections", "New Client : Intiaiting Client Creation Process: : Addr[%q]", "BOB")

		localAddr := conn.LocalAddr().String()
		remoteAddr := conn.RemoteAddr().String()
		clientID := uuid.NewUUID().String()

		s.logs.Log(octo.LOGINFO, "tcp.Server.handleClientConnections", "New Client : Local[%q] : Remote[%q]", localAddr, remoteAddr)

		var client Client
		client = Client{
			server: s,
			conn:   conn,
			logs:   s.logs,
			system: system,
			info: octo.Info{
				Addr:   localAddr,
				Remote: remoteAddr,
				UUID:   clientID,
				SUUID:  s.info.SUUID,
			},
			clusterClient:       false,
			connectionInitiator: false,
			writer:              bufio.NewWriter(conn),
		}

		if err := client.Listen(); err != nil {
			s.logs.Log(octo.LOGERROR, "tcp.Server.handleClientConnections", "New Client Error : %s", err.Error())
			client.conn.Close()
			continue
		}

		s.clients = append(s.clients, &client)
		continue

	}

	s.logs.Log(octo.LOGINFO, "tcp.Server.handleClientConnections", "Started")
}

// handles the connection of cluster servers and intializes the needed operations
// and procedures in getting clusters servers initialized.
func (s *Server) handleClusterConnections() {
	s.logs.Log(octo.LOGINFO, "tcp.Server.handleClusterConnections", "Started")
	defer s.wg.Done()

	sleepTime := minSleepTime

	for {
		// TODO: Is there a way of avoiding this mutex in such a hot path? s.rl.Lock()
		if !s.running {
			s.rl.Unlock()
			return
		}
		s.rl.Unlock()

		conn, err := s.listener.Accept()
		if err != nil {
			s.logs.Log(octo.LOGERROR, "Server.handleClientConnections", "Error : %s", err.Error())
			if opError, ok := err.(*net.OpError); ok {
				if opError.Op == "accept" {
					break
				}
			}

			if tmpError, ok := err.(net.Error); ok && tmpError.Temporary() {
				s.logs.Log(octo.LOGERROR, "Server.handleClientConnections", "Temporary Error : %s : Sleeping %dms", err.Error(), sleepTime/time.Millisecond)
				time.Sleep(sleepTime)
				sleepTime *= 2
				if sleepTime > maxSleepTime {
					sleepTime = minSleepTime
				}

				continue
			}
		}

		s.logs.Log(octo.LOGINFO, "tcp.Server.handleClusterConnections", "New Client : Intiaiting Client Creation Process: : Addr[%q]", "BOB")
		localAddr := conn.LocalAddr().String()
		remoteAddr := conn.RemoteAddr().String()
		clientID := uuid.NewUUID().String()

		s.logs.Log(octo.LOGINFO, "tcp.Server.handleClusterConnections", "New Client : Local[%q] : Remote[%q]", localAddr, remoteAddr)

		var client Client
		client = Client{
			server: s,
			conn:   conn,
			logs:   s.logs,
			info: octo.Info{
				Addr:   localAddr,
				Remote: remoteAddr,
				UUID:   clientID,
				SUUID:  s.info.SUUID,
			},
			clusterClient:       true,
			connectionInitiator: false,
			writer:              bufio.NewWriter(conn),
		}

		if err := client.Listen(); err != nil {
			s.logs.Log(octo.LOGERROR, "tcp.Server.handleClusterConnections", "New Client Error : %s", err.Error())
			client.conn.Close()
			continue
		}

		s.clusters = append(s.clusters, &client)
		continue
	}

	s.logs.Log(octo.LOGINFO, "tcp.Server.handleClusterConnections", "Completed")
}
