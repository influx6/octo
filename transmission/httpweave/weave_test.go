package httpweave_test

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
	"github.com/influx6/octo/parsers/byteutils"
	"github.com/influx6/octo/tests"
	"github.com/influx6/octo/transmission"
	"github.com/influx6/octo/transmission/httpweave"
	"github.com/influx6/octo/transmission/tcp"
	"github.com/influx6/octo/utils"
	uuid "github.com/satori/go.uuid"
)

var (
	scheme    = "XBot"
	key       = "api-32"
	token     = "auth-4531"
	tokenData = []byte("BOMTx")
)

//================================================================================

// SimpleTCPTransformer defines a basic TCPTransformer which simply returns the
// internal data of the transformer.
type simpleTCPTransformer struct{}

// TransformRequest returns the appropriate data for a giving TCPRequest struct value,
// This function simply jsonifies the giving requests.
func (simpleTCPTransformer) TransformRequest(t httpweave.TCPRequest) ([]byte, error) {
	data, err := json.Marshal(t)
	if err != nil {
		return nil, err
	}

	return data, nil
}

// TransformResponse returns a TCPResponse value by using the json parser to parse
// the provided data.
func (simpleTCPTransformer) TransformResponse(data []byte) (httpweave.TCPResponse, error) {
	var res httpweave.TCPResponse

	if err := json.Unmarshal(data, &res); err != nil {
		return res, err
	}

	return res, nil
}

//================================================================================

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
	var treq httpweave.TCPRequest

	if err := json.Unmarshal(message, &treq); err != nil {
		return err
	}

	clientContact, _ := tx.Contact()
	infoData, infoErr := json.Marshal(&clientContact)

	response, resErr := json.Marshal(&httpweave.TCPResponse{
		UUID:    treq.UUID,
		Status:  true,
		Data:    infoData,
		Error:   infoErr,
		Request: treq,
	})

	if resErr != nil {
		return resErr
	}

	return tx.Send(response, true)
}

// TestHTTPBaiscProtocol validates the http basic protocol for the
// octo package.
func TestHTTPBaiscProtocol(t *testing.T) {
	system := &mockSystem{t: t}
	pocket := mock.NewCredentialPocket(octo.AuthCredential{})
	inst := instruments.Instrument(instruments.InstrumentAttr{Log: mock.NewLogger(t)})

	server := newWeaveHTTP(t, inst, system, pocket, system)
	tcpserver := tcp.New(inst, tcp.ServerAttr{
		Addr: ":7060",
	})

	tcpserver.Listen(system)

	defer tcpserver.Close()

	t.Logf("\tWhen request %q for a tcp resource through http", consts.ContactRequest)
	{
		header := make(map[string]string)
		header["X-App"] = "Octo-App"
		header["Authorization"] = "XBot api-32:auth-4531:BOMTx"

		req, err := newMessageRequest(header, byteutils.MakeByteMessage(consts.ContactRequest))
		if err != nil {
			tests.Failed("Should have successfully created request for command %q", "info")
		}
		tests.Passed("Should have successfully created request for command %q", "info")

		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, req)

		var tcpResponse httpweave.TCPResponse
		if err := json.NewDecoder(recorder.Body).Decode(&tcpResponse); err != nil {
			tests.Failed("Should have successfully parsed tcp response from http server for %q: %q", "info", err.Error())
		}
		tests.Passed("Should have successfully parsed tcp response from http server for %q.", "info")

		if tcpResponse.Error != nil {
			tests.Failed("Should have successfully received tcp response with no error: %q", tcpResponse.Error)
		}
		tests.Passed("Should have successfully received tcp response with no error.")

		var info octo.Contact
		if err := json.NewDecoder(bytes.NewBuffer(tcpResponse.Data)).Decode(&info); err != nil {
			tests.Failed("Should have successfully parsed tcp response to generate octo.Command for %q: %q", "info", err.Error())
		}
		tests.Passed("Should have successfully parsed tcp response to generate octo.Command for %q.", "info")

		if info.SUUID != tcpserver.Contact().SUUID {
			tests.Failed("Should have successfully received tcp servers info: %q", info)
		}
		tests.Passed("Should have successfully received tcp servers info.")
	}
}

func newWeaveHTTP(t *testing.T, inst octo.Instrumentation, authenticate octo.Authenticator, cred octo.Credentials, system transmission.System) *httpweave.WeaveServerMux {
	return httpweave.NewWeaveServerMux(
		inst,
		authenticate,
		simpleTCPTransformer{},
		utils.NewContact(":5060"),
		":7060",
		nil,
	)
}

// newMessageRequest returns a new request with the provided body as a command set.
func newMessageRequest(header map[string]string, data []byte) (*http.Request, error) {
	var tcpreq httpweave.TCPRequest
	tcpreq.UUID = uuid.NewV4().String()
	tcpreq.Data = data

	var tbody bytes.Buffer

	if err := json.NewEncoder(&tbody).Encode(tcpreq); err != nil {
		return nil, err
	}

	rq, err := http.NewRequest("GET", "/", &tbody)
	if err != nil {
		return nil, err
	}

	for key, value := range header {
		rq.Header.Set(key, value)
	}

	return rq, nil
}
