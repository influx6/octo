// Package tcp provides a simple package that implements a udp protocol
// transmission for the octo.TranmissionProtocol interface. Which allows a uniform
// response cycle with a tcp based connection. More so the tcp servers support
// clustering and allow delivery messages down the pipeline to all clients in all
// clusters.
package tcp

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/influx6/faux/context"
	"github.com/influx6/octo"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/netutils"
	"github.com/influx6/octo/parsers/blockparser"
	"github.com/influx6/octo/parsers/byteutils"
	"github.com/influx6/octo/systems/blocksystem"
	uuid "github.com/satori/go.uuid"
)

const minTempSleep = 10 * time.Millisecond
const maxSleepTime = 2 * time.Second
const minSleepTime = 10 * time.Millisecond
const flushDeline = 2 * time.Second
const connectDeadline = 4 * time.Second
const minDataSize = 512
const maxConnections = (64 * 1024)
const maxPayload = (1024 * 1024)
const tlsTimeout = float64(500&time.Millisecond) / float64(time.Second)
const authTimeout = float64(2*tlsTimeout) / float64(time.Second)
const maxDataWrite = 6048

// Transmission defines a structure which implements the the octo.Transmission
// interface.
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
	clusterClient         bool
	connectionInitiator   bool
	parser                octo.Parser
	logs                  octo.Logs
	info                  octo.Info
	cinfo                 octo.Info
	system                octo.System
	primarySystem         *octo.BaseSystem
	server                *Server
	sendg                 sync.WaitGroup
	wg                    sync.WaitGroup
	buffer                bytes.Buffer
	authCredentials       octo.AuthCredential
	cl                    sync.Mutex
	running               bool
	doClose               bool
	ml                    sync.Mutex
	stopAcceptingRequests bool
	pl                    sync.Mutex
	writer                *bufio.Writer
	pending               sync.WaitGroup
	conn                  net.Conn
}

// Transmission returns a new transmission based on the giving client.
func (c *Client) Transmission() *Transmission {
	return &Transmission{
		client: c,
		ctx:    context.New(),
	}
}

// Wait calls the internal waiting mechanism for the client connection.
func (c *Client) Wait() {
	c.wg.Wait()
}

// Close ends the client connections and stops reception/transmission of
// any messages.
func (c *Client) Close() error {
	c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.Close", "Started")

	if !c.IsRunning() {
		c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.Close", "Completed : Already Closed")
		return nil
	}

	c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.Close", "Waiting for all sent requests to End")
	c.sendg.Wait()

	c.cl.Lock()
	c.running = false
	c.doClose = true
	c.cl.Unlock()

	// c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.Close", "Waiting for all pending requests to End")
	// c.pending.Wait()
	//
	// c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.Close", "Waiting for all acceptRequests to End")
	// c.wg.Wait()

	if err := c.conn.Close(); err != nil {
		c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.Close", "Completed : %s", err)
	}

	c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.Close", "Completed")
	return nil
}

// transformTLS transform the internal client connection into a TLS
// connection.
func (c *Client) transformTLS() error {
	c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.transformTLS", "Started : %#v", c.info)

	conn := c.conn

	tlsConn := tls.Server(conn, c.server.ServerAttr.TLS)
	ttl := secondsToDuration(tlsTimeout * float64(time.Second))

	var tlsPassed bool

	time.AfterFunc(ttl, func() {
		c.logs.Log(octo.LOGINFO, c.info.UUID, "tpc.Client.transfromTLS", "TLS Handshake Timeout : Status[%s] : Addr[%s]", tlsPassed, conn.RemoteAddr().String())

		// Once the time has elapsed, close the connection and nil out.
		if !tlsPassed {
			tlsConn.SetReadDeadline(time.Time{})
			tlsConn.Close()
		}
	})

	tlsConn.SetReadDeadline(time.Now().Add(ttl))

	if err := tlsConn.Handshake(); err != nil {
		c.logs.Log(octo.LOGERROR, c.info.UUID, "tpc.Client.transfromTLS", "TLS Handshake Failed : %s : %+s", conn.RemoteAddr().String(), err)
		tlsConn.SetReadDeadline(time.Time{})
		tlsConn.Close()
		return err
	}

	c.conn = tlsConn

	c.logs.Log(octo.LOGINFO, c.info.UUID, "tpc.Client.transfromTLS", "Completed")
	return nil
}

func secondsToDuration(seconds float64) time.Duration {
	ttl := seconds * float64(time.Second)
	return time.Duration(ttl)
}

// ErrInvalidResponse defines the error returned when a response recieved
// does not match standards.
var ErrInvalidResponse = errors.New("Invalid Response")

// ErrInvalidInfoResponse defines the error returned when a response recieved
// does not match info response standards.
var ErrInvalidInfoResponse = errors.New("Invalid Response")

// ErrAuthInvalidResponse defines the error returned when a response recieved
// does not match authentication standards and either provides invalid
// authentication data from the connection.
var ErrAuthInvalidResponse = errors.New("Invalid Response")

