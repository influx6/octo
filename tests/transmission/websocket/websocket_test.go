package websocket_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	gwebsocket "github.com/gorilla/websocket"
	"github.com/influx6/octo"
	"github.com/influx6/octo/mock"
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

func TestWebsocketSystem(t *testing.T) {
	pocket := mock.NewCredentialPocket(octo.AuthCredential{})
	system := &mockSystem{t: t}
	ws := websocket.NewBaseSocketServer(websocket.BaseSocketAttr{
		Authenticate: true,
	}, mock.NewLogger(t), utils.NewInfo(":6050"), pocket, system)

	_ = ws
}

var dailer = gwebsocket.Dialer{}

// newWebsocketClient returns a new request with the provided body as a command set.
func newWebsocketClient(url string, headers map[string]string, command string, msgs ...string) (*gwebsocket.Conn, *http.Response, error) {
	header := make(http.Header)

	for key, val := range headers {
		header.Set(key, val)
	}

	return dailer.Dial(url, header)
}
