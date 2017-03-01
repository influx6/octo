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
	"github.com/influx6/octo"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/mock"
	"github.com/influx6/octo/tests"
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
func (mockSystem) Serve(message []byte, tx octo.Transmission) error {
	var command octo.Command

	if err := json.Unmarshal(message, &command); err != nil {
		return err
	}

	switch {
	// case bytes.Equal(consts.AuthRequest, command.Name):
	case bytes.Equal([]byte("PUMP"), command.Name):
		return tx.Send([]byte("RUMP"), true)
	case bytes.Equal([]byte("REX"), command.Name):
		return tx.Send([]byte("DEX"), true)
	}

	return errors.New("Invalid Command")
}

// TestWebsocketSystem validates the behaviour of the websocket that it matches
// the octo standards.
func TestWebsocketSystem(t *testing.T) {
	pocket := mock.NewCredentialPocket(octo.AuthCredential{})
	system := &mockSystem{t: t}
	ws := websocket.NewBaseSocketServer(websocket.BaseSocketAttr{
		Authenticate: true,
	}, mock.NewLogger(t), utils.NewInfo(":6050"), pocket, system)

	server := httptest.NewServer(ws)
	server.URL = server.URL + "/ws"

	uri, _ := url.Parse(server.URL)
	serverURL := fmt.Sprintf("%s:%s", uri.Hostname(), uri.Port())

	t.Logf("\tWhen we make a initial websocket request without Authorization header to %q", serverURL)
	{
		conn, _, err := newWebsocketClient("ws://"+serverURL, map[string]string{
			"X-App": "Octo-App",
		})

		if err == nil {
			conn.Close()
			tests.Failed(t, "Should have successfully failed to connect to websocket server without auhorization.")
		}
		tests.Passed(t, "Should have successfully failed to connect to websocket server without auhorization.")
	}

	t.Logf("\tWhen we make a initial websocket request with Authorization header to %q", serverURL)
	{
		conn, _, err := newWebsocketClient("ws://"+serverURL, map[string]string{
			"X-App":         "Octo-App",
			"Authorization": "XBot api-32:auth-4531:BOMTx",
		})

		if err != nil {
			tests.Failed(t, "Should have successfully connected to websocket server with auhorization: %q", err.Error())
		}

		defer conn.Close()

		tests.Passed(t, "Should have successfully connected to websocket server with auhorization")
	}

	t.Logf("\tWhen we make a initial websocket request for 'INFO' details to %q", serverURL)
	{
		conn, _, err := newWebsocketClient("ws://"+serverURL, map[string]string{
			"X-App":         "Octo-App",
			"Authorization": "XBot api-32:auth-4531:BOMTx",
		})

		if err != nil {
			tests.Failed(t, "Should have successfully connected to websocket server with auhorization: %q", err.Error())
		}
		tests.Passed(t, "Should have successfully connected to websocket server with auhorization")

		defer conn.Close()

		if err := sendMessage(conn, string(consts.InfoRequest)); err != nil {
			tests.Failed(t, "Should have successfully delivered 'INFO' messages: %q.", err.Error())
		}
		tests.Passed(t, "Should have successfully delivered 'INFO' messages.")

		var cmd octo.Command
		if err := conn.ReadJSON(&cmd); err != nil {
			tests.Failed(t, "Should have successfully connected to read messages: %q.", err.Error())
		}
		tests.Passed(t, "Should have successfully connected to read messages.")

		if !bytes.Equal(cmd.Name, consts.InfoResponse) {
			tests.Failed(t, "Should have successfully received 'INFORES' response: %+q.", cmd.Name)
		}
		tests.Passed(t, "Should have successfully received 'INFORES' response: %+q.", cmd.Name)

		if err := conn.WriteControl(gwebsocket.CloseMessage, nil, time.Now().Add(2*time.Second)); err != nil {
			tests.Failed(t, "Should have successfully delivered 'CLOSE' messages: %q.", err.Error())
		}
		tests.Passed(t, "Should have successfully delivered 'CLOSE' messages.")
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