// Listen calls the client to begin listening for connection requests.
func (c *Client) Listen() error {
	c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.Listen", "Started : Client Listen: %+q", c.info)

	if c.IsRunning() {
		c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.Listen", "Completed")
		return nil
	}

	if c.server.ServerAttr.TLS != nil {
		if err := c.transformTLS(); err != nil {
			c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.Listen", "New Client : Initialization Failed : %s", err)
			return err
		}
	}

	c.cl.Lock()
	{
		c.running = true
	}
	c.cl.Unlock()

	c.pending.Add(1)
	defer c.pending.Done()

	// If we are a clusterClient then attempt to exchange information with new cluster.
	if c.clusterClient {
		if !c.connectionInitiator {
			if err := c.initInfoNegotiation(); err != nil {
				c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.Listen", "New Client : Initialization Failed : %s", err)
				return err
			}
		} else {
			if err := c.initSlaveInfoNegotiation(); err != nil {
				c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.Listen", "New Client : Initialization Failed : %s", err)
				return err
			}
		}
	}

	// Initialize and request authentication from client if allowed.
	if c.server.ServerAttr.Authenticate && !c.connectionInitiator {
		if err := c.initAuthNegotiation(); err != nil {
			c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.Listen", "New Client : Initialization Failed : %s", err)
			return err
		}
	}

	if c.clusterClient && c.connectionInitiator {
		if err := c.initClusterNegotiation(); err != nil {
			c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.Listen", "New Client : Initialization Failed : %s", err)
			return err
		}
	}

	c.wg.Add(1)
	go c.acceptRequests()

	c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.Listen", "Complete")
	return nil
}

// IsRunning returns true/false if the giving server is running.
func (c *Client) IsRunning() bool {
	c.cl.Lock()
	defer c.cl.Unlock()
	return c.running
}

// shouldClose returns true/false if a client is requested to close.
func (c *Client) shouldClose() bool {
	c.cl.Lock()
	defer c.cl.Unlock()
	return c.doClose
}

// acceptRequests beings listening for messages from the giving connection.
func (c *Client) acceptRequests() {
	c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.acceptRequests", "Started")
	defer c.wg.Done()

	var eofSeen int
	block := make([]byte, minDataSize)

	for c.IsRunning() {
		if c.buffer.Len() > 0 {
			if err := c.Send(c.buffer.Bytes(), true); err != nil {
				c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.acceptRequests", "Sending buffer failed : %+s", err)
				continue
			}
		}

		c.conn.SetReadDeadline(time.Now().Add(consts.ReadTimeout))
		c.conn.SetWriteDeadline(time.Now().Add(consts.WriteTimeout))

		n, err := c.conn.Read(block)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}

			c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.acceptRequests", "Read Error : %+s", err)

			// TODO: Do we really want to continue here?
			if err == io.EOF {
				if eofSeen < consts.MaxAcceptableEOF {
					eofSeen++
					continue
				}
			}

			c.conn.SetReadDeadline(time.Time{})
			c.conn.SetWriteDeadline(time.Time{})

			break
		}

		c.conn.SetReadDeadline(time.Time{})
		c.conn.SetWriteDeadline(time.Time{})

		c.logs.Log(octo.LOGTRANSMITTED, c.info.UUID, "tcp.Client.acceptRequests", "Transmitted : %+q", block[:n])
		if err := c.handleRequest(block[:n], c.Transmission()); err != nil {
			c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.acceptRequests", "Read Error : %+s", err)
			break
		}

		if n == len(block) && len(block) < maxDataWrite {
			block = make([]byte, len(block)*2)
		}

		if n < len(block)/2 && len(block) > maxDataWrite {
			block = make([]byte, len(block)/2)
		}

		if c.shouldClose() {
			break
		}
	}

	c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.acceptRequests", "Completed")
}

// handleRequest processes data requests coming in from the client's internal
// connection.
func (c *Client) handleRequest(data []byte, tx octo.Transmission) error {
	c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.handleRequest", "Started")

	if c.primarySystem == nil {
		c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.handleRequest", "Completed")
		return c.system.Serve(data, tx)
	}

	unserved, err := c.primarySystem.ServeBase(data, tx)
	if err != nil {
		c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.handleRequest", "Completed : Error : %+s", err)
		return err
	}

	if len(unserved) == 0 {
		c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.handleRequest", "Completed")
		return nil
	}

	c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.handleRequest", "Completed")
	return c.system.Serve(byteutils.JoinMessages(unserved...), tx)
}

// initClusterNegotiation initiates the negotiation of cluster information.
func (c *Client) initClusterNegotiation() error {
	c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.initClusterNegotiation", "Client Cluster Negotiation")

	block := make([]byte, minDataSize)

	c.Send(byteutils.WrapResponseBlock(consts.ClusterRequest, nil), true)

	dataLen, err := c.conn.Read(block)
	if err != nil {
		c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initClusterNegotiation", "Client Negotiation : Initialization Failed : %s", err)
		return err
	}

	if dataLen == 0 {
		c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initClusterNegotiation", "Client Negotiation : Initialization Failed : %s", ErrInvalidResponse)
		return ErrInvalidResponse
	}

	messages, err := c.parser.Parse(block[:dataLen])
	if err != nil {
		c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initClusterNegotiation", "Client Negotiation : Initialization Failed : %s", err)
		return err
	}

	c.logs.Log(octo.LOGDEBUG, c.info.UUID, "tcp.Client.initClusterNegotiation", "Parsed : %+q", messages)

	if len(messages) < 1 {
		c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initClusterNegotiation", "Client Negotiation : Initialization Failed : %s : Expected Two Data Packs {INFO}:{CREDENTIALS}", ErrInvalidResponse)
		return ErrInvalidResponse
	}

	if !bytes.Equal(messages[0].Name, consts.ClusterResponse) {
		c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initAuthNegotiation", "Client Negotiation : Initialization Failed : %s : Expected Two Data Packs {INFO}:{CREDENTIALS}", ErrAuthInvalidResponse)
		return ErrAuthInvalidResponse
	}

	var infos []octo.Info

	for _, message := range messages[0].Data {
		var info octo.Info
		if err := json.Unmarshal(message, &info); err != nil {
			c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initClusterNegotiation", "Client Negotiation : Initialization Failed : %s", err)
			return err
		}

		// If we are matching the same server then skip.
		if info.SUUID == c.info.SUUID {
			continue
		}

		infos = append(infos, info)
	}

	c.Send(byteutils.MakeByteMessage(consts.ClusterPostOK, nil), true)

	c.server.HandleClusters(infos)

	c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.initClusterNegotiation", "Completed")
	return nil
}

