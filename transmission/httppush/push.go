// Package httppush provides a server which implements server push system provided
// by the new go http package.
package httppush

import (
	"crypto/tls"
	"net"
	"net/http"
	"sync"

	"github.com/influx6/faux/context"
	"github.com/influx6/octo"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/netutils"
	uuid "github.com/satori/go.uuid"
)

// PushAttr defines a attribute struct for defining options for the WebPushServer
// struct.
type PushAttr struct {
	Addr          string
	Headers       http.Header
	Credential    octo.AuthCredential
	TLSConfig     *tls.Config
	Notifications chan []byte
}

// PushServer defines a struct implements the http.ServeHTTP interface which
// handles servicing http requests for websockets.
type PushServer struct {
	Attr     PushAttr
	log      octo.Logs
	info     octo.Info
	server   *http.Server
	listener net.Listener
	wg       sync.WaitGroup
	rl       sync.Mutex
	running  bool
	doClose  bool
}

// New returns a new instance of a PushServer.
func New(attr PushAttr) *PushServer {
	ip, port, _ := net.SplitHostPort(attr.Addr)
	if ip == "" || ip == consts.AnyIP {
		if realIP, err := netutils.GetMainIP(); err == nil {
			attr.Addr = net.JoinHostPort(realIP, port)
		}
	}

	var suuid = uuid.NewV4().String()

	var ws PushServer
	ws.info = octo.Info{
		SUUID:  suuid,
		UUID:   suuid,
		Addr:   attr.Addr,
		Remote: attr.Addr,
	}

	return &ws
}

// Listen begins the initialization of the websocket server.
func (s *PushServer) Listen() error {
	s.log.Log(octo.LOGINFO, s.info.UUID, "httppush.PushServer.Listen", "Started")

	if s.isRunning() {
		s.log.Log(octo.LOGINFO, s.info.UUID, "httppush.PushServer.Listen", "Completed")
		return nil
	}

	listener, err := netutils.MakeListener("tcp", s.Attr.Addr, s.Attr.TLSConfig)
	if err != nil {
		s.log.Log(octo.LOGERROR, s.info.UUID, "httppush.PushServer.Listen", "Initialize net.Listener failed : Error : %s", err.Error())
		return err
	}

	server, tlListener, err := netutils.NewHTTPServer(listener, s, s.Attr.TLSConfig)
	if err != nil {
		s.log.Log(octo.LOGERROR, s.info.UUID, "httppush.PushServer.Listen", "Initialize net.Listener failed : Error : %s", err.Error())
		return err
	}

	s.rl.Lock()
	{
		s.running = true
		s.server = server
		s.listener = tlListener
		s.wg.Add(1)
	}
	s.rl.Unlock()

	s.log.Log(octo.LOGINFO, s.info.UUID, "httppush.PushServer.Listen", "Completed")
	return nil
}

// ServeHTTP implements the http.Handler.ServeHTTP interface method to handle http request
// converted to websockets request.
func (s *PushServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.log.Log(octo.LOGINFO, s.info.UUID, "httppush.PushServer.ServeHTTP", "Started")

	if s.shouldClose() {
		s.log.Log(octo.LOGINFO, s.info.UUID, "httppush.PushServer.ServeHTTP", "Completed")
		return
	}

	s.log.Log(octo.LOGINFO, s.info.UUID, "httppush.PushServer.ServeHTTP", "Completed")
}

// Close ends the websocket connection and ensures all requests are finished.
func (s *PushServer) Close() error {
	s.log.Log(octo.LOGINFO, s.info.UUID, "httppush.PushServer.Close", "Start")
	if !s.isRunning() {
		return nil
	}

	s.rl.Lock()
	{
		s.running = false
		s.doClose = true
		s.wg.Done()
	}
	s.rl.Unlock()

	if err := s.server.Close(); err != nil {
		s.log.Log(octo.LOGERROR, s.info.UUID, "httppush.PushServer.Close", "Completed : %+q", err.Error())
	}

	s.log.Log(octo.LOGINFO, s.info.UUID, "httppush.PushServer.Close", "Completed")
	return nil
}

// Wait awaits the closure of the giving client.
func (s *PushServer) Wait() {
	s.wg.Wait()
}

// shouldClose returns true/false if the client should close.
func (s *PushServer) shouldClose() bool {
	s.rl.Lock()
	defer s.rl.Unlock()
	return s.doClose
}

// isRunning returns true/false if the client is still running.
func (s *PushServer) isRunning() bool {
	s.rl.Lock()
	defer s.rl.Unlock()
	return s.running
}

//================================================================================

// PushTransformer defines a struct to hold the request and response object.
type PushTransformer struct {
	Request *http.Request
	Writer  http.ResponseWriter
	ctx     context.Context
}
