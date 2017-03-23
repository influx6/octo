package websocket_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	gwebsocket "github.com/gorilla/websocket"
	"github.com/influx6/faux/tests"
	"github.com/influx6/octo"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/instruments"
	"github.com/influx6/octo/mock"
	"github.com/influx6/octo/netutils"
	"github.com/influx6/octo/transmission"
	"github.com/influx6/octo/transmission/websocket"
	"github.com/influx6/octo/utils"
)

var (
	scheme    = "XBot"
	key       = "api-32"
	token     = "auth-4531"
	tokenData = []byte("BOMTx")
)

type mockSystem struct {
	t *testing.T
}

// Authenticate authenticates the provided credentials and implements
// the octo.Authenticator interface.
func (mockSystem) Authenticate(cred octo.AuthCredential) error {
	if cred.Scheme != scheme {
		return errors.New("Scheme does not match")
	}

	if cred.Key != key {
		return errors.New("Key does not match")
	}

	if cred.Token != token {
		return errors.New("Token does not match")
	}

	if !bytes.Equal(cred.Data, tokenData) {
		return errors.New("Data  does not match")
	}

	return nil
}

// Serve handles the processing of different requests coming from the outside.
func (mockSystem) Serve(message []byte, tx transmission.Stream) error {
	var command octo.Command

	if err := json.Unmarshal(message, &command); err != nil {
		return err
	}

	switch {
	case bytes.Equal([]byte("OK"), command.Name):
		return nil
	case bytes.Equal([]byte("PUMP"), command.Name):
		return tx.Send([]byte("RUMP"), true)
	case bytes.Equal([]byte("REX"), command.Name):
		return tx.Send([]byte("DEX"), true)
	}

	return errors.New("Invalid Command")
}

// TestWebsocketServer validates the behaviour of the websocket that it matches
// the octo standards.
func TestWebsocketServer(t *testing.T) {
	system := &mockSystem{t: t}

	ws := websocket.New(instruments.Instrument(instruments.InstrumentAttr{Log: mock.NewLogger()}), websocket.SocketAttr{
		Addr:         ":4050",
		Authenticate: true,
		Headers: map[string]string{
			"X-App-Server": "octo-websocket",
		},
	})

	if err := ws.Listen(system); err != nil {
		tests.Failed("Should have successfully connected to host server ':4050': %s.", err)
	}

	uri, _ := url.Parse("ws://" + netutils.GetAddr(":4050"))

	t.Logf("\tWhen we make a initial websocket request without Authorization header to %q", uri.String())
	{
		conn, _, err := newWebsocketClient(uri.String(), map[string]string{
			"X-App": "Octo-App",
		})

		if err != nil {
			conn.Close()
			tests.Failed("Should have successfully failed to connect to websocket server without auhorization: %+q", err)
		}
		tests.Passed("Should have successfully failed to connect to websocket server without auhorization.")
	}

	t.Logf("\tWhen we make a initial websocket request with Authorization header to %q", uri.String())
	{
		conn, _, err := newWebsocketClient(uri.String(), map[string]string{
			"X-App":         "Octo-App",
			"Authorization": "XBot api-32:auth-4531:BOMTx",
		})

		if err != nil {
			tests.Failed("Should have successfully connected to websocket server with auhorization: %q", err.Error())
		}

		defer conn.Close()

		tests.Passed("Should have successfully connected to websocket server with auhorization")
	}

	t.Logf("\tWhen we make a initial websocket request for 'INFO' details to %q", uri.String())
	{
		conn, _, err := newWebsocketClient(uri.String(), map[string]string{
			"X-App":         "Octo-App",
			"Authorization": "XBot api-32:auth-4531:BOMTx",
		})

		if err != nil {
			tests.Failed("Should have successfully connected to websocket server with auhorization: %q", err.Error())
		}
		tests.Passed("Should have successfully connected to websocket server with auhorization")

		defer conn.Close()

		if err := sendMessage(conn, string(consts.ContactRequest)); err != nil {
			tests.Failed("Should have successfully delivered 'INFO' messages: %q.", err.Error())
		}
		tests.Passed("Should have successfully delivered 'INFO' messages.")

		var cmd octo.Command
		if err := conn.ReadJSON(&cmd); err != nil {
			tests.Failed("Should have successfully connected to read messages: %q.", err.Error())
		}
		tests.Passed("Should have successfully connected to read messages.")

		if !bytes.Equal(cmd.Name, consts.ContactResponse) {
			tests.Failed("Should have successfully received 'INFORES' response: %+q.", cmd.Name)
		}
		tests.Passed("Should have successfully received 'INFORES' response: %+q.", cmd.Name)

		if err := conn.WriteControl(gwebsocket.CloseMessage, nil, time.Now().Add(2*time.Second)); err != nil {
			tests.Failed("Should have successfully delivered 'CLOSE' messages: %q.", err.Error())
		}
		tests.Passed("Should have successfully delivered 'CLOSE' messages.")
	}
}