// initSlaveInfoNegotiation initializes the client negotation when the giving client
// is not the initiator of the of the cluster connection.
func (c *Client) initSlaveInfoNegotiation() error {
	c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.initSlaveInfoNegotiation", "Client Negotiation")

	// Block attempts to read the request for cluster information.
	{
		block, err := c.temporaryRead()
		if err != nil {
			c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initSlaveInfoNegotiation", "Client Negotiation : Initialization Failed : %s", err)
			return err
		}

		messages, err := c.parser.Parse(block)
		if err != nil {
			c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initSlaveInfoNegotiation", "Client Negotiation : Initialization Failed : %s", err)
			return err
		}

		c.logs.Log(octo.LOGDEBUG, c.info.UUID, "tcp.Client.initSlaveInfoNegotiation", "Parsed : %+q", messages)

		if len(messages) < 1 {
			c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initSlaveInfoNegotiation", "Client Negotiation : Initialization Failed : %s : Expected Atleast One Response", ErrInvalidResponse)
			return ErrInvalidResponse
		}

		if !bytes.Equal(messages[0].Name, consts.InfoRequest) {
			c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initAuthNegotiation", "Client Negotiation : Initialization Failed : %s : Expected Two Data Packs {INFO}:{CREDENTIALS}", ErrInvalidInfoResponse)
			return ErrInvalidInfoResponse
		}

		sinfoData, err := json.Marshal(c.server.clusterInfo)
		if err != nil {
			c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initSlaveInfoNegotiation", "Client Negotiation : Initialization Failed : %s", err)
			return err
		}

		if err := c.Send(byteutils.MakeByteMessage(consts.InfoResponse, sinfoData), true); err != nil {
			c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initSlaveInfoNegotiation", "Started : Client Negotiation : Initialization Failed : %s", err)
			return err
		}
	}

	// Block attempts to validate the OK response for the cluster information delivered.
	{
		block, err := c.temporaryRead()
		if err != nil {
			c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initSlaveInfoNegotiation", "Client Negotiation : Initialization Failed : %s", err)
			return err
		}

		messages, err := c.parser.Parse(block)
		if err != nil {
			c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initSlaveInfoNegotiation", "Client Negotiation : Initialization Failed : %s", err)
			return err
		}

		c.logs.Log(octo.LOGDEBUG, c.info.UUID, "tcp.Client.initSlaveInfoNegotiation", "Parsed : %+q", messages)

		if len(messages) < 1 {
			c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initSlaveInfoNegotiation", "Client Negotiation : Initialization Failed : %s : Expected atleast one Response", ErrInvalidResponse)
			return ErrInvalidResponse
		}

		if !bytes.Equal(messages[0].Name, consts.OK) {
			c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initAuthNegotiation", "Client Negotiation : Initialization Failed : %s : Expected OK", ErrInvalidResponse)
			return ErrInvalidResponse
		}
	}

	c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.initSlaveInfoNegotiation", "Completed")

	// Are we required to authenticate first, then run slave authentication procedure first.
	if c.server.Authenticate {
		if err := c.initSlaveAuthNegotiation(); err != nil {
			c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initAuthNegotiation", "Client Negotiation : Initialization Failed : %s : Expected Authentication Success", err)
			return err
		}
	}

	// Now attempt to retreive information from cluster as well.
	return c.initInfoNegotiation()
}

// initInfoNegotiation intiaites the negotiation of client connections.
func (c *Client) initInfoNegotiation() error {
	c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.initInfoNegotiation", "Started : Client Negotiation")

	c.Send(byteutils.WrapResponseBlock(consts.InfoRequest, nil), true)

	block, err := c.temporaryRead()
	if err != nil {
		c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initInfoNegotiation", "Client Negotiation : Initialization Failed : %s", err)
		return err
	}

	messages, err := c.parser.Parse(block)
	if err != nil {
		c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initInfoNegotiation", "Client Negotiation : Initialization Failed : %s", err)
		return err
	}

	c.logs.Log(octo.LOGDEBUG, c.info.UUID, "tcp.Client.initInfoNegotiation", "Parsed : %+q", messages)

	if len(messages) < 1 {
		c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initInfoNegotiation", "Client Negotiation : Initialization Failed : %s : Expected Two Data Packs {INFO}:{CREDENTIALS}", ErrInvalidResponse)
		return ErrInvalidResponse
	}

	if !bytes.Equal(messages[0].Name, consts.InfoResponse) {
		c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initAuthNegotiation", "Client Negotiation : Initialization Failed : %s : Expected Two Data Packs {INFO}:{CREDENTIALS}", ErrAuthInvalidResponse)
		return ErrAuthInvalidResponse
	}

	var info octo.Info

	infoData := bytes.Join(messages[0].Data, []byte(""))
	if err := json.Unmarshal(infoData, &info); err != nil {
		c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initInfoNegotiation", "Client Negotiation : Initialization Failed : %s", err)
		return err
	}

	// Update the UUID to the servers UUID from the cluster.
	// c.info.UUID = info.SUUID
	// c.info.Remote = info.Addr
	c.cinfo = c.info
	c.info = info
	c.info.Local = c.cinfo.Addr

	// Restore server info as this client belongs here.
	c.info.SUUID = c.cinfo.SUUID

	c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.initInfoNegotiation", "New Client Info : %#v", c.info)

	c.Send(byteutils.MakeByteMessage(consts.OK, nil), true)

	c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.initInfoNegotiation", "Completed")
	return nil
}

