// Package tcp provides a simple package that implements a udp protocol
// server. for the octo.TranmissionProtocol interface. Which allows a uniform
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
	"github.com/influx6/octo/messages/commando"
	commandoServer "github.com/influx6/octo/messages/commando/server"
	"github.com/influx6/octo/netutils"
	"github.com/influx6/octo/streams/server"
	uuid "github.com/satori/go.uuid"
)

// Transmission defines a structure which implements the the server.Stream
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

// Contact returns the giving information for the internal client and server.
func (t *Transmission) Contact() (octo.Contact, octo.Contact) {
	return t.client.Contact()
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

// Client defines the tcp struct which implements the server.Stream
// interface for communication between a server.
type Client struct {
	pub                   *server.Pub
	ppMisses              int64
	closer                chan struct{}
	pongs                 chan struct{}
	clusterClient         bool
	connectionInitiator   bool
	instruments           octo.Instrumentation
	info                  octo.Contact
	cinfo                 octo.Contact
	authenticator         octo.Authenticator
	system                *commando.SxConversations
	server                *Server
	wg                    sync.WaitGroup
	sg                    sync.WaitGroup
	buffer                bytes.Buffer
	authCredentials       octo.AuthCredential
	cl                    sync.Mutex
	running               bool
	doClose               bool
	ml                    sync.Mutex
	stopAcceptingRequests bool
	pl                    sync.Mutex
	writer                *bufio.Writer
	conn                  net.Conn
}

// Transmission returns a new server. based on the giving client.
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

// Close ends the client connections and stops reception/server. of
// any messages.
func (c *Client) Close() error {
	c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.Close", "Started")

	if !c.IsRunning() {
		c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.Close", "Completed : Already Closed")
		return nil
	}

	c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.Close", "Waiting for all sent requests to End")

	c.cl.Lock()
	c.running = false
	c.doClose = true
	c.cl.Unlock()

	close(c.closer)

	c.pub.Notify(server.ClosedHandler, c.info, c, nil)
	// c.sg.Wait()

	if err := c.conn.Close(); err != nil {
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.Close", "Completed : %s", err)
	}

	c.wg.Wait()

	c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.Close", "Completed")
	return nil
}

// transformTLS transform the internal client connection into a TLS
// connection.
func (c *Client) transformTLS() error {
	c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.transformTLS", "Started : %#v", c.info)

	conn := c.conn

	tlsConn := tls.Server(conn, c.server.ServerAttr.TLS)
	ttl := secondsToDuration(consts.TLSTimeout * float64(time.Second))

	var tlsPassed bool

	time.AfterFunc(ttl, func() {
		c.instruments.Log(octo.LOGINFO, c.info.UUID, "tpc.Client.transfromTLS", "TLS Handshake Timeout : Status[%s] : Addr[%s]", tlsPassed, conn.RemoteAddr().String())

		// Once the time has elapsed, close the connection and nil out.
		if !tlsPassed {
			tlsConn.SetReadDeadline(time.Time{})
			tlsConn.Close()
		}
	})

	tlsConn.SetReadDeadline(time.Now().Add(ttl))

	if err := tlsConn.Handshake(); err != nil {
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "tpc.Client.transfromTLS", "TLS Handshake Failed : %s : %+s", conn.RemoteAddr().String(), err)
		tlsConn.SetReadDeadline(time.Time{})
		tlsConn.Close()
		return err
	}

	c.conn = tlsConn

	c.instruments.Log(octo.LOGINFO, c.info.UUID, "tpc.Client.transfromTLS", "Completed")
	return nil
}

func secondsToDuration(seconds float64) time.Duration {
	ttl := seconds * float64(time.Second)
	return time.Duration(ttl)
}

// ErrInvalidResponse defines the error returned when a response recieved
// does not match standards.
var ErrInvalidResponse = errors.New("Invalid Response")

// ErrInvalidContactResponse defines the error returned when a response recieved
// does not match info response standards.
var ErrInvalidContactResponse = errors.New("Invalid Response")

// ErrAuthInvalidResponse defines the error returned when a response recieved
// does not match authentication standards and either provides invalid
// authentication data from the connection.
var ErrAuthInvalidResponse = errors.New("Invalid Response")

// Listen calls the client to begin listening for connection requests.
func (c *Client) Listen() error {
	c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.Listen", "Started : Client Listen: %+q", c.info)

	if c.IsRunning() {
		c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.Listen", "Completed")
		return nil
	}

	if c.server.ServerAttr.TLS != nil {
		if err := c.transformTLS(); err != nil {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.Listen", "New Client : Initialization Failed : %s", err)
			return err
		}
	}

	c.cl.Lock()
	{
		c.running = true
	}
	c.cl.Unlock()

	// If we are a clusterClient then attempt to exchange information with new cluster.
	if c.clusterClient {
		if !c.connectionInitiator {
			if err := c.initContactNegotiation(); err != nil {
				c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.Listen", "New Client : Initialization Failed : %s", err)
				return err
			}
		} else {
			if err := c.initSlaveContactNegotiation(); err != nil {
				c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.Listen", "New Client : Initialization Failed : %s", err)
				return err
			}
		}
	}

	c.pub.Notify(server.ConnectHandler, c.info, c, nil)

	// Initialize and request authentication from client if allowed.
	if c.server.ServerAttr.Authenticate && !c.connectionInitiator {
		if err := c.initAuthNegotiation(); err != nil {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.Listen", "New Client : Initialization Failed : %s", err)
			return err
		}
	}

	if c.clusterClient && c.connectionInitiator {
		if err := c.initClusterNegotiation(); err != nil {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.Listen", "New Client : Initialization Failed : %s", err)
			return err
		}
	}

	c.wg.Add(2)
	go c.acceptRequests()
	go c.acceptPingCycle()

	c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.Listen", "Complete")
	return nil
}

// SendAll sends the giving data to all clients and clusters the giving
// set of data.
func (c *Client) SendAll(data []byte, flush bool) error {
	c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.SendAll", "Started : Transmission to All ")

	if err := c.Send(data, flush); err != nil {
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.SendAll", "Completed : %+q", err)
		return err
	}

	for _, cu := range c.server.ClientList() {
		if cu == c {
			continue
		}

		if err := cu.Send(data, flush); err != nil {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.SendAll", "Unable to deliver for %+q : %+q", cu.info, err)
		}
	}

	// FIX: Resolves issues with parser blowing up because of internal '\r\n' of data in ClusterDistRequest.
	data = bytes.TrimSuffix(data, []byte("\r\n"))
	data = bytes.TrimPrefix(data, []byte("\r\n"))

	// Create a new data format for sending data over the channel using the exluding
	// '()' character to safeguard the original message.
	realData := commando.MakeMessage(string(consts.ClusterDistRequest), fmt.Sprintf("(%+s)", data))

	clusters := c.server.ClusterList()
	c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.SendAll", "Cluster Delivery : %+q : Total %d", realData, len(clusters))

	for _, cu := range clusters {
		c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.SendAll", "Data Delivery : %+q : %+q : %+q", realData, cu.info.UUID, cu.info.Addr)

		if err := cu.Send(realData, flush); err != nil {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.SendAll", "Unable to deliver for %+q : %+q", cu.info, err)
		}
	}

	c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.SendAll", "Completed")
	return nil
}

