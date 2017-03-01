// Package udp provides a simple package that implements a udp protocol
// transmission for the octo.TranmissionProtocol interface. Which allows a uniform
// response cycle with a udp based connection either for single/multicast connections.
package udp

import (
	"bytes"
	"encoding/json"
	"net"
	"sync"

	"github.com/influx6/faux/context"
	"github.com/influx6/octo"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/netutils"
	"github.com/influx6/octo/parsers/byteutils"
	"github.com/influx6/octo/parsers/jsonparser"
	"github.com/influx6/octo/systems/jsonsystem"
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
	log     octo.Logs
	Attr    ServerAttr
	conn    *net.UDPConn
	ip      *net.UDPAddr
	info    octo.Info
	wg      sync.WaitGroup
	rg      sync.WaitGroup
	base    *octo.BaseSystem
	system  octo.System
	rl      sync.Mutex
	running bool
	doClose bool
	cl      sync.Mutex
	clients []Client
}

// New returns a new instance of the UDP server.
func New(log octo.Logs, attr ServerAttr) *Server {
	var s Server
	s.log = log
	s.Attr = attr

	ip, port, _ := net.SplitHostPort(attr.Addr)
	if ip == "" || ip == consts.AnyIP {
		if realIP, err := netutils.GetMainIP(); err == nil {
			attr.Addr = net.JoinHostPort(realIP, port)
		}
	}

	suuid := uuid.NewV4().String()
	s.info = octo.Info{
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

// Info returns the octo.Info related with this server.
func (s *Server) Info() octo.Info {
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
	s.log.Log(octo.LOGINFO, s.info.UUID, "udp.Server.Close", "Started : %#v", s.info)

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
		s.log.Log(octo.LOGERROR, s.info.UUID, "udp.Server.Close", "Completed : %s", err.Error())
	}

	s.wg.Wait()

	s.log.Log(octo.LOGINFO, s.info.UUID, "udp.Server.Close", "Completed")
	return nil
}

// Listen fires up the server and internal operations of the udp server.
func (s *Server) Listen(system octo.System) error {
	s.log.Log(octo.LOGINFO, s.info.UUID, "udp.Server.Listen", "Started : %#v", s.Attr)

	if s.IsRunning() {
		s.log.Log(octo.LOGINFO, s.info.UUID, "udp.Server.Listen", "Completed")
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
		s.log.Log(octo.LOGERROR, s.info.UUID, "udp.Server.Listen", "Completed : Error : %s", err.Error())
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
		s.log.Log(octo.LOGERROR, s.info.UUID, "udp.Server.Listen", "Completed : Error : %s", err.Error())
		return err
	}

	s.conn = conn
	s.system = system
	s.base = octo.NewBaseSystem(system, jsonparser.JSON, s.log, jsonsystem.BaseHandlers(), jsonsystem.AuthHandlers(s, system))

	// Set the server state as active.
	s.rl.Lock()
	{
		s.running = true
		s.doClose = false
	}
	s.rl.Unlock()

	s.wg.Add(1)

	go s.handleConnections(system)

	s.log.Log(octo.LOGINFO, s.info.UUID, "udp.Server.Listen", "Completed")
	return nil
}

// retrieveOrAdd checks if there exists a client with the giving address else
// returns a new Client object for the giving address.
func (s *Server) retrieveOrAdd(addr *net.UDPAddr) *Client {
	s.log.Log(octo.LOGDEBUG, s.info.UUID, "udp.Server.retrieveOrAdd", "Started")

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
			if client.addr.Network() == network && client.addr.String() == addrString {
				return copyClient(&client)
			}
		}
	}

	cuuid := uuid.NewV4().String()
	info := octo.Info{
		Addr:   addr.IP.String(),
		Remote: addr.IP.String(),
		UUID:   cuuid,
		SUUID:  s.info.SUUID,
	}

	cl := Client{
		log:    s.log,
		info:   info,
		addr:   addr,
		conn:   s.conn,
		server: s,
		ctx:    context.New(),
		index:  index,
	}

	s.clients = append(s.clients, cl)

	s.log.Log(octo.LOGDEBUG, s.info.UUID, "udp.Server.retrieveOrAdd", "Completed")
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
	s.log.Log(octo.LOGINFO, s.info.UUID, "udp.Server.getClients", "Started")

	s.cl.Lock()
	defer s.cl.Unlock()

	s.log.Log(octo.LOGINFO, s.info.UUID, "udp.Server.getClients", "Completed")
	return s.clients[0:]
}

