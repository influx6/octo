package http_test

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/influx6/faux/tests"
	"github.com/influx6/octo"
	"github.com/influx6/octo/instruments"
	"github.com/influx6/octo/messages/jsoni"
	"github.com/influx6/octo/mock"
	"github.com/influx6/octo/streams/client"
	"github.com/influx6/octo/streams/client/http"
	"github.com/influx6/octo/streams/server"
	httpbasic "github.com/influx6/octo/streams/server/http"
	"github.com/influx6/octo/utils"
)

func TestHTTPPod(t *testing.T) {
	t.Logf("Given a need to connect to a http server using octo")
	{
		insts := instruments.Instruments(mock.NewTestLogger(), nil)

		serverSystem := mock.NewServerCommandSystem(octo.AuthCredential{
			Scheme: "XBot",
			Key:    "api-32",
			Token:  "auth-4531",
			Data:   "BOMTx",
		})

		clientSystem := mock.NewCallbackClientSystem(octo.AuthCredential{
			Scheme: "XBot",
			Key:    "api-32",
			Token:  "auth-4531",
			Data:   "BOMTx",
		}, func(command jsoni.CommandMessage, tx client.Stream) error {
			switch command.Name {
			case "RUMP":
				tests.Passed("Should have successfully recieved expected response")
				return tx.Send(jsoni.CommandMessage{Name: "REX"}, true)
			case "DEX":
				tests.Passed("Should have successfully recieved expected response")
				return tx.Send(jsoni.CommandMessage{Name: "INFO"}, true)
			case "INFORES":
				tests.Passed("Should have successfully recieved expected response")
				return nil
			case "OK":
				tests.Passed("Should have successfully recieved expected response")
				return nil
			default:
				return errors.New("Unknown")
			}
		})

		pocket := mock.NewCredentialPocket(octo.AuthCredential{
			Scheme: "XBot",
			Key:    "api-32",
			Token:  "auth-4531",
			Data:   ("BOMTx"),
		})

		httpMux := newBasicServeHTTP(true, pocket, serverSystem)
		server := httptest.NewServer(httpMux)
		serverURL := server.URL

		t.Logf("\tWhen provided a system for handling request")
		{
			coclient, err := http.New(insts, http.Attr{
				Authenticate: true,
				Addr:         serverURL,
				Headers: map[string]string{
					"X-App-Client": "Octo-CHTTP",
				},
			})

			if err != nil {
				tests.Failed("Should have successfully created connection for http server: %+q.", err)
			}
			tests.Passed("Should have successfully created connection for http server.")

			if err := coclient.Listen(clientSystem, jsoni.Parser); err != nil {
				tests.Failed("Should have successfully listened for connection to http server: %+q.", err)
			}
			tests.Passed("Should have successfully listened for connection to http server.")

			if err := coclient.Send(jsoni.CommandMessage{
				Name: ("PUMP"),
			}, true); err != nil {
				tests.Failed("Should have successfully delivered command to server: %+q.", err)
			}
			tests.Passed("Should have successfully delivered command to server.")

			if err := coclient.Send(jsoni.CommandMessage{
				Name: ("GLOP"),
			}, true); err == nil {
				tests.Failed("Should have successfully failed to process command to server: %+q.", err)
			}
			tests.Passed("Should have successfully failed to process command to server.")

			clientSystem.Wait()
		}

	}
}

func newBasicServeHTTP(authenticate bool, cred octo.Credentials, system server.System) *httpbasic.BasicServeHTTP {
	return httpbasic.NewBasicServeHTTP(httpbasic.BasicServeAttr{
		Authenticate: authenticate,
		SkipCORS:     false,
		Instruments:  instruments.Instruments(mock.NewTestLogger(), nil),
		Pub:          server.NewPub(),
		Info:         utils.NewContact(":6070"),
		Auth:         cred,
		System:       system,
	})
}