// Send delivers a message into the clients connection stream.
func (c *Client) Send(data []byte, flush bool) error {
	c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.Send", "Started")
	if data == nil {
		c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.Send", "Completed")
		return nil
	}

	// c.sendg.Add(1)
	// defer c.sendg.Done()

	if len(data) > consts.MaxPayload {
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.Send", "Completed : %s", consts.ErrDataOversized)
		return consts.ErrDataOversized
	}

	if !bytes.HasSuffix(data, consts.CTRLLine) {
		data = append(data, consts.CTRLLine...)
	}

	c.pl.Lock()
	if c.writer != nil && c.conn != nil {
		var deadline bool

		// If this is a ping byte then send alone, flush
		if bytes.Equal(data, consts.PINGCTRLByte) {
			if err := c.writer.Flush(); err != nil {
				c.pl.Unlock()
				return err
			}

			_, err := c.writer.Write(data)

			c.pl.Unlock()

			return err
		}

		if c.writer.Available() < len(data) {
			c.conn.SetWriteDeadline(time.Now().Add(consts.FlushDeadline))
			deadline = true
		}

		c.instruments.Log(octo.LOGTRANSMISSION, c.info.UUID, "tcp.Client.Send", "Started : %+q", data)

		_, err := c.writer.Write(data)

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
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.Send", "Completed : %s", err)

			if deadline {
				c.conn.SetWriteDeadline(time.Time{})
			}

			c.pl.Unlock()

			// Is it a Temporary net.Error if so, possible slow consumer here, close
			// connection.
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				go c.Close()
			}

			return err
		}

		if flush {
			if ferr := c.writer.Flush(); ferr != nil {
				c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.Send", "Flush Error : %s", ferr)
			}

			if deadline {
				c.conn.SetWriteDeadline(time.Time{})
			}
		}

		c.instruments.Log(octo.LOGTRANSMISSION, c.info.UUID, "tcp.Client.Send", "Completed")

	}
	c.pl.Unlock()

	c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.Send", "Completed")
	return nil
}

// Contact returns the client and server information.
func (c *Client) Contact() (octo.Contact, octo.Contact) {
	if c.clusterClient {
		return c.info, c.server.clusterContact
	}

	return c.info, c.server.info
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

// acceptPingCycle begins listening for continous ping/pong cycle requests which
// incrementally increases a counter for the total ping/pong set and received.
func (c *Client) acceptPingCycle() {
	c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.acceptPingCycle", "Started")
	defer c.wg.Done()

	c.instruments.NotifyEvent(octo.Event{
		Type:       octo.GoroutineOpened,
		Client:     c.info.UUID,
		Server:     c.info.SUUID,
		LocalAddr:  c.info.Local,
		RemoteAddr: c.info.Remote,
		Data:       octo.NewGoroutineInstrument(),
		Details:    map[string]interface{}{},
	})

	defer c.instruments.NotifyEvent(octo.Event{
		Type:       octo.GoroutineClosed,
		Client:     c.info.UUID,
		Server:     c.info.SUUID,
		LocalAddr:  c.info.Local,
		RemoteAddr: c.info.Remote,
		Data:       octo.NewGoroutineInstrument(),
		Details:    map[string]interface{}{},
	})

	ticker := time.NewTicker(consts.MaxPingInterval)

	{
	cloop:
		for c.IsRunning() {
			select {
			case <-c.closer:
				break cloop
			case <-c.pongs:

				// Register PongEvent.
				go c.instruments.NotifyEvent(octo.Event{
					Type:       octo.PongEvent,
					Client:     c.info.UUID,
					Server:     c.info.SUUID,
					LocalAddr:  c.info.Local,
					RemoteAddr: c.info.Remote,
					Details:    map[string]interface{}{},
				})

				if c.ppMisses > 0 {
					c.ppMisses--
				}
				continue
			case <-ticker.C:

				// Register PingEvent.
				go c.instruments.NotifyEvent(octo.Event{
					Type:       octo.PingEvent,
					Client:     c.info.UUID,
					Server:     c.info.SUUID,
					LocalAddr:  c.info.Local,
					RemoteAddr: c.info.Remote,
					Details:    map[string]interface{}{},
				})

				c.Send(consts.PINGCTRLByte, true)
				continue
			case <-time.After(consts.MaxPingPongWait):
				c.ppMisses++
				continue
			}
		}

	}

	c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.acceptPingCycle", "Completed")
}

// acceptRequests begins listening for messages from the giving connection.
func (c *Client) acceptRequests() {
	c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.acceptRequests", "Started")
	defer c.wg.Done()

	c.instruments.NotifyEvent(octo.Event{
		Type:       octo.GoroutineOpened,
		Client:     c.info.UUID,
		Server:     c.info.SUUID,
		LocalAddr:  c.info.Local,
		RemoteAddr: c.info.Remote,
		Data:       octo.NewGoroutineInstrument(),
		Details:    map[string]interface{}{},
	})

	defer c.instruments.NotifyEvent(octo.Event{
		Type:       octo.GoroutineClosed,
		Client:     c.info.UUID,
		Server:     c.info.SUUID,
		LocalAddr:  c.info.Local,
		RemoteAddr: c.info.Remote,
		Data:       octo.NewGoroutineInstrument(),
		Details:    map[string]interface{}{},
	})

	var eofSeen int
	block := make([]byte, consts.MinDataSize)

	for c.IsRunning() {
		if c.buffer.Len() > 0 {
			if err := c.Send(c.buffer.Bytes(), true); err != nil {
				c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.acceptRequests", "Sending buffer failed : %+s", err)
				continue
			}
		}

		c.conn.SetReadDeadline(time.Now().Add(consts.ReadTimeout))
		c.conn.SetWriteDeadline(time.Now().Add(consts.WriteTimeout))

		n, err := c.conn.Read(block)
		c.instruments.NotifyEvent(octo.Event{
			Type:       octo.DataRead,
			Client:     c.info.UUID,
			Server:     c.info.SUUID,
			LocalAddr:  c.info.Local,
			RemoteAddr: c.info.Remote,
			Data:       octo.NewDataInstrument(block[:n], err),
			Details:    map[string]interface{}{},
		})

		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}

			c.pub.Notify(server.DisconnectHandler, c.info, c, nil)

			// TODO: Do we really want to continue here?
			if err == io.EOF {
				if eofSeen < consts.MaxAcceptableEOF {
					eofSeen++
					continue
				}
			}

			c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.acceptRequests", "Read Error : %+s", err)

			c.conn.SetReadDeadline(time.Time{})
			c.conn.SetWriteDeadline(time.Time{})

			break
		}

		c.instruments.Log(octo.LOGTRANSMITTED, c.info.UUID, "tcp.Client.acceptRequests", "Started : %+s", block[:n])
		c.instruments.Log(octo.LOGTRANSMITTED, c.info.UUID, "tcp.Client.acceptRequests", "Completed")

		if c.shouldClose() {
			block = nil
			continue
		}

		c.conn.SetReadDeadline(time.Time{})
		c.conn.SetWriteDeadline(time.Time{})

		c.instruments.Log(octo.LOGTRANSMITTED, c.info.UUID, "tcp.Client.acceptRequests", "Transmitted : %+q", block[:n])
		if err := c.handleRequest(block[:n], c.Transmission()); err != nil {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.acceptRequests", "Read Error : %+s", err)
			break
		}

		if n == len(block) && len(block) < consts.MaxDataWrite {
			block = make([]byte, len(block)*2)
		}

		if n < len(block)/2 && len(block) > consts.MaxDataWrite {
			block = make([]byte, len(block)/2)
		}

		if c.shouldClose() {
			break
		}
	}

	c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.acceptRequests", "Completed")
}

