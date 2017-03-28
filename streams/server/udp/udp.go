// Package udp provides a simple package that implements a udp protocol
// transmission for the octo.TranmissionProtocol interface. Which allows a uniform
// response cycle with a udp based connection either for single/multicast connections.
package udp

import (
	"encoding/json"
	"net"
	"sync"

	"github.com/influx6/faux/context"
	"github.com/influx6/octo"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/messages/jsoni"
	jsoniserver "github.com/influx6/octo/messages/jsoni/server"
	"github.com/influx6/octo/netutils"
	"github.com/influx6/octo/streams/server"
	uuid "github.com/satori/go.uuid"
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

// ServerAttr defines the giving attributes that determines how a UDP server intializes
// and behaves.
type ServerAttr struct {
	Addr               string
	Authenticate       bool
	Version            Version
	Credential         octo.AuthCredential
	MulticastInterface *net.Interface
}

// Server defines a struct for a managing the internals of a UDP server.
type Server struct {
	instruments         octo.Instrumentation
	Attr                ServerAttr
	conn                *net.UDPConn
	ip                  *net.UDPAddr
	info                octo.Contact
	wg                  sync.WaitGroup
	rg                  sync.WaitGroup
	base                *jsoni.SxConversations
	auth                octo.Authenticator
	rl                  sync.Mutex
	running             bool
	doClose             bool
	cl                  sync.Mutex
	clients             []Client
	cwl                 sync.Mutex
	clientAuthenticated map[string]bool
}

// New returns a new instance of the UDP server.
func New(instrument octo.Instrumentation, attr ServerAttr) *Server {
	var s Server
	s.Attr = attr
	s.instruments = instrument
	s.clientAuthenticated = make(map[string]bool)

	ip, port, _ := net.SplitHostPort(attr.Addr)
	if ip == "" || ip == consts.AnyIP {
		if realIP, err := netutils.GetMainIP(); err == nil {
			attr.Addr = net.JoinHostPort(realIP, port)
		}
	}

	suuid := uuid.NewV4().String()
	s.info = octo.Contact{
		Addr:   attr.Addr,
		Remote: attr.Addr,
		UUID:   suuid,
		SUUID:  suuid,
	}

	return &s
}

// Wait causes a wait on the server.
func (s *Server) Wait() {
	s.wg.Wait()
}

// Credential returns the Credential related to the giving server.
func (s *Server) Credential() octo.AuthCredential {
	return s.Attr.Credential
}

// Contact returns the octo.Contact related with this server.
func (s *Server) Contact() octo.Contact {
	return s.info
}

// IsRunning returns true/false if the giving server is running.
func (s *Server) IsRunning() bool {
	s.rl.Lock()
	defer s.rl.Unlock()
	return s.running
}

// stopRunning returns true/false if the giving server should stop.
func (s *Server) stopRunning() bool {
	s.rl.Lock()
	defer s.rl.Unlock()
	return s.doClose
}

// Close returns the error from closing the listener.
func (s *Server) Close() error {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "udp.Server.Close", "Started : %#v", s.info)

	if !s.IsRunning() {
		return nil
	}

	s.rl.Lock()
	{
		s.running = false
		s.doClose = true
	}
	s.rl.Unlock()

	// Await for last request.
	s.rg.Wait()

	if err := s.conn.Close(); err != nil {
		s.instruments.Log(octo.LOGERROR, s.info.UUID, "udp.Server.Close", "Completed : %s", err.Error())
	}

	s.wg.Wait()

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "udp.Server.Close", "Completed")
	return nil
}

