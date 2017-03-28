package udp_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/influx6/faux/tests"
	"github.com/influx6/octo"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/instruments"
	"github.com/influx6/octo/messages/jsoni"
	"github.com/influx6/octo/mock"
	"github.com/influx6/octo/streams/server"
	"github.com/influx6/octo/streams/server/udp"
)

var (
	scheme    = "XBot"
	key       = "api-32"
	token     = "auth-4531"
	tokenData = "BOMTx"
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

	if cred.Data != tokenData {
		return errors.New("Data  does not match")
	}

	return nil
}

// Serve handles the processing of different requests coming from the outside.
func (mockSystem) Serve(message []byte, tx server.Stream) error {
	var command jsoni.CommandMessage

	if err := json.Unmarshal(message, &command); err != nil {
		return err
	}

	switch command.Name {
	case "OK":
		return nil
	case "PUMP":
		return tx.Send([]byte("RUMP"), true)
	case "REX":
		return tx.Send([]byte("DEX"), true)
	}

	return errors.New("Invalid Command")
}

// TestUDPServer validates the functionality of the udp servers and client connections
// according to the octo standards.
func TestUDPServer(t *testing.T) {
	// pocket := mock.NewCredentialPocket(octo.AuthCredential{})
	system := &mockSystem{t: t}
	server := udp.New(instruments.Instruments(mock.NewTestLogger(), nil), udp.ServerAttr{
		Addr:         ":5060",
		Version:      udp.Ver0,
		Authenticate: true,
	})

	if err := server.Listen(system); err != nil {
		tests.Failed("Should have successfully created udp server: %+q.", err.Error())
	}
	tests.Passed("Should have successfully created udp server.")

	defer server.Close()

	t.Logf("\tWhen connecting to a UDP Server without credentials")
	{

		client, err := mock.NewUDPClient("udp", ":4060", ":5060")
		if err != nil {
			tests.Failed("Should have successfully created udp client: %+q.", err.Error())
		}
		tests.Passed("Should have successfully created udp client for %q.", ":4060")

		defer client.Close()

		if merr := sendMessage(t, client, nil, string(consts.ContactRequest), nil); merr != nil {
			tests.Failed("Should have successfully delivered message %+q to server due to authentication.", consts.ContactRequest)
		}
		tests.Passed("Should have successfully delivered message %+q to server due to authentication.", consts.ContactRequest)

		response, addr, err := client.Read()
		if err != nil {
			tests.Failed("Should have successfully received response from udp client from %q: %+q.", err.Error(), addr.String())
		}
		tests.Passed("Should have successfully received response from udp client: %q.", addr.String())

		cmdResponse, err := jsoni.Parser.Decode(response)
		if err != nil {
			tests.Failed("Should have successfully received command object as response: %+q.", err.Error())
		}
		tests.Passed("Should have successfully received command object as response.")

		commandResponse, ok := (cmdResponse.([]jsoni.CommandMessage))
		if !ok {
			tests.Failed("Should have successfully received command object as response: %+q.", err.Error())
		}
		tests.Passed("Should have successfully received command object as response.")

		if !bytes.Equal(consts.AuthroizationDenied, []byte(commandResponse[0].Name)) {
			tests.Failed("Should have successfully received AuthorizationDenied response without authorization.")
		}
		tests.Passed("Should have successfully received AuthorizationDenied response without authorization.")
	}

	t.Logf("\tWhen connecting to a UDP Server with credentials")
	{
		client, err := mock.NewUDPClient("udp", ":6060", ":5060")
		if err != nil {
			tests.Failed("Should have successfully created udp client: %+q.", err.Error())
		}
		tests.Passed("Should have successfully created udp client for %q.", ":4060")

		defer client.Close()

		authData := octo.AuthCredential{
			Scheme: scheme,
			Key:    key,
			Token:  token,
			Data:   tokenData,
		}

		if err != nil {
			tests.Failed("Should have successfully converted AuthCredentials to JSON: %+q.", err)
		}
		tests.Passed("Should have successfully converted AuthCredentials to JSON.")

		if merr := sendMessage(t, client, nil, string(consts.AuthResponse), authData); merr != nil {
			tests.Failed("Should have successfully delivered message %+q to server due to authentication.", consts.ContactRequest)
		}
		tests.Passed("Should have successfully delivered message %+q to server due to authentication.", consts.ContactRequest)

		response, addr, err := client.Read()
		if err != nil {
			tests.Failed("Should have successfully received response from udp client from %q: %+q.", err.Error(), addr.String())
		}
		tests.Passed("Should have successfully received response from udp client: %q.", addr.String())

		cmdResponse, err := jsoni.Parser.Decode(response)
		if err != nil {
			tests.Failed("Should have successfully received command object as response: %+q.", err.Error())
		}
		tests.Passed("Should have successfully received command object as response.")

		commandResponse, ok := (cmdResponse.([]jsoni.CommandMessage))
		if !ok {
			tests.Failed("Should have successfully received command object as response: %+q.", err.Error())
		}
		tests.Passed("Should have successfully received command object as response.")

		if !bytes.Equal(consts.AuthroizationGranted, []byte(commandResponse[0].Name)) {
			tests.Failed("Should have successfully received AuthorizatioGranted response without authorization.")
		}
		tests.Passed("Should have successfully received AuthorizationGranted response without authorization.")

		if merr := sendMessage(t, client, nil, string(consts.ContactRequest), nil); merr != nil {
			tests.Failed("Should have successfully delivered message %+q to server due to authentication.", consts.ContactRequest)
		}
		tests.Passed("Should have successfully delivered message %+q to server due to authentication.", consts.ContactRequest)

		response2, addr, err := client.Read()
		if err != nil {
			tests.Failed("Should have successfully received response from udp client from %q: %+q.", err.Error(), addr.String())
		}
		tests.Passed("Should have successfully received response from udp client: %q.", addr.String())

		cmdResponse2, err := jsoni.Parser.Decode(response2)
		if err != nil {
			tests.Failed("Should have successfully received command object as response: %+q.", err.Error())
		}
		tests.Passed("Should have successfully received command object as response.")

		commandResponse2, ok := (cmdResponse2.([]jsoni.CommandMessage))
		if !ok {
			tests.Failed("Should have successfully received command object as response: %+q.", err.Error())
		}
		tests.Passed("Should have successfully received command object as response.")

		if !bytes.Equal(consts.ContactResponse, []byte(commandResponse2[0].Name)) {
			tests.Failed("Should have successfully received ContactResponse response without authorization.")
		}
		tests.Passed("Should have successfully received ContactRespone response without authorization.")

	}
}

// sendMessage delivers the giving command to the websoket.
func sendMessage(t *testing.T, conn *mock.UDPClient, addr *net.UDPAddr, command string, data interface{}) error {
	tests.Info("Sending Command: %q Data: %+q, Addr: %q", command, data, addr.String())

	var bu bytes.Buffer

	if err := json.NewEncoder(&bu).Encode(jsoni.CommandMessage{
		Name: command,
		Data: data,
	}); err != nil {
		return err
	}

	return conn.Write(bu.Bytes(), addr)
}