// handleRequest processes data requests coming in from the client's internal
// connection.
func (c *Client) handleRequest(data []byte, tx server.Stream) error {
	c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.handleRequest", "Started")

	// c.sg.Add(1)
	// defer c.sg.Done()

	if bytes.Equal(data, consts.PONGCTRLByte) {
		return nil
	}

	// Trim Suffix from data.
	if bytes.HasSuffix(data, consts.PONGCTRLByte) {
		data = bytes.TrimSuffix(data, consts.PONGCTRLByte)
	}

	// Trim Prefix from data.
	if bytes.HasPrefix(data, consts.PONGCTRLByte) {
		data = bytes.TrimPrefix(data, consts.PONGCTRLByte)
	}

	if err := c.system.Serve(data, tx); err != nil {
		c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.handleRequest", "Completed : System Serve : Error : %+s", err)
		return err
	}

	c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.handleRequest", "Completed")
	return nil
}

// initClusterNegotiation initiates the negotiation of cluster information.
func (c *Client) initClusterNegotiation() error {
	c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.initClusterNegotiation", "Client Cluster Negotiation")

	c.Send(commando.WrapResponseBlock(consts.ClusterRequest, nil), true)

	block, err := c.temporaryRead()
	if err != nil {
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initClusterNegotiation", "Client Negotiation : Initialization Failed : %s", err)
		return err
	}

	msgs, err := commando.Parser.Decode(block)
	if err != nil {
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initClusterNegotiation", "Client Negotiation : Initialization Failed : %s", err)
		return err
	}

	messages, ok := msgs.([]commando.CommandMessage)
	if !ok {
		err := consts.ErrUnservable
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initClusterNegotiation", "Client Negotiation : Initialization Failed : %s", err)
		return err
	}

	c.instruments.Log(octo.LOGDEBUG, c.info.UUID, "tcp.Client.initClusterNegotiation", "Parsed : %+q", messages)

	if len(messages) < 1 {
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initClusterNegotiation", "Client Negotiation : Initialization Failed : %s : Expected Two Data Packs {INFO}:{CREDENTIALS}", ErrInvalidResponse)
		return ErrInvalidResponse
	}

	if !bytes.Equal([]byte(messages[0].Name), consts.ClusterResponse) {
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initAuthNegotiation", "Client Negotiation : Initialization Failed : %s : Expected Two Data Packs {INFO}:{CREDENTIALS}", ErrAuthInvalidResponse)
		return ErrAuthInvalidResponse
	}

	var infos []octo.Contact

	for _, message := range messages[0].Data {
		var info octo.Contact
		if err := json.Unmarshal(message, &info); err != nil {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initClusterNegotiation", "Client Negotiation : Initialization Failed : %s", err)
			return err
		}

		// If we are matching the same server then skip.
		if info.SUUID == c.info.SUUID {
			continue
		}

		infos = append(infos, info)
	}

	c.Send(commando.MakeByteMessage(consts.ClusterPostOK, nil), true)

	c.server.HandleClusters(infos)

	c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.initClusterNegotiation", "Completed")
	return nil
}

// initSlaveContactNegotiation initializes the client negotation when the giving client
// is not the initiator of the of the cluster connection.
func (c *Client) initSlaveContactNegotiation() error {
	c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.initSlaveContactNegotiation", "Client Negotiation")

	// Block attempts to read the request for cluster information.
	{
		block, err := c.temporaryRead()
		if err != nil {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initSlaveContactNegotiation", "Client Negotiation : Initialization Failed : %s", err)
			return err
		}

		msgs, err := commando.Parser.Decode(block[:len(block)])
		if err != nil {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initSlaveContactNegotiation", "Client Negotiation : Initialization Failed : %s", err)
			return err
		}

		messages, ok := msgs.([]commando.CommandMessage)
		if !ok {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initSlaveContactNegotiation", "Client Negotiation : Initialization Failed : %s", consts.ErrUnservable)
			return consts.ErrUnservable
		}

		c.instruments.Log(octo.LOGDEBUG, c.info.UUID, "tcp.Client.initSlaveContactNegotiation", "Parsed : %+q", messages)

		if len(messages) < 1 {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initSlaveContactNegotiation", "Client Negotiation : Initialization Failed : %s : Expected Atleast One Response", ErrInvalidResponse)
			return ErrInvalidResponse
		}

		if !bytes.Equal([]byte(messages[0].Name), consts.ContactRequest) {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initAuthNegotiation", "Client Negotiation : Initialization Failed : %s : Expected Two Data Packs {INFO}:{CREDENTIALS}", ErrInvalidContactResponse)
			return ErrInvalidContactResponse
		}

		sinfoData, err := json.Marshal(c.server.clusterContact)
		if err != nil {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initSlaveContactNegotiation", "Client Negotiation : Initialization Failed : %s", err)
			return err
		}

		if err := c.Send(commando.MakeByteMessage(consts.ContactResponse, sinfoData), true); err != nil {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initSlaveContactNegotiation", "Started : Client Negotiation : Initialization Failed : %s", err)
			return err
		}
	}

	// Block attempts to validate the OK response for the cluster information delivered.
	{
		block, err := c.temporaryRead()
		if err != nil {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initSlaveContactNegotiation", "Client Negotiation : Initialization Failed : %s", err)
			return err
		}

		msgs, err := commando.Parser.Decode(block)
		if err != nil {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initSlaveContactNegotiation", "Client Negotiation : Initialization Failed : %s", err)
			return err
		}

		messages, ok := msgs.([]commando.CommandMessage)
		if !ok {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initSlaveContactNegotiation", "Client Negotiation : Initialization Failed : %s", consts.ErrUnservable)
			return consts.ErrUnservable
		}

		c.instruments.Log(octo.LOGDEBUG, c.info.UUID, "tcp.Client.initSlaveContactNegotiation", "Parsed : %+q", messages)

		if len(messages) < 1 {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initSlaveContactNegotiation", "Client Negotiation : Initialization Failed : %s : Expected atleast one Response", ErrInvalidResponse)
			return ErrInvalidResponse
		}

		if !bytes.Equal([]byte(messages[0].Name), consts.OK) {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initAuthNegotiation", "Client Negotiation : Initialization Failed : %s : Expected OK", ErrInvalidResponse)
			return ErrInvalidResponse
		}
	}

	c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.initSlaveContactNegotiation", "Completed")

	// Are we required to authenticate first, then run slave authentication procedure first.
	if c.server.Authenticate {
		if err := c.initSlaveAuthNegotiation(); err != nil {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initAuthNegotiation", "Client Negotiation : Initialization Failed : %s : Expected Authentication Success", err)
			return err
		}
	}

	// Now attempt to retreive information from cluster as well.
	return c.initContactNegotiation()
}

