package http_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/influx6/octo"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/instruments"
	"github.com/influx6/octo/mock"
	"github.com/influx6/octo/tests"
	"github.com/influx6/octo/transmission/httpbasic"
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

// TestHTTPBaiscProtocol validates the http basic protocol for the
// octo package.
func TestHTTPBaiscProtocol(t *testing.T) {
	pocket := mock.NewCredentialPocket(octo.AuthCredential{})

	server := newBasicServeHTTP(t, true, pocket, mockSystem{})

	t.Logf("\tWhen request %q command with correct authorization", consts.InfoRequest)
	{
		header := make(map[string]string)
		header["X-App"] = "Octo-App"
		header["Authorization"] = "XBot api-32:auth-4531:BOMTx"

		req, err := newMessageRequest(header, string(consts.InfoRequest))
		if err != nil {
			tests.Failed(t, "Should have successfully created request for command %q", "info")
		}
		tests.Passed(t, "Should have successfully created request for command %q", "info")

		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, req)

		var received octo.Command
		if err := json.Unmarshal(recorder.Body.Bytes(), &received); err != nil {
			tests.Failed(t, "Should have successfully parsed response for command %q: %q", "info", err.Error())
		}
		tests.Passed(t, "Should have successfully parsed response for command %q", "info")

		if !bytes.Equal(received.Name, consts.InfoResponse) {
			tests.Failed(t, "Should have successfully matched response command as %+q", consts.InfoResponse)
		}
		tests.Passed(t, "Should have successfully matched response command as %+q", consts.InfoResponse)
	}

	t.Logf("\tWhen request %q command with bad authorization", consts.InfoRequest)
	{
		header := make(map[string]string)
		header["X-App"] = "Octo-App"
		header["Authorization"] = "XBot api-35:auth-6531:BOMTx"

		req, err := newMessageRequest(header, string(consts.InfoRequest))
		if err != nil {
			tests.Failed(t, "Should have successfully created request for command %q", consts.InfoRequest)
		}
		tests.Passed(t, "Should have successfully created request for command %q", consts.InfoRequest)

		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusInternalServerError {
			tests.Failed(t, "Should have successfully failed to make request command %+q: %d", consts.InfoRequest, recorder.Code)
		}
		tests.Passed(t, "Should have successfully failed to make request command %+q", consts.InfoRequest)
	}

}

func newBasicServeHTTP(t *testing.T, authenticate bool, cred octo.Credentials, system octo.System) *httpbasic.BasicServeHTTP {
	return httpbasic.NewBasicServeHTTP(
		authenticate,
		instruments.Instrument(instruments.InstrumentAttr{Log: mock.NewLogger(t)}),
		utils.NewInfo(":6070"),
		cred,
		system,
	)
}

// newMessageRequest returns a new request with the provided body as a command set.
func newMessageRequest(header map[string]string, command string, msgs ...string) (*http.Request, error) {
	var dataMessages [][]byte

	for _, data := range msgs {
		dataMessages = append(dataMessages, []byte(data))
	}

	var body bytes.Buffer

	if err := json.NewEncoder(&body).Encode(octo.Command{Name: []byte(command), Data: dataMessages}); err != nil {
		return nil, err
	}

	rq, err := http.NewRequest("GET", "/basic", &body)
	if err != nil {
		return nil, err
	}

	for key, value := range header {
		rq.Header.Set(key, value)
	}

	return rq, nil
}
