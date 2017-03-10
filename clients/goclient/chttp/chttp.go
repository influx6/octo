package chttp

import (
	"bytes"
	"net/url"
	"sync"

	"github.com/influx6/octo"
	"github.com/influx6/octo/clients/goclient"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/netutils"
	"github.com/influx6/octo/utils"
)

// Attr defines a struct which holds configuration options for the websocket
// client.
type Attr struct {
	MaxReconnets int
	MaxDrops     int
	Authenticate bool
	Addr         string
	Clusters     []string
	Headers      map[string]string
}

// HTTPPod defines a websocket implementation which connects
// to a provided websocket endpoint for making requests.
type HTTPPod struct {
	attr        Attr
	instruments octo.Instrumentation
	pub         *octo.Pub
	servers     []*srvAddr
	curAddr     *srvAddr
	bm          bytes.Buffer
	wg          sync.WaitGroup
	system      goclient.System
	encoding    goclient.MessageEncoding
	cnl         sync.Mutex
	doClose     bool
	started     bool
}

// New returns a new instance of the websocket pod.
func New(insts octo.Instrumentation, attr Attr) (*HTTPPod, error) {
	if attr.MaxDrops <= 0 {
		attr.MaxDrops = consts.MaxTotalConnectionFailure
	}

	if attr.MaxReconnets <= 0 {
		attr.MaxReconnets = consts.MaxTotalReconnection
	}

	var pod HTTPPod
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
func (w *HTTPPod) Listen(sm goclient.System, encoding goclient.MessageEncoding) error {
	w.cnl.Lock()
	if w.started {
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

// Close closes the websocket connection.
func (w *HTTPPod) Close() error {
	if w.isClose() {
		return consts.ErrClosedConnection
	}

	w.notify(octo.ClosedHandler, nil)

	w.cnl.Lock()
	w.doClose = true
	w.started = false
	w.cnl.Unlock()

	return nil
}

// Register registers the handler for a given handler.
func (w *HTTPPod) Register(tm octo.StateHandlerType, hmi interface{}) {
	w.pub.Register(tm, hmi)
}

// notify calls the giving callbacks for each different type of state.
func (w *HTTPPod) notify(n octo.StateHandlerType, err error) {
	var cm octo.Contact

	if w.curAddr != nil {
		cm = w.curAddr.contact
	}

	w.pub.Notify(n, cm, err)
}

// Send delivers the giving message to the underline websocket connection.
func (w *HTTPPod) Send(data interface{}, flush bool) error {

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

	_ = dataBytes
	return nil
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
func (w *HTTPPod) prepareServers() error {

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
func (w *HTTPPod) getNextServer() error {
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

// reconnect attempts to retrieve a new server after a failure to connect and then
// begins message passing.
func (w *HTTPPod) reconnect() error {
	if w.attr.Authenticate && w.attr.Headers != nil {
		if _, ok := w.attr.Headers["Authorization"]; !ok {
			return consts.ErrNoAuthorizationHeader
		}
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

// shouldClose returns true/false if the giving connection should close.
func (w *HTTPPod) shouldClose() bool {
	w.cnl.Lock()
	if w.doClose {
		w.cnl.Unlock()
		return true
	}
	w.cnl.Unlock()

	return false
}

// isClose returns true/false if the connection is already closed.
func (w *HTTPPod) isClose() bool {
	w.cnl.Lock()
	{
		if !w.started {
			w.cnl.Unlock()
			return true
		}
	}
	w.cnl.Unlock()

	return false
}