// initSlaveAuthNegotiation intiaites the negotiation of client connections.
func (c *Client) initSlaveAuthNegotiation() error {
	c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.initSlaveAuthNegotiation", "Started : Client Negotiation")

	// Block attempts to read the request for cluster information.
	{
		block, err := c.temporaryRead()
		if err != nil {
			c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initSlaveAuthNegotiation", "Client Negotiation : Initialization Failed : %s", err)
			return err
		}

		messages, err := c.parser.Parse(block)
		if err != nil {
			c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initSlaveAuthNegotiation", "Client Negotiation : Initialization Failed : %s", err)
			return err
		}

		c.logs.Log(octo.LOGDEBUG, c.info.UUID, "tcp.Client.initSlaveAuthNegotiation", "Parsed : %+q", messages)

		if len(messages) < 1 {
			c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initSlaveAuthNegotiation", "Client Negotiation : Initialization Failed : %s : Expected atleast one Response", ErrInvalidResponse)
			return ErrInvalidResponse
		}

		if !bytes.Equal(messages[0].Name, consts.AuthRequest) {
			c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initAuthNegotiation", "Client Negotiation : Initialization Failed : %s : Expected Two Data Packs {INFO}:{CREDENTIALS}", ErrInvalidInfoResponse)
			return ErrAuthInvalidResponse
		}

		sauthData, err := json.Marshal(c.server.Credential())
		if err != nil {
			c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initSlaveAuthNegotiation", "Client Negotiation : Initialization Failed : %s", err)
			return err
		}

		if err := c.Send(byteutils.MakeByteMessage(consts.AuthResponse, sauthData), true); err != nil {
			c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initSlaveAuthNegotiation", "Started : Client Negotiation : Initialization Failed : %s", err)
			return err
		}
	}

	// Block attempts to validate the OK response for the cluster information delivered.
	{
		block, err := c.temporaryRead()
		if err != nil {
			c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initSlaveAuthNegotiation", "Client Negotiation : Initialization Failed : %s", err)
			return err
		}

		messages, err := c.parser.Parse(block)
		if err != nil {
			c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initSlaveAuthNegotiation", "Client Negotiation : Initialization Failed : %s", err)
			return err
		}

		c.logs.Log(octo.LOGDEBUG, c.info.UUID, "tcp.Client.initSlaveAuthNegotiation", "Parsed : %+q", messages)

		if len(messages) < 1 {
			c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initSlaveAuthNegotiation", "Client Negotiation : Initialization Failed : %s : Expected atleast One Response", ErrInvalidResponse)
			return ErrInvalidResponse
		}

		if !bytes.Equal(messages[0].Name, consts.OK) {
			c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initAuthNegotiation", "Client Negotiation : Initialization Failed : %s : Expected OK", ErrAuthInvalidResponse)
			return ErrAuthInvalidResponse
		}
	}

	c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.initSlaveAuthNegotiation", "Completed")
	return nil
}

// initAuthNegotiation intiaites the negotiation of client connections.
func (c *Client) initAuthNegotiation() error {
	c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.initAuthNegotiation", "Started")

	c.Send(byteutils.WrapResponseBlock(consts.AuthRequest, nil), true)

	block := make([]byte, minDataSize)

	dataLen, err := c.conn.Read(block)
	if err != nil {
		c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initAuthNegotiation", "Completed : Error : %s ", err)
		return err
	}

	if dataLen == 0 {
		c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initAuthNegotiation", "Completed : Error : %s ", ErrInvalidResponse)
		return ErrInvalidResponse
	}

	messages, err := c.parser.Parse(block[:dataLen])
	if err != nil {
		c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initAuthNegotiation", "Completed : Error : %s ", err)
		return err
	}

	c.logs.Log(octo.LOGDEBUG, c.info.UUID, "tcp.Client.initAuthNegotiation", "Parsed : %+q", messages)

	if len(messages) < 1 {
		c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initAuthNegotiation", "Client Negotiation : Initialization Failed : %s : Expected Two Data Packs {INFO}:{CREDENTIALS}", ErrInvalidResponse)
		return ErrInvalidResponse
	}

	if !bytes.Equal(messages[0].Name, consts.AuthResponse) {
		c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initAuthNegotiation", "Completed : Error : %s ", ErrAuthInvalidResponse)
		return ErrAuthInvalidResponse
	}

	var auth octo.AuthCredential

	authData := bytes.Join(messages[0].Data, []byte(""))
	if err := json.Unmarshal(authData, &auth); err != nil {
		c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initAuthNegotiation", "Client Negotiation : Initialization Failed : %s", err)
		return err
	}

	if err := c.system.Authenticate(auth); err != nil {
		c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initAuthNegotiation", "Client Negotiation : Authentication Failed : %s", err)
		return err
	}

	c.Send(byteutils.MakeByteMessage(consts.OK, nil), true)

	c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.initAuthNegotiation", "Completed")
	return nil
}

func (c *Client) temporaryRead() ([]byte, error) {
	c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.temporaryRead", "Started")
	block := make([]byte, minDataSize)

	var readtimeCount = 0

	for c.IsRunning() {
		c.conn.SetReadDeadline(time.Now().Add(consts.ReadTempTimeout))
		c.conn.SetWriteDeadline(time.Now().Add(consts.WriteTempTimeout))

		c.pl.Lock()
		dataLen, err := c.conn.Read(block)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.temporaryRead", "Client Negotiation : ReadTimeout")

				c.conn.SetReadDeadline(time.Time{})
				c.conn.SetWriteDeadline(time.Time{})
				c.pl.Unlock()

				if readtimeCount >= consts.MaxAcceptableReadTimeout {
					c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.temporaryRead", "Client Negotiation : Max ReadTimeout count allowed : Closing")
					c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.temporaryRead", "Completed")
					return nil, consts.ErrTimeoutOverReached
				}

				readtimeCount++
				continue
			}

			if c.shouldClose() {
				c.pl.Unlock()
				break
			}

			c.conn.SetReadDeadline(time.Time{})
			c.conn.SetWriteDeadline(time.Time{})
			c.pl.Unlock()

			c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initInfoNegotiation", "Client Negotiation : Initialization Failed : %s", err)
			c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.temporaryRead", "Completed")
			return nil, err
		}

		c.conn.SetReadDeadline(time.Time{})
		c.conn.SetWriteDeadline(time.Time{})
		c.pl.Unlock()

		block = block[:dataLen]
		break
	}

	c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.temporaryRead", "Completed")
	return block, nil
}

