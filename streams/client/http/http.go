package http

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/influx6/octo"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/netutils"
	"github.com/influx6/octo/streams/client"
	"github.com/influx6/octo/utils"
)

// Attr defines a struct which holds configuration options for the http
// client.
type Attr struct {
	MaxReconnets int
	MaxDrops     int
	Authenticate bool
	Addr         string
	Clusters     []string
	Headers      map[string]string
}

// ClientPod defines a http implementation which connects
// to a provided http endpoint for making requests.
type ClientPod struct {
	attr        Attr
	instruments octo.Instrumentation
	pub         *octo.Pub
	servers     []*srvAddr
	curAddr     *srvAddr
	bm          bytes.Buffer
	wg          sync.WaitGroup
	system      client.SystemServer
	encoding    octo.MessageEncoding
	cnl         sync.Mutex
	doClose     bool
	started     bool
}

// New returns a new instance of the http pod.
func New(insts octo.Instrumentation, attr Attr) (*ClientPod, error) {
	if attr.MaxDrops <= 0 {
		attr.MaxDrops = consts.MaxTotalConnectionFailure
	}

	if attr.MaxReconnets <= 0 {
		attr.MaxReconnets = consts.MaxTotalReconnection
	}

	var pod ClientPod
	pod.attr = attr
	pod.instruments = insts
	pod.pub = octo.NewPub()

	// Prepare all server registering and validate paths.
	if err := pod.prepareServers(); err != nil {
		return nil, err
	}

	return &pod, nil
}

// Listen calls the connection to be create and begins serving requests.
func (w *ClientPod) Listen(sm client.SystemServer, encoding octo.MessageEncoding) error {
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

// Close closes the http connection.
func (w *ClientPod) Close() error {
	if w.isClosed() {
		return consts.ErrClosedConnection
	}

	w.notify(octo.ClosedHandler, nil)

	w.bm.Reset()

	w.cnl.Lock()
	w.doClose = true
	w.started = false
	w.cnl.Unlock()

	return nil
}

// Register registers the handler for a given handler.
func (w *ClientPod) Register(tm octo.StateHandlerType, hmi interface{}) {
	w.pub.Register(tm, hmi)
}

// notify calls the giving callbacks for each different type of state.
func (w *ClientPod) notify(n octo.StateHandlerType, err error) {
	var cm octo.Contact

	if w.curAddr != nil {
		cm = w.curAddr.contact
	}

	w.pub.Notify(n, cm, err)
}

// Send delivers the giving message to the underline http connection.
// HTTP does not supported buffering, hence buffer if required must be done by the
// user and then passed in. The flush bool is not functional.
func (w *ClientPod) Send(data interface{}, _ bool) error {
	if !w.isStarted() {
		return consts.ErrRequestUnsearvable
	}

	// Encode outside of lock to reduce contention.
	dataBytes, err := w.encoding.Encode(data)
	if err != nil {
		return err
	}

	return w.do(bytes.NewBuffer(dataBytes))
}

func (w *ClientPod) do(bu *bytes.Buffer) error {
	var url string

	w.cnl.Lock()
	url = w.curAddr.addr
	w.cnl.Unlock()

	// We will attempt to make the request and deliver it, if successfully, we will
	// clear the buffer if not then we must not attempt to clear it.
	req, err := http.NewRequest("POST", url, bu)
	if err != nil {
		return err
	}

	for key, val := range w.attr.Headers {
		req.Header.Set(key, val)
	}

	cred := w.system.Credential()
	req.Header.Set("Authorization", fmt.Sprintf("%s %s:%s:%s", cred.Scheme, cred.Key, cred.Token, cred.Data))

	var client http.Client
	client.Transport = &http.Transport{MaxIdleConnsPerHost: 5}
	client.Timeout = 20 * time.Second

	res, err := client.Do(req)
	if err != nil {
		// We must attempt to resent the data over and over until we find a server that
		// can handle it or all servers have become unreachable.

		if findErr := w.getNextServer(); findErr == nil {
			return w.do(bu)
		}

		return err
	}

	// Read response body if not empty deliver to system else ignore.
	var response bytes.Buffer
	io.Copy(&response, res.Body)
	res.Body.Close()

	if response.Len() == 0 {
		return nil
	}

	val, err := w.encoding.Decode(response.Bytes())
	if err != nil {
		return err
	}

	return w.system.Serve(val, w)
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
func (w *ClientPod) prepareServers() error {

	// Add the main addr if provided.
	if w.attr.Addr != "" {
		w.attr.Addr = netutils.GetAddr(w.attr.Addr)

		ep, err := url.Parse(w.attr.Addr)
		// fmt.Printf("Preping: %q -> %+q <- %q", w.attr.Addr, ep, err)
		if err != nil {
			return err
		}

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

		ep, err := url.Parse(addr)
		if err != nil {
			return err
		}

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
func (w *ClientPod) getNextServer() error {
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

// isStarted returns true/false if the giving http handler has started and has
// provided the system and encoder for the httppod.
func (w *ClientPod) isStarted() bool {
	w.cnl.Lock()
	if w.started {
		w.cnl.Unlock()
		return true
	}
	w.cnl.Unlock()

	return false
}

// isClosed returns true/false if the connection is closed or expected to be closed.
func (w *ClientPod) isClosed() bool {
	w.cnl.Lock()
	if w.doClose {
		w.cnl.Unlock()
		return true
	}
	w.cnl.Unlock()

	return false
}

// reconnect attempts to retrieve a new server after a failure to connect and then
// begins message passing.
func (w *ClientPod) reconnect() error {
	if w.system == nil {
		return consts.ErrNoSystemProvided
	}

	cred := w.system.Credential()
	if w.attr.Authenticate && cred.Scheme == "" {
		return consts.ErrInvalidCredentialDetail
	}

	var started bool

	w.cnl.Lock()
	started = w.started
	w.cnl.Unlock()

	if started {
		w.notify(octo.DisconnectHandler, nil)
	}

	if err := w.getNextServer(); err != nil {
		return err
	}

	w.cnl.Lock()
	{
		w.curAddr.reconnecting = true
		w.curAddr.connected = false
		w.curAddr.recons++
	}
	w.cnl.Unlock()

	w.cnl.Lock()
	w.started = true
	w.curAddr.connected = true
	w.curAddr.reconnecting = false
	w.cnl.Unlock()

	w.notify(octo.ConnectHandler, nil)

	return nil
}