// initContactNegotiation intiaites the negotiation of client connections.
func (c *Client) initContactNegotiation() error {
	c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.initContactNegotiation", "Started : Client Negotiation")

	c.Send(commando.WrapResponseBlock(consts.ContactRequest, nil), true)

	block, err := c.temporaryRead()
	if err != nil {
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initContactNegotiation", "Client Negotiation : Initialization Failed : %s", err)
		return err
	}

	msgs, err := commando.Parser.Decode(block)
	if err != nil {
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initContactNegotiation", "Client Negotiation : Initialization Failed : %s", err)
		return err
	}

	messages, ok := msgs.([]commando.CommandMessage)
	if !ok {
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initContactNegotiation", "Client Negotiation : Initialization Failed : %s", consts.ErrUnservable)
		return consts.ErrUnservable
	}

	c.instruments.Log(octo.LOGDEBUG, c.info.UUID, "tcp.Client.initContactNegotiation", "Parsed : %+q", messages)

	if len(messages) < 1 {
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initContactNegotiation", "Client Negotiation : Initialization Failed : %s : Expected Two Data Packs {INFO}:{CREDENTIALS}", ErrInvalidResponse)
		return ErrInvalidResponse
	}

	if !bytes.Equal([]byte(messages[0].Name), consts.ContactResponse) {
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initAuthNegotiation", "Client Negotiation : Initialization Failed : %s : Expected Two Data Packs {INFO}:{CREDENTIALS}", ErrAuthInvalidResponse)
		return ErrAuthInvalidResponse
	}

	var info octo.Contact

	infoData := bytes.Join(messages[0].Data, []byte(""))
	if err := json.Unmarshal(infoData, &info); err != nil {
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initContactNegotiation", "Client Negotiation : Initialization Failed : %s", err)
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

	c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.initContactNegotiation", "New Client Contact : %#v", c.info)

	c.Send(commando.MakeByteMessage(consts.OK, nil), true)

	c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.initContactNegotiation", "Completed")
	return nil
}

// initSlaveAuthNegotiation intiaites the negotiation of client connections.
func (c *Client) initSlaveAuthNegotiation() error {
	c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.initSlaveAuthNegotiation", "Started : Client Negotiation")

	// Block attempts to read the request for cluster information.
	{
		block, err := c.temporaryRead()
		if err != nil {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initSlaveAuthNegotiation", "Client Negotiation : Initialization Failed : %s", err)
			return err
		}

		msgs, err := commando.Parser.Decode(block)
		if err != nil {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initSlaveAuthNegotiation", "Client Negotiation : Initialization Failed : %s", err)
			return err
		}

		messages, ok := msgs.([]commando.CommandMessage)
		if !ok {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initSlaveNegotiation", "Client Negotiation : Initialization Failed : %s", consts.ErrUnservable)
			return consts.ErrUnservable
		}

		c.instruments.Log(octo.LOGDEBUG, c.info.UUID, "tcp.Client.initSlaveAuthNegotiation", "Parsed : %+q", messages)

		if len(messages) < 1 {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initSlaveAuthNegotiation", "Client Negotiation : Initialization Failed : %s : Expected atleast one Response", ErrInvalidResponse)
			return ErrInvalidResponse
		}

		if !bytes.Equal([]byte(messages[0].Name), consts.AuthRequest) {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initAuthNegotiation", "Client Negotiation : Initialization Failed : %s : Expected Two Data Packs {INFO}:{CREDENTIALS}", ErrInvalidContactResponse)
			return ErrAuthInvalidResponse
		}

		sauthData, err := json.Marshal(c.server.Credential())
		if err != nil {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initSlaveAuthNegotiation", "Client Negotiation : Initialization Failed : %s", err)
			return err
		}

		if err := c.Send(commando.MakeByteMessage(consts.AuthResponse, sauthData), true); err != nil {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initSlaveAuthNegotiation", "Started : Client Negotiation : Initialization Failed : %s", err)
			return err
		}
	}

	// Block attempts to validate the OK response for the cluster information delivered.
	{
		block, err := c.temporaryRead()
		if err != nil {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initSlaveAuthNegotiation", "Client Negotiation : Initialization Failed : %s", err)
			return err
		}

		msgs, err := commando.Parser.Decode(block)
		if err != nil {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initSlaveAuthNegotiation", "Client Negotiation : Initialization Failed : %s", err)
			return err
		}

		messages, ok := msgs.([]commando.CommandMessage)
		if !ok {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initSlaveAuthNegotiation", "Client Negotiation : Initialization Failed : %s", consts.ErrUnservable)
			return consts.ErrUnservable
		}

		c.instruments.Log(octo.LOGDEBUG, c.info.UUID, "tcp.Client.initSlaveAuthNegotiation", "Parsed : %+q", messages)

		if len(messages) < 1 {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initSlaveAuthNegotiation", "Client Negotiation : Initialization Failed : %s : Expected atleast One Response", ErrInvalidResponse)
			return ErrInvalidResponse
		}

		if !bytes.Equal([]byte(messages[0].Name), consts.AuthroizationGranted) {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initAuthNegotiation", "Client Negotiation : Initialization Failed : %s : Expected AUTHGRANTED", ErrAuthInvalidResponse)
			return ErrAuthInvalidResponse
		}
	}

	c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.initSlaveAuthNegotiation", "Completed")
	return nil
}

// initAuthNegotiation intiaites the negotiation of client connections.
func (c *Client) initAuthNegotiation() error {
	c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.initAuthNegotiation", "Started")

	c.Send(commando.WrapResponseBlock(consts.AuthRequest, nil), true)

	block, err := c.temporaryRead()
	if err != nil {
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initAuthNegotiation", "Completed : Error : %s ", err)
		c.pub.Notify(server.ErrorHandler, c.info, c, err)
		return err
	}

	msgs, err := commando.Parser.Decode(block)
	if err != nil {
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initAuthNegotiation", "Completed : Error : %s ", err)
		c.pub.Notify(server.ErrorHandler, c.info, c, err)
		return err
	}

	messages, ok := msgs.([]commando.CommandMessage)
	if !ok {
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initAuthNegotiation", "Client Negotiation : Initialization Failed : %s", consts.ErrUnservable)
		return consts.ErrUnservable
	}

	c.instruments.Log(octo.LOGDEBUG, c.info.UUID, "tcp.Client.initAuthNegotiation", "Parsed : %+q", messages)

	if len(messages) < 1 {
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initAuthNegotiation", "Client Negotiation : Initialization Failed : %s : Expected Two Data Packs {INFO}:{CREDENTIALS}", ErrInvalidResponse)
		return ErrInvalidResponse
	}

	if !bytes.Equal([]byte(messages[0].Name), consts.AuthResponse) {
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initAuthNegotiation", "Completed : Error : %s ", ErrAuthInvalidResponse)
		c.pub.Notify(server.ErrorHandler, c.info, c, ErrAuthInvalidResponse)
		return ErrAuthInvalidResponse
	}

	var auth octo.AuthCredential

	authData := bytes.Join(messages[0].Data, []byte(""))
	if err := json.Unmarshal(authData, &auth); err != nil {
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initAuthNegotiation", "Client Negotiation : Initialization Failed : %s", err)
		c.pub.Notify(server.ErrorHandler, c.info, c, err)
		return err
	}

	if err := c.authenticator.Authenticate(auth); err != nil {
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initAuthNegotiation", "Client Negotiation : Authentication Failed : %s", err)
		c.Send(commando.MakeByteMessage(consts.AuthroizationDenied, []byte(err.Error())), true)

		c.pub.Notify(server.ErrorHandler, c.info, c, err)
		return err
	}

	c.Send(commando.MakeByteMessage(consts.AuthroizationGranted, nil), true)

	c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.initAuthNegotiation", "Completed")
	return nil
}

func (c *Client) temporaryRead() ([]byte, error) {
	c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.temporaryRead", "Started")
	block := make([]byte, consts.MinDataSize)

	var readtimeCount = 0

	for c.IsRunning() {
		c.conn.SetReadDeadline(time.Now().Add(consts.ReadTempTimeout))
		c.conn.SetWriteDeadline(time.Now().Add(consts.WriteTempTimeout))

		c.pl.Lock()
		dataLen, err := c.conn.Read(block)
		c.instruments.Log(octo.LOGTRANSMITTED, c.info.UUID, "tcp.Client.temporaryRead", "Started : %+q", block[:dataLen])
		c.instruments.Log(octo.LOGTRANSMITTED, c.info.UUID, "tcp.Client.temporaryRead", "Completed")

		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.temporaryRead", "Client Negotiation : ReadTimeout")

				c.conn.SetReadDeadline(time.Time{})
				c.conn.SetWriteDeadline(time.Time{})
				c.pl.Unlock()

				if readtimeCount >= consts.MaxAcceptableReadTimeout {
					c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.temporaryRead", "Client Negotiation : Max ReadTimeout count allowed : Closing")
					c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.temporaryRead", "Completed")
					return nil, consts.ErrTimeoutOverReached
				}

				c.pub.Notify(server.DisconnectHandler, c.info, c, nil)
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

			c.instruments.Log(octo.LOGERROR, c.info.UUID, "tcp.Client.initContactNegotiation", "Client Negotiation : Initialization Failed : %s", err)
			c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.temporaryRead", "Completed")
			return nil, err
		}

		c.conn.SetReadDeadline(time.Time{})
		c.conn.SetWriteDeadline(time.Time{})
		c.pl.Unlock()

		block = block[:dataLen]
		break
	}

	c.instruments.Log(octo.LOGINFO, c.info.UUID, "tcp.Client.temporaryRead", "Completed")
	return block, nil
}