// SendAll sends the giving data to all clients and clusters the giving
// set of data.
func (c *Client) SendAll(data []byte, flush bool) error {
	c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.SendAll", "Started : Transmission to All ")

	if err := c.Send(data, flush); err != nil {
		c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.SendAll", "Completed : %+q", err)
		return err
	}

	for _, cu := range c.server.ClientList() {
		if cu == c {
			continue
		}

		if err := cu.Send(data, flush); err != nil {
			c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.SendAll", "Unable to deliver for %+q : %+q", cu.info, err)
		}
	}

	// FIX: Resolves issues with parser blowing up because of internal '\r\n' of data in ClusterDistRequest.
	data = bytes.TrimSuffix(data, []byte("\r\n"))
	data = bytes.TrimPrefix(data, []byte("\r\n"))

	// Create a new data format for sending data over the channel using the exluding
	// '()' character to safeguard the original message.
	realData := byteutils.MakeMessage(string(consts.ClusterDistRequest), fmt.Sprintf("(%+s)", data))

	clusters := c.server.ClusterList()
	c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.SendAll", "Cluster Delivery : %+q : Total %d", realData, len(clusters))

	for _, cu := range clusters {
		c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.SendAll", "Data Delivery : %+q : %+q : %+q", realData, cu.info.UUID, cu.info.Addr)

		if err := cu.Send(realData, flush); err != nil {
			c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.SendAll", "Unable to deliver for %+q : %+q", cu.info, err)
		}
	}

	c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.SendAll", "Completed")
	return nil
}

// ErrDataOversized is delivered when the provide data passes the maximum allowed
// data size.
var ErrDataOversized = errors.New("Data size is to big")

// Send delivers a message into the clients connection stream.
func (c *Client) Send(data []byte, flush bool) error {
	c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.Send", "Started")
	if data == nil {
		c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.Send", "Completed")
		return nil
	}

	// c.sendg.Add(1)
	// defer c.sendg.Done()

	if len(data) > maxPayload {
		c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.Send", "Completed : %s", ErrDataOversized)
		return ErrDataOversized
	}

	if !bytes.HasSuffix(data, consts.CTRLLine) {
		data = append(data, consts.CTRLLine...)
	}

	c.pl.Lock()
	if c.writer != nil && c.conn != nil {
		var deadline bool

		if c.writer.Available() < len(data) {
			c.conn.SetWriteDeadline(time.Now().Add(flushDeline))
			deadline = true
		}

		c.logs.Log(octo.LOGTRANSMISSION, c.info.UUID, "tcp.Client.Send", "Started : %+q", data)

		_, err := c.writer.Write(data)
		if err == nil && flush {
			err = c.writer.Flush()

			if deadline {
				c.conn.SetWriteDeadline(time.Time{})
			}
		}

		c.logs.Log(octo.LOGTRANSMISSION, c.info.UUID, "tcp.Client.Send", "Completed")

		if err != nil {
			c.pl.Unlock()
			c.logs.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.Send", "Completed : %s", ErrDataOversized)
			return err
		}

	}
	c.pl.Unlock()

	c.logs.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.Send", "Completed")
	return nil
}

// Info returns the client and server information.
func (c *Client) Info() (octo.Info, octo.Info) {
	if c.clusterClient {
		return c.info, c.server.clusterInfo
	}

	return c.info, c.server.info
}

//================================================================================

// ServerAttr defines a struct which holds the attributes and config values for a
// tcp.Server.
type ServerAttr struct {
	Addr         string
	ClusterAddr  string
	Authenticate bool
	TLS          *tls.Config
	Credential   octo.AuthCredential // Credential for the server.
}

// Server defines a core structure which manages the intenals
// of the way tcp works.
type Server struct {
	ServerAttr
	logs             octo.Logs
	info             octo.Info
	clusterInfo      octo.Info
	listener         net.Listener
	clusterListener  net.Listener
	clientSystem     octo.System
	clusterSystem    *octo.BaseSystem
	clientBaseSystem *octo.BaseSystem
	wg               sync.WaitGroup
	cg               sync.WaitGroup
	clientLock       sync.Mutex
	clients          []*Client
	clientsInfo      []octo.Info
	clusterLock      sync.Mutex
	clusters         []*Client
	clustersInfo     []octo.Info
	rl               sync.RWMutex
	running          bool
}

// New returns a new Server which handles connections from clients and
// clusters if allowed.
func New(logs octo.Logs, attr ServerAttr) *Server {
	suuid := uuid.NewV4().String()

	ip, port, _ := net.SplitHostPort(attr.Addr)
	if ip == "" || ip == consts.AnyIP {
		if realIP, err := netutils.GetMainIP(); err == nil {
			attr.Addr = net.JoinHostPort(realIP, port)
		}
	}

	cip, cport, _ := net.SplitHostPort(attr.ClusterAddr)
	if cip == "" || cip == consts.AnyIP {
		if realIP, err := netutils.GetMainIP(); err == nil {
			attr.ClusterAddr = net.JoinHostPort(realIP, cport)
		}
	}

	var s Server
	s.ServerAttr = attr
	s.logs = logs
	s.info = octo.Info{
		Remote: attr.Addr,
		Addr:   attr.Addr,
		Local:  attr.Addr,
		UUID:   suuid,
		SUUID:  suuid,
	}

	s.clusterInfo = octo.Info{
		Remote: attr.ClusterAddr,
		Addr:   attr.ClusterAddr,
		UUID:   suuid,
		SUUID:  suuid,
	}

	return &s
}

