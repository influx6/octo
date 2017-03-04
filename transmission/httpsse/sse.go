// Package httpsse provides a server which implements server-sent-events easily and
// unlike other packages in octo, provides a package for delivery data and not in a
// request-response form. This is provided to allow leveraging the ability to start
// a system which provides notifications for whatever use as desired.
package httpsse

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/influx6/octo"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/netutils"
	"github.com/influx6/octo/utils"
	uuid "github.com/satori/go.uuid"
)

// SSEAttr defines a attribute struct for defining options for the WebSSEServer
// struct.
type SSEAttr struct {
	Addr          string
	Headers       http.Header
	Auth          octo.Authenticator
	TLSConfig     *tls.Config
	Notifications chan []byte
}

// SSEServer defines a struct implements the http.ServeHTTP interface which
// handles servicing http requests for websockets.
type SSEServer struct {
	Attr        SSEAttr
	instruments octo.Instrumentation
	info        octo.Info
	server      *http.Server
	listener    net.Listener
	mux         *SSEServerMux
	wg          sync.WaitGroup
	rl          sync.Mutex
	running     bool
	doClose     bool
	closer      chan struct{}
}

// New returns a new instance of a SSEServer.
func New(instrument octo.Instrumentation, attr SSEAttr) *SSEServer {
	ip, port, _ := net.SplitHostPort(attr.Addr)
	if ip == "" || ip == consts.AnyIP {
		if realIP, err := netutils.GetMainIP(); err == nil {
			attr.Addr = net.JoinHostPort(realIP, port)
		}
	}

	var suuid = uuid.NewV4().String()

	var ws SSEServer
	ws.closer = make(chan struct{})
	ws.instruments = instrument
	ws.info = octo.Info{
		SUUID:  suuid,
		UUID:   suuid,
		Addr:   attr.Addr,
		Remote: attr.Addr,
	}

	return &ws
}

// Listen begins the initialization of the websocket server.
func (s *SSEServer) Listen() error {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpsee.SSEServer.Listen", "Started")

	if s.isRunning() {
		s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpsee.SSEServer.Listen", "Completed")
		return nil
	}

	if s.Attr.Notifications == nil {
		err := errors.New("SSEServer expects you to provide a channel for notifications")
		s.instruments.Log(octo.LOGERROR, s.info.UUID, "httpsee.SSEServer.Listen", "Initialize net.Listener failed : Error : %s", err.Error())
		return err
	}

	listener, err := netutils.MakeListener("tcp", s.Attr.Addr, s.Attr.TLSConfig)
	if err != nil {
		s.instruments.Log(octo.LOGERROR, s.info.UUID, "httpsee.SSEServer.Listen", "Initialize net.Listener failed : Error : %s", err.Error())
		return err
	}

	server, tlListener, err := netutils.NewHTTPServer(listener, s, s.Attr.TLSConfig)
	if err != nil {
		s.instruments.Log(octo.LOGERROR, s.info.UUID, "httpsee.SSEServer.Listen", "Initialize net.Listener failed : Error : %s", err.Error())
		return err
	}

	s.server = server
	s.listener = tlListener
	s.mux = NewSSEServerMux(s.instruments, s.Attr.Auth, s.info, s.Attr.Notifications, s.closer)

	s.rl.Lock()
	{
		s.running = true
		s.wg.Add(1)
	}
	s.rl.Unlock()

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpsee.SSEServer.Listen", "Completed")
	return nil
}

// Close ends the websocket connection and ensures all requests are finished.
func (s *SSEServer) Close() error {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpsee.SSEServer.Close", "Start")
	if !s.isRunning() {
		return nil
	}

	close(s.closer)

	s.rl.Lock()
	{
		s.running = false
		s.doClose = true
		s.wg.Done()
	}
	s.rl.Unlock()

	if err := s.server.Close(); err != nil {
		s.instruments.Log(octo.LOGERROR, s.info.UUID, "httpsee.SSEServer.Close", "Completed : %+q", err.Error())
	}

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpsee.SSEServer.Close", "Completed")
	return nil
}

// Wait awaits the closure of the giving client.
func (s *SSEServer) Wait() {
	s.wg.Wait()
}

// shouldClose returns true/false if the client should close.
func (s *SSEServer) shouldClose() bool {
	s.rl.Lock()
	defer s.rl.Unlock()
	return s.doClose
}

// isRunning returns true/false if the client is still running.
func (s *SSEServer) isRunning() bool {
	s.rl.Lock()
	defer s.rl.Unlock()
	return s.running
}

// ServeHTTP implements the http.Handler.ServeHTTP interface method to handle http request
// converted to websockets request.
func (s *SSEServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpsee.SSEServer.ServeHTTP", "Started")

	if s.shouldClose() {
		s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpsee.SSEServer.ServeHTTP", "Completed")
		return
	}

	s.mux.ServeHTTP(w, r)

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpsee.SSEServer.ServeHTTP", "Completed")
}

//================================================================================

// SSEServerMux defines a struct which implements the http.Handler interface for
// handling http server-sent requests.
type SSEServerMux struct {
	info          octo.Info
	notifications chan []byte
	closer        chan struct{}
	auth          octo.Authenticator
	instruments   octo.Instrumentation
}

