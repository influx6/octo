package chttp_test

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/influx6/faux/tests"
	"github.com/influx6/octo"
	"github.com/influx6/octo/clients/goclient"
	"github.com/influx6/octo/clients/goclient/chttp"
	"github.com/influx6/octo/instruments"
	"github.com/influx6/octo/mock"
	"github.com/influx6/octo/transmission"
	"github.com/influx6/octo/transmission/httpbasic"
	"github.com/influx6/octo/utils"
)

func TestHTTPPod(t *testing.T) {
	t.Logf("Given a need to connect to a http server using octo")
	{
		insts := instruments.Instrument(instruments.InstrumentAttr{Log: mock.NewLogger()})
		serverSystem := mock.NewServerCommandSystem(octo.AuthCredential{
			Scheme: "XBot",
			Key:    "api-32",
			Token:  "auth-4531",
			Data:   []byte("BOMTx"),
		})

		clientSystem := mock.NewCallbackClientSystem(octo.AuthCredential{
			Scheme: "XBot",
			Key:    "api-32",
			Token:  "auth-4531",
			Data:   []byte("BOMTx"),
		}, func(command octo.Command, tx goclient.Stream) error {
			switch string(command.Name) {
			case "RUMP":
				tests.Passed("Should have successfully recieved expected response")
				return tx.Send(octo.Command{Name: []byte("REX")}, true)
			case "DEX":
				tests.Passed("Should have successfully recieved expected response")
				return tx.Send(octo.Command{Name: []byte("INFO")}, true)
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
			Data:   []byte("BOMTx"),
		})

		httpMux := newBasicServeHTTP(true, pocket, serverSystem)
		server := httptest.NewServer(httpMux)
		serverURL := server.URL

		t.Logf("\tWhen provided a system for handling request")
		{
			client, err := chttp.New(insts, chttp.Attr{
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

			client.Register(octo.ConnectHandler, func(c octo.Contact) {
				fmt.Printf("Connect Contact: %#v\n", c)
			})

			client.Register(octo.DisconnectHandler, func(c octo.Contact) {
				fmt.Printf("Disconnect Contact: %#v\n", c)
			})

			if err := client.Listen(clientSystem, mock.CommandEncoding{}); err != nil {
				tests.Failed("Should have successfully listened for connection to http server: %+q.", err)
			}
			tests.Passed("Should have successfully listened for connection to http server.")

			if err := client.Send(octo.Command{
				Name: []byte("PUMP"),
			}, true); err != nil {
				tests.Failed("Should have successfully delivered command to server: %+q.", err)
			}
			tests.Passed("Should have successfully delivered command to server.")

			if err := client.Send(octo.Command{
				Name: []byte("GLOP"),
			}, true); err == nil {
				tests.Failed("Should have successfully failed to process command to server: %+q.", err)
			}
			tests.Passed("Should have successfully failed to process command to server.")

			clientSystem.Wait()
		}

	}
}

func newBasicServeHTTP(authenticate bool, cred octo.Credentials, system transmission.System) *httpbasic.BasicServeHTTP {
	return httpbasic.NewBasicServeHTTP(
		authenticate,
		instruments.Instrument(instruments.InstrumentAttr{Log: mock.NewLogger()}),
		utils.NewContact(":6070"),
		cred,
		system,
	)
}