// Listen fires up the server and internal operations of the udp server.
func (s *Server) Listen(system server.System) error {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "udp.Server.Listen", "Started : %#v", s.Attr)

	if s.IsRunning() {
		s.instruments.Log(octo.LOGINFO, s.info.UUID, "udp.Server.Listen", "Completed")
		return nil
	}

	var version string

	switch s.Attr.Version {
	case Ver0:
		version = "udp"
	case Ver4:
		version = "udp4"
	case Ver6:
		version = "udp6"
	}

	udpAddr, err := net.ResolveUDPAddr(version, s.Attr.Addr)
	if err != nil {
		s.instruments.Log(octo.LOGERROR, s.info.UUID, "udp.Server.Listen", "Completed : Error : %s", err.Error())
		return err
	}

	s.ip = udpAddr

	var conn *net.UDPConn

	if s.Attr.MulticastInterface != nil {
		conn, err = net.ListenMulticastUDP(version, s.Attr.MulticastInterface, s.ip)
	} else {
		conn, err = net.ListenUDP(version, s.ip)
	}

	if err != nil {
		s.instruments.Log(octo.LOGERROR, s.info.UUID, "udp.Server.Listen", "Completed : Error : %s", err.Error())
		return err
	}

	s.conn = conn
	s.auth = system
	s.base = jsoni.NewSxConversations(system, &jsoniserver.ContactServer{}, &jsoniserver.ConversationServer{}, &jsoniserver.AuthServer{
		Credentials: s,
	})

	// Set the server state as active.
	s.rl.Lock()
	{
		s.running = true
		s.doClose = false
	}
	s.rl.Unlock()

	s.wg.Add(1)

	go s.handleConnections(system)

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "udp.Server.Listen", "Completed")
	return nil
}

// retrieveOrAdd checks if there exists a client with the giving address else
// returns a new Client object for the giving address.
func (s *Server) retrieveOrAdd(addr *net.UDPAddr) *Client {
	s.instruments.Log(octo.LOGDEBUG, s.info.UUID, "udp.Server.retrieveOrAdd", "Started")

	s.cl.Lock()
	defer s.cl.Unlock()

	network := addr.Network()
	addrString := addr.String()

	var index int

	// Look through the client list if we've already encountered such a client,
	// and return it.
	{

		index = len(s.clients)

		for _, client := range s.clients {
			// TODO: Do we want to match by network: `client.addr.Network() == network` ?
			if client.addr.String() == addrString {
				s.instruments.Log(octo.LOGDEBUG, s.info.UUID, "udp.Server.retrieveOrAdd", "Found Existing Client : %q : %q", network, addr.String())
				return copyClient(&client)
			}
		}
	}

	s.instruments.Log(octo.LOGDEBUG, s.info.UUID, "udp.Server.retrieveOrAdd", "Creating New Client : %q : %q", network, addr.String())

	cuuid := uuid.NewV4().String()
	info := octo.Contact{
		Addr:   addr.IP.String(),
		Remote: addr.IP.String(),
		UUID:   cuuid,
		SUUID:  s.info.SUUID,
	}

	cl := Client{
		server:      s,
		info:        info,
		addr:        addr,
		index:       index,
		conn:        s.conn,
		ctx:         context.New(),
		instruments: s.instruments,
	}

	s.clients = append(s.clients, cl)

	s.instruments.Log(octo.LOGDEBUG, s.info.UUID, "udp.Server.retrieveOrAdd", "Completed")
	return &cl
}

// copyClient creates a new copy of the giving client.
func copyClient(c *Client) *Client {
	newClient := new(Client)
	*newClient = *c

	newClient.ctx = context.New()
	newClient.info = c.info
	newClient.addr = netutils.CopyUDPAddr(c.addr)

	return newClient
}

// getClients returns all registered clients of the giving server.
func (s *Server) getClients() []Client {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "udp.Server.getClients", "Started")

	s.cl.Lock()
	defer s.cl.Unlock()

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "udp.Server.getClients", "Completed")
	return s.clients[0:]
}