// NewSSEServerMux returns a new instance of a SSEServer.
func NewSSEServerMux(instruments octo.Instrumentation, auth octo.Authenticator, info octo.Info, notifications chan []byte, closer chan struct{}) *SSEServerMux {
	return &SSEServerMux{
		auth:          auth,
		notifications: notifications,
		closer:        closer,
		info:          info,
		instruments:   instruments,
	}
}

// authenticate runs the authentication procedure to authenticate that the connection
// was valid.
func (s *SSEServerMux) authenticate(request *http.Request) error {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpbasic.SSEServerMux.authenticate", "Started")
	if s.auth == nil {
		s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpbasic.SSEServerMux.authenticate", "Completed : No Authentication Required")
		return nil
	}

	authorizationHeader := request.Header.Get("Authorization")
	if len(authorizationHeader) == 0 {
		return errors.New("'Authorization' header needed for authentication")
	}

	credentials, err := utils.ParseAuthorization(authorizationHeader)
	if err != nil {
		s.instruments.Log(octo.LOGERROR, s.info.UUID, "httpbasic.SSEServerMux.authenticate", "Completed : Error : %+q", err)
		return err
	}

	if err := s.auth.Authenticate(credentials); err != nil {
		s.instruments.Log(octo.LOGERROR, s.info.UUID, "httpbasic.SSEServerMux.authenticate", "Completed : Error : %+q", err)
		return err
	}

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpbasic.SSEServerMux.authenticate", "Completed")
	return nil
}

// ServeHTTP implements the http.Handler.ServeHTTP interface method to handle http request
// converted to websockets request.
func (s *SSEServerMux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpsee.SSEServerMux.ServeHTTP", "Started")

	s.instruments.RecordConnectionOp(octo.ConnectionConnet, s.info.UUID, r.RemoteAddr, s.info.Addr, map[string]interface{}{
		"pkg": "github.com/influx6/octo/transmission/httpsse",
	})

	defer s.instruments.RecordConnectionOp(octo.ConnectionDisconnect, s.info.UUID, r.RemoteAddr, s.info.Addr, map[string]interface{}{
		"pkg": "github.com/influx6/octo/transmission/httpsse",
	})

	defer s.instruments.RecordGoroutineOp(octo.GoroutineClosed, s.info.UUID, map[string]interface{}{
		"from": r.RemoteAddr,
		"pkg":  "github.com/influx6/octo/transmission/httpsse",
	})

	if err := s.authenticate(r); err != nil {
		s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpsee.WeaveServer.ServeHTTP", "Completed : Error : Failed to autnethicate : %+q", err)
		http.Error(w, "Http Streaming not supported", http.StatusInternalServerError)
		return
	}

	if err := s.authenticate(r); err != nil {
		s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpsee.SSEServerMux.ServeHTTP", "Completed : Error : Failed to autnethicate : %+q", err)
		http.Error(w, "Http Streaming not supported", http.StatusInternalServerError)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpsee.SSEServerMux.ServeHTTP", "Completed : Error : No Support for http.Flusher")
		http.Error(w, "Http Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Set the headers related to event streaming.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Parse the form and validate if we are expected to send the received
	// data as raw data or not.
	r.ParseForm()
	raw := len(r.Form["raw"]) > 0

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpsee.SSEServerMux.ServeHTTP", "Init : Message Loop : Client : %+q : %+q", r.RemoteAddr, r.URL.Path)
	closer := w.(http.CloseNotifier).CloseNotify()

	{
	messageLoop:
		for {
			select {
			case <-s.closer:
				s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpsee.SSEServerMux.ServeHTTP", "Closing : Server requests end : Client : %+q : %+q", r.RemoteAddr, r.URL.Path)
				break messageLoop
			case <-closer:
				s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpsee.SSEServerMux.ServeHTTP", "Closing : Client requests end : Client : %+q : %+q", r.RemoteAddr, r.URL.Path)
				break messageLoop

			case data, ok := <-s.notifications:
				if !ok {
					s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpsee.SSEServerMux.ServeHTTP", "Closing : Notifications Channel requests end : Client : %+q : %+q", r.RemoteAddr, r.URL.Path)
					break messageLoop
				}

				if !raw {
					s.instruments.RecordDataOp(octo.DataWrite, s.info.UUID, nil, data, map[string]interface{}{
						"to":  r.RemoteAddr,
						"pkg": "github.com/influx6/octo/transmission/httpsse",
					})

					fmt.Fprintf(w, "%s", data)

					// Flush data immedaitely to client.
					flusher.Flush()
					continue messageLoop
				}

				s.instruments.RecordDataOp(octo.DataWrite, s.info.UUID, nil, []byte(fmt.Sprintf("data: %+q\n\n", data)), map[string]interface{}{
					"to":  r.RemoteAddr,
					"pkg": "github.com/influx6/octo/transmission/httpsse",
				})

				fmt.Fprintf(w, "data: %s\n\n", data)

				// Flush data immedaitely to client.
				flusher.Flush()
			}
		}
	}

	s.instruments.Log(octo.LOGINFO, s.info.UUID, "httpsee.SSEServerMux.ServeHTTP", "Completed")
}