// CInfo returns the octo.Info related with this server cluster listener.
func (s *Server) CInfo() octo.Info {
	return s.clusterInfo
}

// Info returns the octo.Info related with this server.
func (s *Server) Info() octo.Info {
	return s.info
}

// Wait causes a wait on the server.
func (s *Server) Wait() {
	s.wg.Wait()
}

// Credential returns the Credential related to the giving server.
func (s *Server) Credential() octo.AuthCredential {
	return s.ServerAttr.Credential
}

// IsRunning returns true/false if the giving server is running.
func (s *Server) IsRunning() bool {
	s.rl.RLock()
	defer s.rl.RUnlock()
	return s.running
}

// Close returns the error from closing the listener.
func (s *Server) Close() error {
	s.logs.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.Close", "Started : %#v", s.info)

	if !s.IsRunning() {
		return nil
	}

	s.rl.Lock()
	s.running = false
	s.rl.Unlock()

	if err := s.listener.Close(); err != nil {
		s.logs.Log(octo.LOGERROR, s.info.UUID, "tcp.Server.Close", "Completed : Client Close Error : %+s", err)
	}

	if s.clusterListener != nil {
		if err := s.clusterListener.Close(); err != nil {
			s.logs.Log(octo.LOGERROR, s.info.UUID, "tcp.Server.Close", "Completed : Cluster Close Error : %+s", err)
		}
	}

	s.wg.Wait()

	s.logs.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.Close", "Init : Close Clients")
	s.clientLock.Lock()
	{
		for _, client := range s.clients {
			go client.Close()
		}
	}
	s.clientLock.Unlock()
	s.logs.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.Close", "Finished : Close Clients")

	s.logs.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.Close", "Init : Close Clusters")
	s.clusterLock.Lock()
	{
		for _, cluster := range s.clusters {
			go cluster.Close()
		}
	}
	s.clusterLock.Unlock()
	s.logs.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.Close", "Finished : Close Clusters")

	s.cg.Wait()

	return nil
}

// Listen sets up the listener and begins listening for connection requests from
// the listener.
func (s *Server) Listen(system octo.System) error {
	s.logs.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.Listen", "Started")

	if s.IsRunning() {
		s.logs.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.Listen", "Ended : Already Running : %#v", s.info)
		return nil
	}

	var err error

	s.listener, err = net.Listen("tcp", s.info.Addr)
	s.logs.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.Listen", "New TCP Listener : Client : %#v", s.info)
	if err != nil {
		s.logs.Log(octo.LOGERROR, s.info.UUID, "tcp.Server.Listen", "Failed to start client listener: %s", err.Error())
		return err
	}

	if s.ClusterAddr != "" {
		s.clusterListener, err = net.Listen("tcp", s.ClusterAddr)
		s.logs.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.Listen", "New TCP Listener : Cluster : %#v", s.clusterInfo)
		if err != nil {
			s.logs.Log(octo.LOGERROR, s.info.UUID, "tcp.Server.Listen", "Failed to start cluster listener : %s", err.Error())
			return err
		}
	}

	s.rl.Lock()
	s.running = true
	s.rl.Unlock()

	s.clientSystem = system
	s.clientBaseSystem = octo.NewBaseSystem(system, blockparser.Blocks, s.logs, blocksystem.BaseHandlers())
	s.clusterSystem = octo.NewBaseSystem(system, blockparser.Blocks, s.logs, blocksystem.BaseHandlers(), blocksystem.AuthHandlers(s), blocksystem.ClusterHandlers(s, s, s.TransmitToClients))

	if s.clusterListener != nil {
		s.wg.Add(2)

		go s.handleClientConnections(system)
		go s.handleClusterConnections(s.clusterSystem)
	} else {
		s.wg.Add(1)
		go s.handleClientConnections(system)
	}

	s.logs.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.Listen", "Completed")
	return nil
}

// RelateWithCluster asks the server to connect and initialize a relationship with
// the giving cluster at the giving address.
func (s *Server) RelateWithCluster(addr string) error {
	s.logs.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.RelateWithCluster", "Started")

	ip, port, _ := net.SplitHostPort(addr)
	if ip == "" || ip == consts.AnyIP {
		if realIP, err := netutils.GetMainIP(); err == nil {
			addr = net.JoinHostPort(realIP, port)
		}
	}

	s.rl.Lock()
	if !s.running {
		s.rl.Unlock()
		return nil
	}
	s.rl.Unlock()

	conn, err := net.DialTimeout("tcp", addr, connectDeadline)
	if err != nil {
		s.logs.Log(octo.LOGERROR, s.info.UUID, "tcp.Server.RelateWithCluster", "Completed : %s", err)
		return err
	}

	localAddr := conn.LocalAddr().String()
	remoteAddr := conn.RemoteAddr().String()
	clientID := uuid.NewV4().String()

	s.logs.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.RelateWithCluster", "New Client : Local[%q] : Remote[%q]", localAddr, remoteAddr)

	var client Client
	client = Client{
		info: octo.Info{
			Addr:   localAddr,
			Local:  localAddr,
			Remote: remoteAddr,
			UUID:   clientID,
			SUUID:  s.info.SUUID,
		},
		parser:              blockparser.Blocks,
		server:              s,
		conn:                conn,
		logs:                s.logs,
		clusterClient:       true,
		connectionInitiator: true,
		system:              s.clusterSystem,
		writer:              bufio.NewWriter(conn),
	}

	if err := client.Listen(); err != nil {
		s.logs.Log(octo.LOGERROR, s.info.UUID, "tcp.Server.RelateWithCluster", "New Client Error : %s", err.Error())
		client.conn.Close()
		return err
	}

	var clientIndex int

	s.clusterLock.Lock()
	{
		clientIndex = len(s.clusters)
		s.clusters = append(s.clusters, &client)
	}
	s.clusterLock.Unlock()

	s.generateClusterInfo()

	go func() {
		client.Wait()

		s.clusterLock.Lock()
		{
			s.clusters = append(s.clusters[:clientIndex], s.clusters[clientIndex+1:]...)
		}
		s.clusterLock.Unlock()

		s.generateClusterInfo()
	}()

	s.logs.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.RelateWithCluster", "Completed")
	return nil
}