//================================================================================

// ServerAttr defines a struct which holds the attributes and config values for a
// tcp.Server.
type ServerAttr struct {
	Addr         string
	ClusterAddr  string
	Authenticate bool
	Clusters     []string
	TLS          *tls.Config
	Credential   octo.AuthCredential // Credential for the server.
}

// Server defines a core structure which manages the intenals
// of the way tcp works.
type Server struct {
	ServerAttr
	pub                    *server.Pub
	instruments            octo.Instrumentation
	info                   octo.Contact
	clusterContact         octo.Contact
	listener               net.Listener
	clusterListener        net.Listener
	clientSystem           server.System
	clusterSystem          *commando.SxConversations
	clientBaseSystem       *commando.SxConversations
	wg                     sync.WaitGroup
	cg                     sync.WaitGroup
	clientLock             sync.Mutex
	clients                []*Client
	clientsContact         []octo.Contact
	clusterLock            sync.Mutex
	clusters               []*Client
	clusterConnectFailures map[string]int
	clustersContact        []octo.Contact
	rl                     sync.RWMutex
	running                bool
	doClose                bool
}

// New returns a new Server which handles connections from clients and
// clusters if allowed.
func New(instruments octo.Instrumentation, attr ServerAttr) *Server {
	suuid := uuid.NewV4().String()

	ip, port, _ := net.SplitHostPort(attr.Addr)
	if ip == "" || ip == consts.AnyIP {
		if realIP, err := netutils.GetMainIP(); err == nil {
			attr.Addr = net.JoinHostPort(realIP, port)
		}
	}

	if attr.ClusterAddr != "" {
		cip, cport, _ := net.SplitHostPort(attr.ClusterAddr)
		if cip == "" || cip == consts.AnyIP {
			if realIP, err := netutils.GetMainIP(); err == nil {
				attr.ClusterAddr = net.JoinHostPort(realIP, cport)
			}
		}
	}

	var s Server
	s.pub = server.NewPub()
	s.ServerAttr = attr
	s.clusterConnectFailures = make(map[string]int)
	s.instruments = instruments
	s.info = octo.Contact{
		Remote: attr.Addr,
		Addr:   attr.Addr,
		Local:  attr.Addr,
		UUID:   suuid,
		SUUID:  suuid,
	}

	s.clusterContact = octo.Contact{
		Remote: attr.ClusterAddr,
		Addr:   attr.ClusterAddr,
		UUID:   suuid,
		SUUID:  suuid,
	}

	return &s
}