// TestWebsocketSystem validates the behaviour of the websocket that it matches
// the octo standards.
func TestWebsocketSystem(t *testing.T) {
	pocket := mock.NewCredentialPocket(octo.AuthCredential{
		Scheme: scheme,
		Key:    key,
		Token:  token,
		Data:   tokenData,
	})

	system := &mockSystem{t: t}
	ws := websocket.NewBaseSocketServer(instruments.Instrument(instruments.InstrumentAttr{Log: mock.NewLogger()}), websocket.BaseSocketAttr{
		Authenticate: true,
	}, utils.NewContact(":6050"), pocket, system)

	server := httptest.NewServer(ws)
	server.URL = server.URL + "/ws"

	uri, _ := url.Parse(server.URL)
	serverURL := fmt.Sprintf("%s:%s", uri.Hostname(), uri.Port())

	t.Logf("\tWhen we make a initial websocket request without Authorization header to %q", serverURL)
	{
		conn, _, err := newWebsocketClient("ws://"+serverURL, map[string]string{
			"X-App": "Octo-App",
		})

		if err != nil {
			conn.Close()
			tests.Failed("Should have successfully failed to connect to websocket server without auhorization.")
		}
		tests.Passed("Should have successfully failed to connect to websocket server without auhorization.")
	}

	t.Logf("\tWhen we make a websocket request with Authorization sent as a message to %q", serverURL)
	{

		conn, _, err := newWebsocketClient("ws://"+serverURL, map[string]string{
			"X-App": "Octo-App",
		})

		if err != nil {
			conn.Close()
			tests.Failed("Should have successfully connected to websocket server without auhorization: %+q.", err.Error())
		}
		tests.Passed("Should have successfully connected to websocket server without auhorization.")

		mtype, mdata, merr := conn.ReadMessage()
		if merr != nil {
			conn.Close()
			tests.Failed("Should have successfully received message from server: %+q.", merr.Error())
		}
		tests.Passed("Should have successfully received message from server: %+q -> %d.", mdata, mtype)

		cmd, cerr := utils.ToCommand(mdata)
		if cerr != nil {
			conn.Close()
			tests.Failed("Should have successfully received command type from server: %+q.", cerr.Error())
		}
		tests.Passed("Should have successfully received command type from server.")

		if !bytes.Equal(cmd.Name, consts.AuthRequest) {
			tests.Failed("Should have successfully received AUTH command request from server.")
		}
		tests.Passed("Should have successfully received AUTH command request from server.")

		pocketData, _ := pocket.Bytes()
		auth, _, aerr := utils.NewCommandByte(consts.AuthResponse, pocketData)
		if aerr != nil {
			tests.Failed("Should have successfully created AuthResponse for server.")
		}
		tests.Passed("Should have successfully created AuthResponse for server.")

		if serr := conn.WriteMessage(gwebsocket.BinaryMessage, auth); serr != nil {
			tests.Failed("Should have successfully received delivered AUTH response to server: %+q.", serr)
		}
		tests.Passed("Should have successfully received delivered AUTH response to server.")

		mtype, mdata, merr = conn.ReadMessage()
		if merr != nil {
			conn.Close()
			tests.Failed("Should have successfully received message from server: %+q.", merr.Error())
		}
		tests.Passed("Should have successfully received message from server: %+q -> %d.", mdata, mtype)

		cmd, cerr = utils.ToCommand(mdata)
		if cerr != nil {
			conn.Close()
			tests.Failed("Should have successfully received command type from server: %+q.", cerr.Error())
		}
		tests.Passed("Should have successfully received command type from server.")

		if !bytes.Equal(cmd.Name, consts.OK) {
			tests.Failed("Should have successfully passed AUTH process with server.")
		}
		tests.Passed("Should have successfully passsed AUTH process with server.")

		conn.Close()
	}

	t.Logf("\tWhen we make a initial websocket request with Authorization header to %q", serverURL)
	{
		conn, _, err := newWebsocketClient("ws://"+serverURL, map[string]string{
			"X-App":         "Octo-App",
			"Authorization": "XBot api-32:auth-4531:BOMTx",
		})

		if err != nil {
			tests.Failed("Should have successfully connected to websocket server with auhorization: %q", err.Error())
		}

		conn.Close()

		tests.Passed("Should have successfully connected to websocket server with auhorization")
	}

	t.Logf("\tWhen we make a initial websocket request for 'INFO' details to %q", serverURL)
	{
		conn, _, err := newWebsocketClient("ws://"+serverURL, map[string]string{
			"X-App":         "Octo-App",
			"Authorization": "XBot api-32:auth-4531:BOMTx",
		})

		if err != nil {
			tests.Failed("Should have successfully connected to websocket server with auhorization: %q", err.Error())
		}
		tests.Passed("Should have successfully connected to websocket server with auhorization")

		defer conn.Close()

		if err := sendMessage(conn, string(consts.ContactRequest)); err != nil {
			tests.Failed("Should have successfully delivered 'INFO' messages: %q.", err.Error())
		}
		tests.Passed("Should have successfully delivered 'INFO' messages.")

		var cmd octo.Command
		if err := conn.ReadJSON(&cmd); err != nil {
			tests.Failed("Should have successfully connected to read messages: %q.", err.Error())
		}
		tests.Passed("Should have successfully connected to read messages.")

		if !bytes.Equal(cmd.Name, consts.ContactResponse) {
			tests.Failed("Should have successfully received 'INFORES' response: %+q.", cmd.Name)
		}
		tests.Passed("Should have successfully received 'INFORES' response: %+q.", cmd.Name)

		if err := conn.WriteControl(gwebsocket.CloseMessage, nil, time.Now().Add(2*time.Second)); err != nil {
			tests.Failed("Should have successfully delivered 'CLOSE' messages: %q.", err.Error())
		}
		tests.Passed("Should have successfully delivered 'CLOSE' messages.")
	}
}

var dailer = gwebsocket.Dialer{}

// newWebsocketClient returns a new request with the provided body as a command set.
func newWebsocketClient(url string, headers map[string]string) (*gwebsocket.Conn, *http.Response, error) {
	header := make(http.Header)

	for key, val := range headers {
		header.Set(key, val)
	}

	return dailer.Dial(url, header)
}

// sendMessage delivers the giving command to the websoket.
func sendMessage(conn *gwebsocket.Conn, command string, data ...string) error {
	var bu bytes.Buffer

	if err := json.NewEncoder(&bu).Encode(utils.NewCommand(command, data...)); err != nil {
		return err
	}

	return conn.WriteMessage(gwebsocket.BinaryMessage, bu.Bytes())
}