// handleConnections handles the process of accepting/reading requests from the server
// and passing it to desired clients.
func (s *Server) handleConnections(system server.System) {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "udp.Server.handleConnections", "Started : %#v", s.Attr)

	defer s.wg.Done()

	block := make([]byte, consts.MinDataSize)

	for s.IsRunning() {
		if s.stopRunning() {
			s.instruments.Log(octo.LOGERROR, s.info.UUID, "udp.Server.handleConnections", "Closing")
			break
		}

		n, addr, err := s.conn.ReadFromUDP(block)
		s.instruments.Log(octo.LOGTRANSMITTED, s.info.UUID, "udp.Server.handleConnections", "Received : %+q : %+q", block[:n], addr.String())
		s.instruments.Log(octo.LOGTRANSMITTED, s.info.UUID, "udp.Server.handleConnections", "Completed")

		if err != nil {
			s.instruments.Log(octo.LOGERROR, s.info.UUID, "udp.Server.handleConnections", "Read Error : %+s", err)
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}

			// TODO: Do we wish to break here or continue?
			// break
			continue
		}

		// Retrieve the client with the provided addr and serve the respone in a go
		// routine.
		s.rg.Add(1)

		s.instruments.Log(octo.LOGINFO, s.info.UUID, "udp.Server.handleConnections", "Initiaiting Client Connection : %+q", addr)

		(func(data []byte, tx *Client) {
			s.instruments.Log(octo.LOGINFO, s.info.UUID, "udp.Server.handleConnections", "Client Init : %+q", tx.info)
			defer s.rg.Done()

			if s.Attr.Authenticate {

				// NOTE: We dont need to this here because the Client.authenticate method handles.
				var authenticated bool

				s.cwl.Lock()
				{
					authenticated = s.clientAuthenticated[addr.String()]
				}
				s.cwl.Unlock()

				if !authenticated {
					if err := tx.authenticate(data); err != nil {
						s.instruments.Log(octo.LOGERROR, s.info.UUID, "udp.Server.handleConnections", "UDP Client Auth : Authentication Failed : Error : %+s", err)
						go tx.Close()
						return
					}

					return
				}

			}

			if err := s.base.Serve(data, tx); err != nil {
				s.instruments.Log(octo.LOGERROR, s.info.UUID, "udp.Server.handleConnections", "UDP Base System : Fails Parsing : Error : %+s", err)
				return
			}

		}(block[:n], s.retrieveOrAdd(addr)))

		// TODO: Do we need the expansion algorithmn here?
		if n == len(block) && len(block) < consts.MaxDataWrite {
			block = make([]byte, len(block)*2)
		}

		if n < len(block)/2 && len(block) > consts.MaxDataWrite {
			block = make([]byte, len(block)/2)
		}
	}

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "udp.Server.handleConnections", "Completed")
}

//================================================================================

// Client defines the structure which communicates with other udp connections.
type Client struct {
	instruments   octo.Instrumentation
	conn          *net.UDPConn
	addr          *net.UDPAddr
	server        *Server
	ctx           context.Context
	info          octo.Contact
	authenticated bool
	index         int
}

// authenticate runs the authentication procedure to authenticate that the connection
// was valid.
func (c *Client) authenticate(data []byte) error {
	c.instruments.Log(octo.LOGINFO, c.info.UUID, "udp.Client.authenticate", "Started")

	if !c.server.Attr.Authenticate {
		c.instruments.Log(octo.LOGINFO, c.info.UUID, "udp.Client.authenticate", "Completed")
		return nil
	}

	var cmd jsoni.AuthMessage

	if err := json.Unmarshal(data, &cmd); err != nil {
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "udp.Client.authenticate", "Completed : Error : Unmarshalling failed : %q", err.Error())

		if block, cerr := jsoni.Parser.Encode(jsoni.CommandMessage{
			Name: string(consts.AuthroizationDenied),
			Data: []byte(err.Error()),
		}); cerr == nil {
			c.Send(block, true)
		}

		return nil
	}

	if cmd.Name != string(consts.AuthResponse) {
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "udp.Client.authenticate", "Completed : Error : Not Authorization Response : %q", consts.ErrAuthorizationFailed.Error())

		if block, cerr := jsoni.Parser.Encode(jsoni.CommandMessage{
			Name: string(consts.AuthroizationDenied),
			Data: []byte(consts.ErrAuthorizationFailed.Error()),
		}); cerr == nil {
			c.Send(block, true)
		}

		return consts.ErrAuthorizationFailed
	}

	c.instruments.Log(octo.LOGDEBUG, c.info.UUID, "udp.Client.authenticate", "Auth Command : %+q", cmd)

	if err := c.server.auth.Authenticate(cmd.Data); err != nil {
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "udp.Client.authenticate", "Completed : Error : AuthCredential Authorization Failure : %+q", err.Error())

		if block, cerr := jsoni.Parser.Encode(jsoni.CommandMessage{
			Name: string(consts.AuthroizationDenied),
			Data: []byte(err.Error()),
		}); cerr == nil {
			c.Send(block, true)
		}

		return err
	}

	if block, berr := jsoni.Parser.Encode(jsoni.CommandMessage{Name: string(consts.AuthroizationGranted)}); berr == nil {
		c.instruments.Log(octo.LOGINFO, c.info.UUID, "udp.Client.authenticate", "Completed : AuthCredential Authorization Granted : %q", c.info.Addr)
		c.Send(block, true)
	}

	c.server.cwl.Lock()
	{
		c.server.clientAuthenticated[c.addr.String()] = true
	}
	c.server.cwl.Unlock()

	c.authenticated = true
	c.instruments.Log(octo.LOGINFO, c.info.UUID, "udp.Client.authenticate", "Completed")
	return nil
}