// Register registers the handler for a given handler.
func (s *Server) Register(tm server.StateHandlerType, hmi interface{}) {
	s.pub.Register(tm, hmi)
}

// CLContact returns the octo.Contact related with this server cluster listener.
func (s *Server) CLContact() octo.Contact {
	return s.clusterContact
}

// Contact returns the octo.Contact related with this server.
func (s *Server) Contact() octo.Contact {
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

// IsRunning returns true/false if the giving server is running.
func (s *Server) shouldClose() bool {
	s.rl.RLock()
	defer s.rl.RUnlock()
	return s.doClose
}

// Close returns the error from closing the listener.
func (s *Server) Close() error {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.Close", "Started : %#v", s.info)

	if !s.IsRunning() {
		return nil
	}

	s.rl.Lock()
	s.doClose = false
	s.rl.Unlock()

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.Close", "Init : Close Clients")
	s.clientLock.Lock()
	{
		for _, client := range s.clients {
			go client.Close()
		}
	}
	s.clientLock.Unlock()
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.Close", "Completed : Close Clients")

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.Close", "Init : Close Clusters")
	s.clusterLock.Lock()
	{
		for _, cluster := range s.clusters {
			go cluster.Close()
		}
	}
	s.clusterLock.Unlock()
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.Close", "Completed : Close Clusters")

	s.rl.Lock()
	s.running = false
	s.rl.Unlock()

	if err := s.listener.Close(); err != nil {
		s.instruments.Log(octo.LOGERROR, s.info.UUID, "tcp.Server.Close", "Completed : Client Close Error : %+s", err)
	}

	if s.clusterListener != nil {
		if err := s.clusterListener.Close(); err != nil {
			s.instruments.Log(octo.LOGERROR, s.info.UUID, "tcp.Server.Close", "Completed : Cluster Close Error : %+s", err)
		}
	}

	s.wg.Wait()

	return nil
}

// Listen sets up the listener and begins listening for connection requests from
// the listener.
func (s *Server) Listen(system server.System) error {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.Listen", "Started")

	if s.IsRunning() {
		s.instruments.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.Listen", "Ended : Already Running : %#v", s.info)
		return nil
	}

	var err error

	s.listener, err = net.Listen("tcp", s.info.Addr)
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.Listen", "New TCP Listener : Client : %#v", s.info)
	if err != nil {
		s.instruments.Log(octo.LOGERROR, s.info.UUID, "tcp.Server.Listen", "Failed to start client listener: %s", err.Error())
		return err
	}

	if s.ClusterAddr != "" {
		s.clusterListener, err = net.Listen("tcp", s.ClusterAddr)
		s.instruments.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.Listen", "New TCP Listener : Cluster : %#v", s.clusterContact)
		if err != nil {
			s.instruments.Log(octo.LOGERROR, s.info.UUID, "tcp.Server.Listen", "Failed to start cluster listener : %s", err.Error())
			return err
		}
	}

	s.rl.Lock()
	s.running = true
	s.doClose = false
	s.rl.Unlock()

	s.clientSystem = system
	s.clientBaseSystem = commando.NewSxConversations(system, commandoServer.CloseServer{}, commandoServer.ContactServer{}, commandoServer.ConversationServer{})
	s.clusterSystem = commando.NewSxConversations(system, commandoServer.CloseServer{}, commandoServer.ContactServer{}, commandoServer.ConversationServer{}, &commandoServer.AuthServer{Credentials: s}, &commandoServer.ClusterServer{
		ClusterHandler:         s,
		ClientsMessageDelivery: s,
		Clusters:               s,
	})

	// s.clusterSystem = server.NewBaseSystem(system, blockparser.Blocks, s.instruments, blocksystem.BaseHandlers(), blocksystem.AuthHandlers(s), blocksystem.ClusterHandlers(s, s, s.TransmitToClients))

	if s.clusterListener != nil {
		s.wg.Add(2)

		go s.handleClientConnections()
		go s.handleClusterConnections()
	} else {
		s.wg.Add(1)
		go s.handleClientConnections()
	}

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.Listen", "Completed")
	return nil
}

// RelateWithCluster asks the server to connect and initialize a relationship with
// the giving cluster at the giving address.
func (s *Server) RelateWithCluster(addr string) error {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.RelateWithCluster", "Started")

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

	conn, err := net.DialTimeout("tcp", addr, consts.ConnectDeadline)
	if err != nil {
		s.instruments.Log(octo.LOGERROR, s.info.UUID, "tcp.Server.RelateWithCluster", "Completed : %s", err)
		return err
	}

	localAddr := conn.LocalAddr().String()
	remoteAddr := conn.RemoteAddr().String()
	clientID := uuid.NewV4().String()

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.RelateWithCluster", "New Client : Local[%q] : Remote[%q]", localAddr, remoteAddr)

	var client Client
	client = Client{
		pub: s.pub,
		info: octo.Contact{
			Addr:   localAddr,
			Local:  localAddr,
			Remote: remoteAddr,
			UUID:   clientID,
			SUUID:  s.info.SUUID,
		},
		server:              s,
		conn:                conn,
		closer:              make(chan struct{}),
		pongs:               make(chan struct{}),
		instruments:         s.instruments,
		clusterClient:       true,
		connectionInitiator: true,
		authenticator:       s.clientSystem,
		system:              s.clusterSystem,
		writer:              bufio.NewWriter(conn),
	}

	if err := client.Listen(); err != nil {
		s.instruments.Log(octo.LOGERROR, s.info.UUID, "tcp.Server.RelateWithCluster", "New Client Error : %s", err.Error())
		s.pub.Notify(server.ClosedHandler, client.info, &client, err)
		client.conn.Close()

		// Record failure to connect client.
		s.clusterLock.Lock()
		{
			if misses, ok := s.clusterConnectFailures[addr]; ok {
				misses++
				s.clusterConnectFailures[addr] = misses
			}
		}
		s.clusterLock.Unlock()

		return err
	}

	var clientIndex int

	s.clusterLock.Lock()
	{

		// Add client into the giving list
		clientIndex = len(s.clusters)
		s.clusters = append(s.clusters, &client)

		// Since we connected successfully, reduce the failure count else register it.
		if misses, ok := s.clusterConnectFailures[addr]; ok {
			misses--
			s.clusterConnectFailures[addr] = misses
		} else {
			s.clusterConnectFailures[addr] = 0
		}

	}
	s.clusterLock.Unlock()

	s.generateClusterContact()

	s.cg.Add(1)
	go func() {
		var attemptAgain bool

		client.Wait()
		s.cg.Done()

		s.clusterLock.Lock()
		{
			s.clusters = append(s.clusters[:clientIndex], s.clusters[clientIndex+1:]...)

			if misses, ok := s.clusterConnectFailures[addr]; ok {
				if misses > consts.MaxTotalConnectionFailure {
					s.clusterLock.Unlock()
					return
				}

				attemptAgain = true

				misses++
				s.clusterConnectFailures[addr] = misses
			}

		}
		s.clusterLock.Unlock()

		// Sort out cluster's lists properly.
		s.generateClusterContact()

		if attemptAgain {
			s.RelateWithCluster(addr)
		}
	}()

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.RelateWithCluster", "Completed")
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

// Clusters returns all Contact related to each registered cluster.
func (s *Server) Clusters() []octo.Contact {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.Clusters", "Started")

	var cluster []octo.Contact

	s.clusterLock.Lock()
	cluster = s.clustersContact[0:]
	s.clusterLock.Unlock()

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.Clusters", "Completed")
	return cluster
}

// Clients returns the giving list of clients and clusters.
func (s *Server) Clients() []octo.Contact {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.Clients", "Started")

	var clients []octo.Contact

	s.clientLock.Lock()
	clients = s.clientsContact[0:]
	s.clientLock.Unlock()

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.Clients", "Completed")
	return clients
}

// DeliverToClients transmit the provided data to all clients.
func (s *Server) DeliverToClients(data []byte) error {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.TransmitToClient", "Started : %+q", data)

	s.clientLock.Lock()
	defer s.clientLock.Unlock()

	for _, client := range s.clients {
		if err := client.Send(data, true); err != nil {
			s.instruments.Log(octo.LOGERROR, s.info.UUID, "tcp.Server.TransmitToClient", "Failed to transmit : %#v : %q", client.info, err.Error())
		}
	}

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.Clients", "Completed")
	return nil
}

// generateClientContact updates the giving info list for the
// clients set.
func (s *Server) generateClientContact() {
	s.clusterLock.Lock()
	defer s.clusterLock.Unlock()

	var clients []octo.Contact

	for _, client := range s.clients {
		clientContact, _ := client.Contact()
		clients = append(clients, clientContact)
	}

	s.clientsContact = clients
}

// generateClusterContact updates the giving info list for the
// clusters set.
func (s *Server) generateClusterContact() {
	s.clusterLock.Lock()
	defer s.clusterLock.Unlock()

	var clusters []octo.Contact

	for _, cluster := range s.clusters {
		clusterContact, _ := cluster.Contact()
		clusters = append(clusters, clusterContact)
	}

	s.clustersContact = clusters
}

// hasClusterContact returns true/false if the giving info exists.
func (s *Server) hasClusterContact(info octo.Contact) bool {
	s.clusterLock.Lock()
	defer s.clusterLock.Unlock()

	for _, cu := range s.clustersContact {
		if cu.UUID == info.UUID {
			return true
		}
	}

	return false
}

// filteredClusters defines a function to handle connection to clusters.
func (s *Server) filterClusters(infos []octo.Contact) []octo.Contact {
	var filtered []octo.Contact

	for _, info := range infos {
		if s.hasClusterContact(info) {
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
func (s *Server) HandleClusters(infos []octo.Contact) {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.HandleClusters", "Started")
	filtered := s.filterClusters(infos)

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.HandleClusters", "Clusters Accepted : %#v", filtered)

	for _, cluster := range filtered {
		if cluster.Remote == "" {
			continue
		}

		go s.RelateWithCluster(cluster.Addr)
	}

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.HandleClusters", "Completed")
}

// handleClientConnections handles the connection from client providers.
func (s *Server) handleClientConnections() {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.handleClientConnections", "Started")
	defer s.wg.Done()

	sleepTime := consts.MinSleepTime

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.handleClientConnections", "Initiating Accept Loop")
	for s.IsRunning() {

		conn, err := s.listener.Accept()
		if err != nil {
			s.instruments.Log(octo.LOGERROR, s.info.UUID, "Server.handleClientConnections", "Error : %s", err.Error())
			if opError, ok := err.(*net.OpError); ok {
				if opError.Op == "accept" {
					break
				}
			}

			if tmpError, ok := err.(net.Error); ok && tmpError.Temporary() {
				s.instruments.Log(octo.LOGERROR, s.info.UUID, "Server.handleClientConnections", "Temporary Error : %s : Sleeping %dms", err.Error(), sleepTime/time.Millisecond)
				time.Sleep(sleepTime)
				sleepTime *= 2
				if sleepTime > consts.MaxSleepTime {
					sleepTime = consts.MinSleepTime
				}

				continue
			}
		}

		// Close all connections coming in now, since we want to stop
		// running.
		if s.shouldClose() {
			conn.Close()
			continue
		}

		if tcpConn, ok := conn.(*net.TCPConn); ok {
			tcpConn.SetKeepAlive(true)
			tcpConn.SetKeepAlivePeriod(consts.KeepAlivePeriod)
		}

		s.instruments.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.handleClientConnections", "New Client : Intiaiting Client Creation Process.")

		localAddr := conn.LocalAddr().String()
		remoteAddr := conn.RemoteAddr().String()
		clientID := uuid.NewV4().String()

		s.instruments.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.handleClientConnections", "New Client : Local[%q] : Remote[%q]", localAddr, remoteAddr)

		var client Client
		client = Client{
			pub: s.pub,
			info: octo.Contact{
				Addr:   remoteAddr,
				Local:  localAddr,
				Remote: remoteAddr,
				UUID:   clientID,
				SUUID:  s.info.SUUID,
			},
			server:              s,
			conn:                conn,
			closer:              make(chan struct{}),
			pongs:               make(chan struct{}),
			instruments:         s.instruments,
			system:              s.clientBaseSystem,
			authenticator:       s.clientSystem,
			clusterClient:       false,
			connectionInitiator: false,
			writer:              bufio.NewWriter(conn),
		}

		if err := client.Listen(); err != nil {
			s.instruments.Log(octo.LOGERROR, s.info.UUID, "tcp.Server.handleClientConnections", "New Client Error : %s", err.Error())
			s.pub.Notify(server.ClosedHandler, client.info, &client, err)
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
		s.generateClientContact()

		go func() {
			client.Wait()
			s.instruments.Log(octo.LOGERROR, s.info.UUID, "tcp.Server.handleClentConnections", "Closed Client[%d : %q : %q] ", clientIndex, clientAddr, clientID)

			s.cg.Done()

			s.clientLock.Lock()
			{
				s.clients = append(s.clients[:clientIndex], s.clients[clientIndex+1:]...)
			}
			s.clientLock.Unlock()

			s.generateClientContact()
		}()

		continue

	}

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.handleClientConnections", "Waiting for Close")
	s.cg.Wait()
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.handleClientConnections", "Completed")
}

// handles the connection of cluster servers and intializes the needed operations
// and procedures in getting clusters servers initialized.
func (s *Server) handleClusterConnections() {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.handleClusterConnections", "Started")
	defer s.wg.Done()

	s.instruments.NotifyEvent(octo.Event{
		Type:       octo.GoroutineOpened,
		Client:     s.clusterContact.UUID,
		Server:     s.clusterContact.SUUID,
		LocalAddr:  s.clusterContact.Local,
		RemoteAddr: s.clusterContact.Remote,
		Data:       octo.NewGoroutineInstrument(),
		Details: map[string]interface{}{
			"Addr": s.clusterContact.Addr,
		},
	})

	defer s.instruments.NotifyEvent(octo.Event{
		Type:       octo.GoroutineClosed,
		Client:     s.clusterContact.UUID,
		Server:     s.clusterContact.SUUID,
		LocalAddr:  s.clusterContact.Local,
		RemoteAddr: s.clusterContact.Remote,
		Data:       octo.NewGoroutineInstrument(),
		Details: map[string]interface{}{
			"Addr": s.clusterContact.Addr,
		},
	})

	sleepTime := consts.MinSleepTime

	// Setup the calls to immediately connect to clusters from provided clusters list
	// if not empty and start after 1 second of clustring called.
	if s.ServerAttr.Clusters != nil {
		go func() {
			<-time.After(consts.WaitTimeBeforeClustering)
			for _, cluster := range s.ServerAttr.Clusters {
				if err := s.RelateWithCluster(cluster); err != nil {
					s.instruments.Log(octo.LOGERROR, s.info.UUID, "tcp.Server.handleClusterConnections", "Failed Connecting to Cluster : %q : %+q", cluster, err)

					s.instruments.NotifyEvent(octo.Event{
						Type:       octo.ClusterConnection,
						Client:     s.clusterContact.UUID,
						Server:     s.clusterContact.SUUID,
						LocalAddr:  s.clusterContact.Local,
						RemoteAddr: s.clusterContact.Remote,
						Data:       octo.NewConnectionInstrument(cluster, s.clusterContact.Addr),
						Details: map[string]interface{}{
							"Addr": cluster,
						},
					})

					continue
				}
			}
		}()

	}

	for s.IsRunning() {

		conn, err := s.clusterListener.Accept()
		if err != nil {
			s.instruments.Log(octo.LOGERROR, s.info.UUID, "Server.handleClusterConnections", "Error : %s", err.Error())
			if opError, ok := err.(*net.OpError); ok {
				if opError.Op == "accept" {
					break
				}
			}

			if tmpError, ok := err.(net.Error); ok && tmpError.Temporary() {
				s.instruments.Log(octo.LOGERROR, s.info.UUID, "Server.handleClusterConnections", "Temporary Error : %s : Sleeping %dms", err.Error(), sleepTime/time.Millisecond)
				time.Sleep(sleepTime)
				sleepTime *= 2
				if sleepTime > consts.MaxSleepTime {
					sleepTime = consts.MinSleepTime
				}

				continue
			}
		}

		// Close all connections coming in now, since we want to stop
		// running.
		if s.shouldClose() {
			conn.Close()
			continue
		}

		if tcpConn, ok := conn.(*net.TCPConn); ok {
			tcpConn.SetKeepAlive(true)
			tcpConn.SetKeepAlivePeriod(consts.KeepAlivePeriod)
		}

		s.instruments.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.handleClusterConnections", "New Cluster : Intiaiting Creation Process.")

		localAddr := conn.LocalAddr().String()
		remoteAddr := conn.RemoteAddr().String()
		clientID := uuid.NewV4().String()

		s.instruments.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.handleClusterConnections", "New Cluster : Local[%q] : Remote[%q]", localAddr, remoteAddr)

		var client Client
		client = Client{
			pub: s.pub,
			info: octo.Contact{
				Addr:   remoteAddr,
				Local:  localAddr,
				Remote: remoteAddr,
				UUID:   clientID,
				SUUID:  s.info.SUUID,
			},
			server:              s,
			conn:                conn,
			instruments:         s.instruments,
			authenticator:       s.clientSystem,
			closer:              make(chan struct{}),
			pongs:               make(chan struct{}),
			system:              s.clusterSystem,
			clusterClient:       true,
			connectionInitiator: false,
			writer:              bufio.NewWriter(conn),
		}

		if err := client.Listen(); err != nil {
			s.instruments.Log(octo.LOGERROR, s.info.UUID, "tcp.Server.handleClusterConnections", "New Client Error : %s", err.Error())
			s.pub.Notify(server.ClosedHandler, client.info, &client, err)
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
		s.generateClusterContact()

		go func() {
			client.Wait()
			s.instruments.Log(octo.LOGERROR, s.info.UUID, "tcp.Server.handleClusterConnections", "Closed Cluster[%d : %q : %q] ", clientIndex, clientAddr, clientID)

			s.cg.Done()

			s.clusterLock.Lock()
			{
				if len(s.clusters) == 0 {
					s.clusters = nil
				} else {
					s.clusters = append(s.clusters[:clientIndex], s.clusters[clientIndex+1:]...)
				}
			}
			s.clusterLock.Unlock()

			s.generateClusterContact()
		}()

		continue
	}

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.handleClusterConnections", "Waiting for Close")
	s.cg.Wait()
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "tcp.Server.handleClusterConnections", "Completed")
}

//================================================================================

// pingPongConversationServer defines a new struct which implements the several message
// handling for the commando message types.
// type pingPongConversationServer struct {
// 	c *Client
// }
//
// // Serve handles the response requested by the giving commando.CommandMessage returning
// // then needed response.
// func (c pingPongConversationServer) Serve(cmd commando.CommandMessage, tx server.Stream) error {
// 	switch cmd.Name {
// 	case string(consts.PONG):
// 		return tx.Send(commando.WrapResponseBlock(consts.PING, nil), true)
//
// 	case string(consts.PING):
// 		return tx.Send(commando.WrapResponseBlock(consts.PONG, nil), true)
//
// 	default:
// 		return consts.ErrUnservable
// 	}
// }
//
// // CanServe returns true/false if the giving element is able to server the
// // provided message.Command.
// func (c pingPongConversationServer) CanServe(cmd commando.CommandMessage) bool {
// 	switch cmd.Name {
// 	case string(consts.PONG):
// 		return true
// 	case string(consts.PING):
// 		return true
// 	default:
// 		return false
// 	}
// }