// ClientList returns the slice of clients.
func (s *Server) ClientList() []*Client {
	s.clientLock.Lock()
	defer s.clientLock.Unlock()
	return s.clients[0:]
}

// ClusterList returns the slice of clusters clients.
func (s *Server) ClusterList() []*Client {
	s.clusterLock.Lock()
	defer s.clusterLock.Unlock()
	return s.clusters[0:]
}

// Clusters returns all Info related to each registered cluster.
func (s *Server) Clusters() []octo.Info {
	s.logs.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.Clusters", "Started")

	var cluster []octo.Info

	s.clusterLock.Lock()
	cluster = s.clustersInfo[0:]
	s.clusterLock.Unlock()

	s.logs.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.Clusters", "Completed")
	return cluster
}

// Clients returns the giving list of clients and clusters.
func (s *Server) Clients() []octo.Info {
	s.logs.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.Clients", "Started")

	var clients []octo.Info

	s.clientLock.Lock()
	clients = s.clientsInfo[0:]
	s.clientLock.Unlock()

	s.logs.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.Clients", "Completed")
	return clients
}

// TransmitToClients transmit the provided data to all clients.
func (s *Server) TransmitToClients(data []byte) error {
	s.logs.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.TransmitToClient", "Started : %+q", data)

	s.clientLock.Lock()
	defer s.clientLock.Unlock()

	for _, client := range s.clients {
		if err := client.Send(data, true); err != nil {
			s.logs.Log(octo.LOGERROR, s.info.UUID, "tcp.Server.TransmitToClient", "Failed to transmit : %#v : %q", client.info, err.Error())
		}
	}

	s.logs.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.Clients", "Completed")
	return nil
}

// generateClientInfo updates the giving info list for the
// clients set.
func (s *Server) generateClientInfo() {
	s.clusterLock.Lock()
	defer s.clusterLock.Unlock()

	var clients []octo.Info

	for _, client := range s.clients {
		clientInfo, _ := client.Info()
		clients = append(clients, clientInfo)
	}

	s.clientsInfo = clients
}

// generateClusterInfo updates the giving info list for the
// clusters set.
func (s *Server) generateClusterInfo() {
	s.clusterLock.Lock()
	defer s.clusterLock.Unlock()

	var clusters []octo.Info

	for _, cluster := range s.clusters {
		clusterInfo, _ := cluster.Info()
		clusters = append(clusters, clusterInfo)
	}

	s.clustersInfo = clusters
}

// hasClusterInfo returns true/false if the giving info exists.
func (s *Server) hasClusterInfo(info octo.Info) bool {
	s.clusterLock.Lock()
	defer s.clusterLock.Unlock()

	for _, cu := range s.clustersInfo {
		if cu.UUID == info.UUID {
			return true
		}
	}

	return false
}

// filteredClusters defines a function to handle connection to clusters.
func (s *Server) filterClusters(infos []octo.Info) []octo.Info {
	var filtered []octo.Info

	for _, info := range infos {
		if s.hasClusterInfo(info) {
			continue
		}

		if info.UUID == s.info.SUUID {
			continue
		}

		if info.Addr == s.ServerAttr.Addr {
			continue
		}

		if info.Addr == s.ServerAttr.ClusterAddr {
			continue
		}

		filtered = append(filtered, info)
	}

	return filtered
}

// HandleClusters handles the connection to provided clusters lists.
func (s *Server) HandleClusters(infos []octo.Info) {
	s.logs.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.HandleClusters", "Started")
	filtered := s.filterClusters(infos)

	s.logs.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.HandleClusters", "Clusters Accepted : %#v", filtered)

	for _, cluster := range filtered {
		if cluster.Remote == "" {
			continue
		}

		go s.RelateWithCluster(cluster.Addr)
	}

	s.logs.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.HandleClusters", "Completed")
}

