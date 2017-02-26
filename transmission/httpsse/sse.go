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
	uuid "github.com/satori/go.uuid"
)

// SSEAttr defines a attribute struct for defining options for the WebSSEServer
// struct.
type SSEAttr struct {
	Addr          string
	Headers       http.Header
	Credential    octo.AuthCredential
	TLSConfig     *tls.Config
	Notifications chan []byte
}

// SSEServer defines a struct implements the http.ServeHTTP interface which
// handles servicing http requests for websockets.
type SSEServer struct {
	Attr     SSEAttr
	log      octo.Logs
	info     octo.Info
	server   *http.Server
	listener net.Listener
	wg       sync.WaitGroup
	rl       sync.Mutex
	running  bool
	doClose  bool
}

// New returns a new instance of a SSEServer.
func New(attr SSEAttr) *SSEServer {
	ip, port, _ := net.SplitHostPort(attr.Addr)
	if ip == "" || ip == consts.AnyIP {
		if realIP, err := netutils.GetMainIP(); err == nil {
			attr.Addr = net.JoinHostPort(realIP, port)
		}
	}

	var suuid = uuid.NewV4().String()

	var ws SSEServer
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
	s.log.Log(octo.LOGINFO, s.info.UUID, "httpsee.SSEServer.Listen", "Started")

	if s.isRunning() {
		s.log.Log(octo.LOGINFO, s.info.UUID, "httpsee.SSEServer.Listen", "Completed")
		return nil
	}

	if s.Attr.Notifications == nil {
		err := errors.New("SSEServer expects you to provide a channel for notifications")
		s.log.Log(octo.LOGERROR, s.info.UUID, "httpsee.SSEServer.Listen", "Initialize net.Listener failed : Error : %s", err.Error())
		return err
	}

	listener, err := netutils.MakeListener("tcp", s.Attr.Addr, s.Attr.TLSConfig)
	if err != nil {
		s.log.Log(octo.LOGERROR, s.info.UUID, "httpsee.SSEServer.Listen", "Initialize net.Listener failed : Error : %s", err.Error())
		return err
	}

	server, tlListener, err := netutils.NewHTTPServer(listener, s, s.Attr.TLSConfig)
	if err != nil {
		s.log.Log(octo.LOGERROR, s.info.UUID, "httpsee.SSEServer.Listen", "Initialize net.Listener failed : Error : %s", err.Error())
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

	s.log.Log(octo.LOGINFO, s.info.UUID, "httpsee.SSEServer.Listen", "Completed")
	return nil
}

// ServeHTTP implements the http.Handler.ServeHTTP interface method to handle http request
// converted to websockets request.
func (s *SSEServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.log.Log(octo.LOGINFO, s.info.UUID, "httpsee.SSEServer.ServeHTTP", "Started")

	if s.shouldClose() {
		s.log.Log(octo.LOGINFO, s.info.UUID, "httpsee.SSEServer.ServeHTTP", "Completed")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
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

	s.log.Log(octo.LOGINFO, s.info.UUID, "httpsee.SSEServer.ServeHTTP", "Init : Message Loop : Client : %+q : %+q", r.RemoteAddr, r.URL.Path)
	closer := w.(http.CloseNotifier).CloseNotify()

	{
	messageLoop:
		for {
			select {
			case <-closer:
				s.log.Log(octo.LOGINFO, s.info.UUID, "httpsee.SSEServer.ServeHTTP", "Closing : Client requests end : Client : %+q : %+q", r.RemoteAddr, r.URL.Path)
				go s.Close()
				break messageLoop

			case data, ok := <-s.Attr.Notifications:
				if !ok {
					s.log.Log(octo.LOGINFO, s.info.UUID, "httpsee.SSEServer.ServeHTTP", "Closing : Notifications Channel requests end : Client : %+q : %+q", r.RemoteAddr, r.URL.Path)
					break messageLoop
				}

				if !raw {
					fmt.Fprintf(w, "%s", data)

					// Flush data immedaitely to client.
					flusher.Flush()
					continue messageLoop
				}

				fmt.Fprintf(w, "data: %s\n\n", data)

				// Flush data immedaitely to client.
				flusher.Flush()
			}
		}
	}

	s.log.Log(octo.LOGINFO, s.info.UUID, "httpsee.SSEServer.ServeHTTP", "Completed")
}

// Close ends the websocket connection and ensures all requests are finished.
func (s *SSEServer) Close() error {
	s.log.Log(octo.LOGINFO, s.info.UUID, "httpsee.SSEServer.Close", "Start")
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
		s.log.Log(octo.LOGERROR, s.info.UUID, "httpsee.SSEServer.Close", "Completed : %+q", err.Error())
	}

	s.log.Log(octo.LOGINFO, s.info.UUID, "httpsee.SSEServer.Close", "Completed")
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
