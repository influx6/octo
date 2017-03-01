package udp_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"testing"

	"github.com/gu-io/gu/tests"
	"github.com/influx6/octo"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/mock"
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

// TestUDPServer validates the functionality of the udp servers and client connections
// according to the octo standards.
func TestUDPServer(t *testing.T) {
	// pocket := mock.NewCredentialPocket(octo.AuthCredential{})
	system := &mockSystem{t: t}
	server := udp.New(mock.NewLogger(t), udp.ServerAttr{
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

		if err := sendMessage(client, nil, string(consts.InfoRequest)); err != nil {
			tests.Failed(t, "Should have successfully delivered message %+q to server due to authentication.", consts.InfoRequest)
		}
		tests.Passed(t, "Should have successfully delivered message %+q to server due to authentication.", consts.InfoRequest)
	}

	server.Wait()
	// t.Logf("\tWhen connecting to a UDP Server with credentials")
	// {
	//
	// 	client, err := mock.NewUDPClient("udp", ":6060", ":5060")
	// 	if err != nil {
	// 		tests.Failed(t, "Should have successfully created udp client: %+q.", err.Error())
	// 	}
	// 	tests.Passed(t, "Should have successfully created udp client for %q.", ":6060")
	//
	// 	defer client.Close()
	//
	// 	if err := sendMessage(client, nil, string(consts.InfoRequest)); err == nil {
	// 		tests.Failed(t, "Should have successfully delivered message %+q to server: %+q.", consts.InfoRequest, err.Error())
	// 	}
	// 	tests.Passed(t, "Should have successfully delivered message %+q to server.", consts.InfoRequest)
	// }
}

// sendMessage delivers the giving command to the websoket.
func sendMessage(conn *mock.UDPClient, addr *net.UDPAddr, command string, data ...string) error {
	var bu bytes.Buffer
	if err := json.NewEncoder(&bu).Encode(utils.NewCommand(command, data...)); err != nil {
		return err
	}

	return conn.Write(bu.Bytes(), addr)
}
