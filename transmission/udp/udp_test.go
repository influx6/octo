package udp_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/influx6/octo"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/instruments"
	"github.com/influx6/octo/mock"
	"github.com/influx6/octo/tests"
	"github.com/influx6/octo/transmission"
	"github.com/influx6/octo/transmission/udp"
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
	fmt.Printf("Validating Credentails: %+q\n", cred)

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
	// case bytes.Equal(consts.AuthRequest, command.Name):
	case bytes.Equal([]byte("PUMP"), command.Name):
		return tx.Send([]byte("RUMP"), true)
	case bytes.Equal([]byte("REX"), command.Name):
		return tx.Send([]byte("DEX"), true)
	}

	return errors.New("Invalid Command")
}

// TestUDPServer validates the functionality of the udp servers and client connections
// according to the octo standards.
func TestUDPServer(t *testing.T) {
	// pocket := mock.NewCredentialPocket(octo.AuthCredential{})
	system := &mockSystem{t: t}
	server := udp.New(instruments.Instrument(instruments.InstrumentAttr{Log: mock.NewLogger(t)}), udp.ServerAttr{
		Addr:         ":5060",
		Version:      udp.Ver0,
		Authenticate: true,
	})

	if err := server.Listen(system); err != nil {
		tests.Failed(t, "Should have successfully created udp server: %+q.", err.Error())
	}
	tests.Passed(t, "Should have successfully created udp server.")

	defer server.Close()

	t.Logf("\tWhen connecting to a UDP Server without credentials")
	{

		client, err := mock.NewUDPClient("udp", ":4060", ":5060")
		if err != nil {
			tests.Failed(t, "Should have successfully created udp client: %+q.", err.Error())
		}
		tests.Passed(t, "Should have successfully created udp client for %q.", ":4060")

		defer client.Close()

		if merr := sendMessage(t, client, nil, string(consts.InfoRequest)); merr != nil {
			tests.Failed(t, "Should have successfully delivered message %+q to server due to authentication.", consts.InfoRequest)
		}
		tests.Passed(t, "Should have successfully delivered message %+q to server due to authentication.", consts.InfoRequest)

		response, addr, err := client.Read()
		if err != nil {
			tests.Failed(t, "Should have successfully received response from udp client from %q: %+q.", err.Error(), addr.String())
		}
		tests.Passed(t, "Should have successfully received response from udp client: %q.", addr.String())

		commandResponse, err := utils.ToCommand(response)
		if err != nil {
			tests.Failed(t, "Should have successfully received command object as response: %+q.", err.Error())
		}
		tests.Passed(t, "Should have successfully received command object as response.")

		if !bytes.Equal(consts.AuthroizationDenied, commandResponse.Name) {
			tests.Failed(t, "Should have successfully received AuthorizationDenied response without authorization.")
		}
		tests.Passed(t, "Should have successfully received AuthorizationDenied response without authorization.")
	}

	t.Logf("\tWhen connecting to a UDP Server with credentials")
	{
		client, err := mock.NewUDPClient("udp", ":6060", ":5060")
		if err != nil {
			tests.Failed(t, "Should have successfully created udp client: %+q.", err.Error())
		}
		tests.Passed(t, "Should have successfully created udp client for %q.", ":4060")

		defer client.Close()

		authData, err := utils.AuthCredentialToJSON(octo.AuthCredential{
			Scheme: scheme,
			Key:    key,
			Token:  token,
			Data:   tokenData,
		})

		if err != nil {
			tests.Failed(t, "Should have successfully converted AuthCredentials to JSON: %+q.", err)
		}
		tests.Passed(t, "Should have successfully converted AuthCredentials to JSON.")

		if merr := sendMessage(t, client, nil, string(consts.AuthResponse), string(authData)); merr != nil {
			tests.Failed(t, "Should have successfully delivered message %+q to server due to authentication.", consts.InfoRequest)
		}
		tests.Passed(t, "Should have successfully delivered message %+q to server due to authentication.", consts.InfoRequest)

		response, addr, err := client.Read()
		if err != nil {
			tests.Failed(t, "Should have successfully received response from udp client from %q: %+q.", err.Error(), addr.String())
		}
		tests.Passed(t, "Should have successfully received response from udp client: %q.", addr.String())

		commandResponse, err := utils.ToCommand(response)
		if err != nil {
			tests.Failed(t, "Should have successfully received command object as response: %+q.", err.Error())
		}
		tests.Passed(t, "Should have successfully received command object as response.")

		if !bytes.Equal(consts.AuthroizationGranted, commandResponse.Name) {
			tests.Failed(t, "Should have successfully received AuthorizatioGranted response without authorization.")
		}
		tests.Passed(t, "Should have successfully received AuthorizationGranted response without authorization.")

		if merr := sendMessage(t, client, nil, string(consts.InfoRequest)); merr != nil {
			tests.Failed(t, "Should have successfully delivered message %+q to server due to authentication.", consts.InfoRequest)
		}
		tests.Passed(t, "Should have successfully delivered message %+q to server due to authentication.", consts.InfoRequest)

		response2, addr, err := client.Read()
		if err != nil {
			tests.Failed(t, "Should have successfully received response from udp client from %q: %+q.", err.Error(), addr.String())
		}
		tests.Passed(t, "Should have successfully received response from udp client: %q.", addr.String())

		commandResponse2, err := utils.ToCommand(response2)
		if err != nil {
			tests.Failed(t, "Should have successfully received command object as response: %+q.", err.Error())
		}
		tests.Passed(t, "Should have successfully received command object as response.")

		if !bytes.Equal(consts.InfoResponse, commandResponse2.Name) {
			tests.Failed(t, "Should have successfully received InfoResponse response without authorization.")
		}
		tests.Passed(t, "Should have successfully received InfoRespone response without authorization.")

	}
}

// sendMessage delivers the giving command to the websoket.
func sendMessage(t *testing.T, conn *mock.UDPClient, addr *net.UDPAddr, command string, data ...string) error {
	tests.Info(t, "Sending Command: %q Data: %+q, Addr: %q", command, data, addr.String())
	var bu bytes.Buffer
	if err := json.NewEncoder(&bu).Encode(utils.NewCommand(command, data...)); err != nil {
		return err
	}

	return conn.Write(bu.Bytes(), addr)
}
