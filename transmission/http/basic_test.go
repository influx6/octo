package http_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/influx6/faux/tests"
	"github.com/influx6/octo"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/instruments"
	"github.com/influx6/octo/mock"
	"github.com/influx6/octo/transmission"
	httpbasic "github.com/influx6/octo/transmission/http"
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
	commands, err := utils.ToCommands(message)
	if err != nil {
		return err
	}

	for _, command := range commands {
		switch command.Name {
		case "PUMP":
			if err := tx.Send([]byte("RUMP"), true); err != nil {
				return err
			}

			continue
		case "REX":
			if err := tx.Send([]byte("DEX"), true); err != nil {
				return err
			}
			continue
		default:
			return errors.New("Invalid Command")
		}
	}

	return nil
}

// TestHTTPBaiscProtocol validates the http basic protocol for the
// octo package.
func TestHTTPBaiscProtocol(t *testing.T) {
	pocket := mock.NewCredentialPocket(octo.AuthCredential{})

	server := newBasicServeHTTP(t, true, pocket, mockSystem{})

	t.Logf("\tWhen request %q command with correct authorization", consts.ContactRequest)
	{
		header := make(map[string]string)
		header["X-App"] = "Octo-App"
		header["Authorization"] = "XBot api-32:auth-4531:BOMTx"

		req, err := newMessageRequest(header, string(consts.ContactRequest))
		if err != nil {
			tests.Failed("Should have successfully created request for command %q", "info")
		}
		tests.Passed("Should have successfully created request for command %q", "info")

		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, req)

		var received octo.Command
		if err := json.Unmarshal(recorder.Body.Bytes(), &received); err != nil {
			tests.Failed("Should have successfully parsed response for command %q: %q", "info", err.Error())
		}
		tests.Passed("Should have successfully parsed response for command %q", "info")

		if received.Name != string(consts.ContactResponse) {
			tests.Failed("Should have successfully matched response command as %+q", consts.ContactResponse)
		}
		tests.Passed("Should have successfully matched response command as %+q", consts.ContactResponse)
	}

	t.Logf("\tWhen request %q command with bad authorization", consts.ContactRequest)
	{
		header := make(map[string]string)
		header["X-App"] = "Octo-App"
		header["Authorization"] = "XBot api-35:auth-6531:BOMTx"

		req, err := newMessageRequest(header, string(consts.ContactRequest))
		if err != nil {
			tests.Failed("Should have successfully created request for command %q", consts.ContactRequest)
		}
		tests.Passed("Should have successfully created request for command %q", consts.ContactRequest)

		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusInternalServerError {
			tests.Failed("Should have successfully failed to make request command %+q: %d", consts.ContactRequest, recorder.Code)
		}
		tests.Passed("Should have successfully failed to make request command %+q", consts.ContactRequest)
	}

	t.Logf("\tWhen request %q command with authorization", "REX")
	{
		header := make(map[string]string)
		header["X-App"] = "Octo-App"
		header["Authorization"] = "XBot api-32:auth-4531:BOMTx"

		req, err := newMessageRequest(header, string("REX"))
		if err != nil {
			tests.Failed("Should have successfully created request for command %q", consts.ContactRequest)
		}
		tests.Passed("Should have successfully created request for command %q", consts.ContactRequest)

		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusOK {
			tests.Failed("Should have successfully failed to make request command %+q: %d", consts.ContactRequest, recorder.Code)
		}
		tests.Passed("Should have successfully failed to make request command %+q", consts.ContactRequest)

		if !bytes.Equal(recorder.Body.Bytes(), []byte("DEX")) {
			tests.Failed("Should have successfully received response %+q from server: %+q", "DEX", recorder.Body.Bytes())
		}
		tests.Passed("Should have successfully received response %+q from server: %+q", "DEX", recorder.Body.Bytes())
	}

}

func newBasicServeHTTP(t *testing.T, authenticate bool, cred octo.Credentials, system transmission.System) *httpbasic.BasicServeHTTP {
	return httpbasic.NewBasicServeHTTP(
		authenticate,
		instruments.Instrument(instruments.InstrumentAttr{Log: mock.NewLogger()}),
		utils.NewContact(":6070"),
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

	if err := json.NewEncoder(&body).Encode(octo.Command{Name: command, Data: dataMessages}); err != nil {
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