// handleConnections handles the process of accepting/reading requests from the server
// and passing it to desired clients.
func (s *Server) handleConnections(system octo.System) {
	s.log.Log(octo.LOGINFO, s.info.UUID, "udp.Server.handleConnections", "Started : %#v", s.Attr)

	defer s.wg.Done()

	block := make([]byte, consts.MinDataSize)

	for s.IsRunning() {
		if s.stopRunning() {
			s.log.Log(octo.LOGERROR, s.info.UUID, "udp.Server.handleConnections", "Closing")
			break
		}

		n, addr, err := s.conn.ReadFromUDP(block)
		s.log.Log(octo.LOGTRANSMITTED, s.info.UUID, "udp.Server.handleConnections", "Received : %+q : %+q", block[:n], addr.String())
		s.log.Log(octo.LOGTRANSMITTED, s.info.UUID, "udp.Server.handleConnections", "Completed")

		if err != nil {
			s.log.Log(octo.LOGERROR, s.info.UUID, "udp.Server.handleConnections", "Read Error : %+s", err)
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

		s.log.Log(octo.LOGINFO, s.info.UUID, "udp.Server.handleConnections", "Initiaiting Client Connection : %+q", addr)

		(func(data []byte, tx *Client) {
			s.log.Log(octo.LOGINFO, s.info.UUID, "udp.Server.handleConnections", "Client Init : %+q", tx.info)
			defer s.rg.Done()

			authenticated := tx.authenticated
			if err := tx.authenticate(data); err != nil {
				s.log.Log(octo.LOGERROR, s.info.UUID, "udp.Server.handleConnections", "UDP Client Auth : Authentication Failed : Error : %+s", err)
				go tx.Close()
				return
			}

			// If its false, then its a new connection that should have been authenticated,
			// so we dont serve the initial request since authentication is on.
			if !authenticated && s.Attr.Authenticate {
				return
			}

			rem, err := s.base.ServeBase(data, tx)
			if err != nil {
				s.log.Log(octo.LOGERROR, s.info.UUID, "udp.Server.handleConnections", "UDP Base System : Fails Parsing : Error : %+s", err)

				if err := s.system.Serve(data, tx); err != nil {
					s.log.Log(octo.LOGERROR, s.info.UUID, "udp.Server.handleConnections", "UDP Base System : Fails Parsing : Error : %+s", err)
					return
				}
			}

			// Handle remaining messages and pass it to user system.
			if rem != nil {
				if err := s.system.Serve(byteutils.JoinMessages(rem...), tx); err != nil {
					s.log.Log(octo.LOGERROR, s.info.UUID, "udp.Server.handleConnections", "UDP Base System : Fails Parsing : Error : %+s", err)
					return
				}
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

	s.log.Log(octo.LOGINFO, s.info.UUID, "udp.Server.handleConnections", "Completed")
}

//================================================================================

// Client defines the structure which communicates with other udp connections.
type Client struct {
	log           octo.Logs
	conn          *net.UDPConn
	addr          *net.UDPAddr
	server        *Server
	ctx           context.Context
	info          octo.Info
	authenticated bool
	index         int
}

// authenticate runs the authentication procedure to authenticate that the connection
// was valid.
func (c *Client) authenticate(data []byte) error {
	c.log.Log(octo.LOGINFO, c.info.UUID, "udp.Client.authenticate", "Started")
	if !c.server.Attr.Authenticate {
		c.log.Log(octo.LOGINFO, c.info.UUID, "udp.Client.authenticate", "Completed")
		return nil
	}

	if c.authenticated {
		c.log.Log(octo.LOGINFO, c.info.UUID, "udp.Client.authenticate", "Completed")
		return nil
	}

	var cmd octo.Command

	if err := json.Unmarshal(data, &cmd); err != nil {
		c.log.Log(octo.LOGERROR, c.info.UUID, "udp.Client.authenticate", "Completed : Error : %q", err.Error())
		return nil
	}

	if !bytes.Equal(cmd.Name, consts.AuthResponse) {
		c.log.Log(octo.LOGERROR, c.info.UUID, "udp.Client.authenticate", "Completed : Error : %q", consts.ErrAuthorizationFailed.Error())
		return consts.ErrAuthorizationFailed
	}

	var auth octo.AuthCredential

	if err := json.Unmarshal(bytes.Join(cmd.Data, []byte("")), &cmd); err != nil {
		c.log.Log(octo.LOGERROR, c.info.UUID, "udp.Client.authenticate", "Completed : Error : %q", err.Error())
		return nil
	}

	if err := c.server.system.Authenticate(auth); err != nil {
		c.log.Log(octo.LOGERROR, c.info.UUID, "udp.Client.authenticate", "Completed : Error : %q", err.Error())
		return nil
	}

	c.authenticated = true
	c.log.Log(octo.LOGINFO, c.info.UUID, "udp.Client.authenticate", "Completed")
	return nil
}

// Send delivers the message to the giving addr associated with the client.
func (c *Client) Send(data []byte, flush bool) error {
	c.log.Log(octo.LOGINFO, c.info.UUID, "udp.Client.Send", "Started")

	c.log.Log(octo.LOGTRANSMISSION, c.info.UUID, "udp.Client.Send", "Started : %+q", data)
	_, err := c.conn.WriteToUDP(data, c.addr)
	c.log.Log(octo.LOGTRANSMISSION, c.info.UUID, "udp.Client.Send", "Ended")

	if err != nil {
		c.log.Log(octo.LOGERROR, c.info.UUID, "udp.Client.Send", "Completed : %s", err.Error())
		return err
	}

	c.log.Log(octo.LOGINFO, c.info.UUID, "udp.Client.Send", "Completed")
	return nil
}

// SendAll delivers the message to the giving addr associated with the client.
func (c *Client) SendAll(data []byte, flush bool) error {
	c.log.Log(octo.LOGINFO, c.info.UUID, "udp.Client.SendAll", "Started")

	if err := c.Send(data, flush); err != nil {
		c.log.Log(octo.LOGERROR, c.info.UUID, "udp.Client.SendAll", "Completed : %s", err.Error())
		return err
	}

	clients := c.server.getClients()

	for _, client := range clients {
		if client.addr.IP.Equal(c.addr.IP) && client.addr.Port == c.addr.Port {
			continue
		}

		if _, err := c.conn.WriteToUDP(data, client.addr); err != nil {
			c.log.Log(octo.LOGERROR, c.info.UUID, "udp.Client.SendAll", "Completed : Client{UUID: %s, Addr: %+s} : %s", client.info.UUID, client.addr.String(), err.Error())
		}
	}

	c.log.Log(octo.LOGINFO, c.info.UUID, "udp.Client.SendAll", "Completed")
	return nil
}

// Close usually closes the connection patterning to the giving client, but in
// udp the server connection handles the response, so operation happens here.
func (c *Client) Close() error {
	c.log.Log(octo.LOGINFO, c.info.UUID, "udp.Client.Close", "Started : %+q", c.info)
	c.server.cl.Lock()
	{
		c.server.clients = append(c.server.clients[:c.index], c.server.clients[c.index+1:]...)
	}
	c.server.cl.Unlock()
	c.log.Log(octo.LOGINFO, c.info.UUID, "udp.Client.Close", "Completed")
	return nil
}

// Info returns the Info objects of the giving client and server.
func (c *Client) Info() (octo.Info, octo.Info) {
	return c.info, c.server.info
}

// Ctx returns the giving context pertaining to the specific request.
func (c *Client) Ctx() context.Context {
	return c.ctx
}
