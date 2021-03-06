package wsclient_test

import (
	"testing"

	"github.com/influx6/faux/tests"
	"github.com/influx6/octo"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/instruments"
	"github.com/influx6/octo/messages/jsoni"
	"github.com/influx6/octo/mock"
	"github.com/influx6/octo/netutils"
	"github.com/influx6/octo/streams/client/wsclient"
	"github.com/influx6/octo/streams/server/websocket"
)

// TestClientConnectionWithAuth validates the behave of the tcp client for
// connecting to tcp servers.
func TestClientConnectionWithAuth(t *testing.T) {
	addr := netutils.GetAddr(":7050")
	wsAddr := "ws://" + addr

	clientSystem := mock.NewClientSystem(octo.AuthCredential{
		Scheme: "XBot",
		Key:    "api-32",
		Token:  "auth-4531",
		Data:   "BOMTx",
	})

	system := mock.NewServerSystem(octo.AuthCredential{
		Scheme: "XBot",
		Key:    "api-32",
		Token:  "auth-4531",
		Data:   "BOMTx",
	})

	inst := instruments.Instruments(mock.NewTestLogger(), nil)

	server := websocket.New(inst, websocket.SocketAttr{
		Authenticate: true,
		Addr:         addr,
	})

	if err := server.Listen(system); err != nil {
		tests.Failed("Should have successfully created conenction for websocket server: %+q.", err)
	}
	tests.Passed("Should have successfully created conenction for websocket server.")

	defer server.Close()

	client, err := wsclient.New(inst, wsclient.Attr{
		Addr:         wsAddr,
		Authenticate: true,
		MaxDrops:     6,
		MaxReconnets: 5,
		Headers: map[string]string{
			"X-App":         "Octo-App",
			"Authorization": "XBot api-32:auth-4531:BOMTx",
		},
	})

	if err != nil {
		tests.Failed("Should have successfully connected to websocket server: %+q.", err)
	}
	tests.Passed("Should have successfully connected to websocket server.")

	defer client.Close()

	if err := client.Listen(clientSystem, jsoni.Parser); err != nil {
		tests.Failed("Should have successfully connected to websocket server with client: %+q.", err)
	}
	tests.Passed("Should have successfully connected to websocket server with client.")

	err = client.Send(jsoni.CommandMessage{Name: string(consts.ContactRequest)}, true)
	if err != nil {
		tests.Failed("Should have successfully delivered command to server: %+q.", err)
	}
	tests.Passed("Should have successfully delivered command to server.")

	clientSystem.Wait()
}

// TestClientConnectionWithoutAuth validates the behave of the tcp client for
// connecting to tcp servers.
func TestClientConnectionWithoutAuth(t *testing.T) {
	addr := netutils.GetAddr(":6050")
	wsAddr := "ws://" + addr

	clientSystem := mock.NewClientSystem(octo.AuthCredential{
		Scheme: "XBot",
		Key:    "api-32",
		Token:  "auth-4531",
		Data:   "BOMTx",
	})

	system := mock.NewServerSystem(octo.AuthCredential{
		Scheme: "XBot",
		Key:    "api-32",
		Token:  "auth-4531",
		Data:   "BOMTx",
	})

	inst := instruments.Instruments(mock.NewTestLogger(), nil)

	server := websocket.New(inst, websocket.SocketAttr{
		Authenticate: true,
		Addr:         addr,
	})

	if err := server.Listen(system); err != nil {
		tests.Failed("Should have successfully created conenction for websocket server: %+q.", err)
	}
	tests.Passed("Should have successfully created conenction for websocket server.")

	defer server.Close()

	client, err := wsclient.New(inst, wsclient.Attr{
		Addr:         wsAddr,
		Authenticate: false,
		MaxDrops:     6,
		MaxReconnets: 5,
		Headers: map[string]string{
			"X-App": "Octo-App",
			// "Authorization": "XBot api-32:auth-4531:BOMTx",
		},
	})

	if err != nil {
		tests.Failed("Should have successfully connected to websocket server: %+q.", err)
	}
	tests.Passed("Should have successfully connected to websocket server.")

	defer client.Close()

	if err := client.Listen(clientSystem, jsoni.Parser); err != nil {
		tests.Failed("Should have successfully failed connected to websocket server with client: %+q.", err)
	}
	tests.Passed("Should have successfully failed connected to websocket socket with client.")
}