// Send delivers the message to the giving addr associated with the client.
func (c *Client) Send(data []byte, flush bool) error {
	c.instruments.Log(octo.LOGINFO, c.info.UUID, "udp.Client.Send", "Started")

	c.instruments.Log(octo.LOGTRANSMISSION, c.info.UUID, "udp.Client.Send", "Started : %q", string(data))
	_, err := c.conn.WriteToUDP(data, c.addr)
	c.instruments.Log(octo.LOGTRANSMISSION, c.info.UUID, "udp.Client.Send", "Ended")

	if err != nil {
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "udp.Client.Send", "Completed : %s", err.Error())
		return err
	}

	c.instruments.Log(octo.LOGINFO, c.info.UUID, "udp.Client.Send", "Completed")
	return nil
}

// SendAll delivers the message to the giving addr associated with the client.
func (c *Client) SendAll(data []byte, flush bool) error {
	c.instruments.Log(octo.LOGINFO, c.info.UUID, "udp.Client.SendAll", "Started")

	if err := c.Send(data, flush); err != nil {
		c.instruments.Log(octo.LOGERROR, c.info.UUID, "udp.Client.SendAll", "Completed : %s", err.Error())
		return err
	}

	clients := c.server.getClients()

	for _, client := range clients {
		if client.addr.IP.Equal(c.addr.IP) && client.addr.Port == c.addr.Port {
			continue
		}

		if _, err := c.conn.WriteToUDP(data, client.addr); err != nil {
			c.instruments.Log(octo.LOGERROR, c.info.UUID, "udp.Client.SendAll", "Completed : Client{UUID: %s, Addr: %+s} : %s", client.info.UUID, client.addr.String(), err.Error())
		}
	}

	c.instruments.Log(octo.LOGINFO, c.info.UUID, "udp.Client.SendAll", "Completed")
	return nil
}

// Close usually closes the connection patterning to the giving client, but in
// udp the server connection handles the response, so operation happens here.
func (c *Client) Close() error {
	c.instruments.Log(octo.LOGINFO, c.info.UUID, "udp.Client.Close", "Started : %+q", c.info)
	c.server.cl.Lock()
	{
		c.server.clients = append(c.server.clients[:c.index], c.server.clients[c.index+1:]...)
	}
	c.server.cl.Unlock()
	c.instruments.Log(octo.LOGINFO, c.info.UUID, "udp.Client.Close", "Completed")
	return nil
}

// Contact returns the Contact objects of the giving client and server.
func (c *Client) Contact() (octo.Contact, octo.Contact) {
	return c.info, c.server.info
}

// Ctx returns the giving context pertaining to the specific request.
func (c *Client) Ctx() context.Context {
	return c.ctx
}