// handleClientConnections handles the connection from client providers.
func (s *Server) handleClientConnections(system octo.System) {
	s.logs.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.handleClientConnections", "Started")
	defer s.wg.Done()

	sleepTime := minSleepTime

	s.logs.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.handleClientConnections", "Initiating Accept Loop")
	for s.IsRunning() {

		conn, err := s.listener.Accept()
		if err != nil {
			s.logs.Log(octo.LOGERROR, s.info.UUID, "Server.handleClientConnections", "Error : %s", err.Error())
			if opError, ok := err.(*net.OpError); ok {
				if opError.Op == "accept" {
					break
				}
			}

			if tmpError, ok := err.(net.Error); ok && tmpError.Temporary() {
				s.logs.Log(octo.LOGERROR, s.info.UUID, "Server.handleClientConnections", "Temporary Error : %s : Sleeping %dms", err.Error(), sleepTime/time.Millisecond)
				time.Sleep(sleepTime)
				sleepTime *= 2
				if sleepTime > maxSleepTime {
					sleepTime = minSleepTime
				}

				continue
			}
		}

		if tcpConn, ok := conn.(*net.TCPConn); ok {
			tcpConn.SetKeepAlive(true)
			tcpConn.SetKeepAlivePeriod(consts.KeepAlivePeriod)
		}

		s.logs.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.handleClientConnections", "New Client : Intiaiting Client Creation Process.")

		localAddr := conn.LocalAddr().String()
		remoteAddr := conn.RemoteAddr().String()
		clientID := uuid.NewV4().String()

		s.logs.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.handleClientConnections", "New Client : Local[%q] : Remote[%q]", localAddr, remoteAddr)

		var client Client
		client = Client{
			info: octo.Info{
				Addr:   remoteAddr,
				Local:  localAddr,
				Remote: remoteAddr,
				UUID:   clientID,
				SUUID:  s.info.SUUID,
			},
			parser:              blockparser.Blocks,
			server:              s,
			conn:                conn,
			logs:                s.logs,
			system:              system,
			primarySystem:       s.clientBaseSystem,
			clusterClient:       false,
			connectionInitiator: false,
			writer:              bufio.NewWriter(conn),
		}

		if err := client.Listen(); err != nil {
			s.logs.Log(octo.LOGERROR, s.info.UUID, "tcp.Server.handleClientConnections", "New Client Error : %s", err.Error())
			client.conn.Close()
			continue
		}

		var clientIndex int
		var clientAddr string

		s.clientLock.Lock()
		{
			clientIndex = len(s.clients)
			clientAddr = client.info.Addr
			s.clients = append(s.clients, &client)
		}
		s.clientLock.Unlock()

		s.cg.Add(1)
		s.generateClientInfo()

		go func() {
			client.Wait()
			s.logs.Log(octo.LOGERROR, s.info.UUID, "tcp.Server.handleClentConnections", "Closed Client[%d : %q : %q] ", clientIndex, clientAddr, clientID)

			s.cg.Done()

			s.clientLock.Lock()
			{
				s.clients = append(s.clients[:clientIndex], s.clients[clientIndex+1:]...)
			}
			s.clientLock.Unlock()

			s.generateClientInfo()
		}()

		continue

	}

	s.logs.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.handleClientConnections", "Completed")
}

// handles the connection of cluster servers and intializes the needed operations
// and procedures in getting clusters servers initialized.
func (s *Server) handleClusterConnections(system octo.System) {
	s.logs.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.handleClusterConnections", "Started")
	defer s.wg.Done()

	sleepTime := minSleepTime

	for s.IsRunning() {

		conn, err := s.clusterListener.Accept()
		if err != nil {
			s.logs.Log(octo.LOGERROR, s.info.UUID, "Server.handleClusterConnections", "Error : %s", err.Error())
			if opError, ok := err.(*net.OpError); ok {
				if opError.Op == "accept" {
					break
				}
			}

			if tmpError, ok := err.(net.Error); ok && tmpError.Temporary() {
				s.logs.Log(octo.LOGERROR, s.info.UUID, "Server.handleClusterConnections", "Temporary Error : %s : Sleeping %dms", err.Error(), sleepTime/time.Millisecond)
				time.Sleep(sleepTime)
				sleepTime *= 2
				if sleepTime > maxSleepTime {
					sleepTime = minSleepTime
				}

				continue
			}
		}

		if tcpConn, ok := conn.(*net.TCPConn); ok {
			tcpConn.SetKeepAlive(true)
			tcpConn.SetKeepAlivePeriod(consts.KeepAlivePeriod)
		}

		s.logs.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.handleClusterConnections", "New Cluster : Intiaiting Creation Process.")

		localAddr := conn.LocalAddr().String()
		remoteAddr := conn.RemoteAddr().String()
		clientID := uuid.NewV4().String()

		s.logs.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.handleClusterConnections", "New Cluster : Local[%q] : Remote[%q]", localAddr, remoteAddr)

		var client Client
		client = Client{
			parser: blockparser.Blocks,
			info: octo.Info{
				Addr:   remoteAddr,
				Local:  localAddr,
				Remote: remoteAddr,
				UUID:   clientID,
				SUUID:  s.info.SUUID,
			},
			server:              s,
			conn:                conn,
			logs:                s.logs,
			system:              system,
			clusterClient:       true,
			connectionInitiator: false,
			writer:              bufio.NewWriter(conn),
		}

		if err := client.Listen(); err != nil {
			s.logs.Log(octo.LOGERROR, s.info.UUID, "tcp.Server.handleClusterConnections", "New Client Error : %s", err.Error())
			client.conn.Close()
			continue
		}

		var clientIndex int
		var clientAddr string

		s.clusterLock.Lock()
		{
			clientIndex = len(s.clusters)
			clientAddr = client.info.Addr
			s.clusters = append(s.clusters, &client)
		}
		s.clusterLock.Unlock()

		s.cg.Add(1)
		s.generateClusterInfo()

		go func() {
			client.Wait()
			s.logs.Log(octo.LOGERROR, s.info.UUID, "tcp.Server.handleClusterConnections", "Closed Cluster[%d : %q : %q] ", clientIndex, clientAddr, clientID)

			s.cg.Done()

			s.clusterLock.Lock()
			{
				s.clusters = append(s.clusters[:clientIndex], s.clusters[clientIndex+1:]...)
			}
			s.clusterLock.Unlock()

			s.generateClusterInfo()
		}()

		continue
	}

	s.logs.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.handleClusterConnections", "Completed")
}
