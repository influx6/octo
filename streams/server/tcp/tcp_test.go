package tcp_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/influx6/faux/tests"
	"github.com/influx6/octo"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/instruments"
	"github.com/influx6/octo/messages/commando"
	"github.com/influx6/octo/mock"
	"github.com/influx6/octo/streams/server"
	"github.com/influx6/octo/streams/server/tcp"
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
	cmds, err := commando.Parser.Decode(message)
	if err != nil {
		return err
	}

	commands, ok := cmds.([]commando.CommandMessage)
	if !ok {
		return consts.ErrUnservable
	}

	for _, command := range commands {

		switch command.Name {
		// case bytes.Equal(consts.AuthRequest, command.Name):
		case "PUMP":
			return tx.Send([]byte("RUMP"), true)
		case "REX":
			return tx.Send([]byte("DEX"), true)
		case "BULL":
			return tx.SendAll(commando.MakeByteMessage([]byte("PRINT"), command.Data...), true)
		case "BONG":
			return tx.Send([]byte("BING"), true)
		default:
			break
		}

	}

	return errors.New("Invalid Command")
}

// TestServer tests the validity of our server code.
func TestServer(t *testing.T) {
	system := mockSystem{t: t}
	inst := instruments.Instruments(mock.NewTestLogger(), nil)

	server := tcp.New(inst, tcp.ServerAttr{
		Addr: ":6050",
		// ClusterAddr: ":6060",
	})

	server.Listen(system)

	client, err := mock.NewTCPClient(":6050", nil)
	if err != nil {
		tests.Failed("Should have successfully connected to host server ':4050': %s.", err)
	}
	tests.Passed("Should have successfully connected to host server ':4050'.")

	defer server.Close()
	defer client.Close()

	t.Logf("\tWhen 'PONG' is sent to the server")
	{
		if errx := client.Write(commando.WrapResponseBlock([]byte("PING"), nil), true); err != nil {
			tests.Failed("Should have delivered 'PING' message: %s.", errx)
		}
		tests.Passed("Should have delivered 'PING' message.")

		response, errm := client.Read()
		if errm != nil {
			tests.Failed("Should have successfully received response from server: %s.", errm)
		}
		tests.Passed("Should have successfully received response from server.")

		validateResponseHeader(t, response, []byte("PONG"))
	}

	t.Logf("\tWhen 'PING' is sent to the server")
	{
		if errx := client.Write(commando.WrapResponseBlock([]byte("PONG"), nil), true); err != nil {
			tests.Failed("Should have delivered 'PONG' message: %s.", errx)
		}
		tests.Passed("Should have delivered 'PONG' message.")

		response, errm := client.Read()
		if errm != nil {
			tests.Failed("Should have successfully received response from server: %s.", errm)
		}
		tests.Passed("Should have successfully received response from server.")

		validateResponseHeader(t, response, []byte("PING"))
	}

	t.Logf("\tWhen 'INFO' is sent to the server")
	{
		if errx := client.Write(commando.WrapResponseBlock([]byte("INFO"), nil), true); err != nil {
			tests.Failed("Should have delivered 'CLOSE' message: %s.", errx)
		}
		tests.Passed("Should have delivered 'CLOSE' message.")

		response, errm := client.Read()
		if errm != nil {
			tests.Failed("Should have successfully received response from server: %s.", errm)
		}
		tests.Passed("Should have successfully received response from server.")

		validateResponseHeader(t, response, []byte("INFORES"))
	}

	t.Logf("\tWhen 'CLOSED' is sent to the server")
	{
		if errx := client.Write(commando.WrapResponseBlock([]byte("CLOSE"), nil), true); err != nil {
			tests.Failed("Should have delivered 'CLOSE' message: %s.", errx)
		}
		tests.Passed("Should have delivered 'CLOSE' message.")

		response, errm := client.Read()
		if errm != nil {
			tests.Failed("Should have successfully received response from server: %s.", errm)
		}
		tests.Passed("Should have successfully received response from server.")

		validateResponseHeader(t, response, []byte("OK"))
	}
}

// TestClusterServers tests the validity of our server code in connecting and
// relating between clusters with authentication.
func TestClusterServers(t *testing.T) {
	system := mockSystem{t: t}
	inst := instruments.Instruments(mock.NewTestLogger(), nil)

	server := tcp.New(inst, tcp.ServerAttr{
		Addr:         ":6050",
		ClusterAddr:  ":6060",
		Authenticate: true,
		Credential: octo.AuthCredential{
			Scheme: scheme,
			Key:    key,
			Token:  token,
			Data:   tokenData,
		},
	})

	server2 := tcp.New(inst, tcp.ServerAttr{
		Addr:         ":7050",
		ClusterAddr:  ":7060",
		Authenticate: true,
		Credential: octo.AuthCredential{
			Scheme: scheme,
			Key:    key,
			Token:  token,
			Data:   tokenData,
		},
	})

	server.Listen(system)
	server2.Listen(system)

	defer server2.Close()
	defer server.Close()

	if err := server2.RelateWithCluster(":6060"); err != nil {
		tests.Failed("Should have successfully connected with cluster: %s.", err)
	}
	tests.Passed("Should have successfully connected with cluster.")

	clusters := server2.Clusters()
	if len(clusters) == 0 {
		tests.Failed("Should have successfully connected server2 with server1 cluster.")
	}
	tests.Passed("Should have successfully connected server2 with server1 cluster.")

	if len(clusters) == 0 {
		tests.Failed("Should have successfully connected to another cluster.")
	}
	tests.Passed("Should have successfully connected to another cluster.")

	if clusters[0].UUID != server.CLContact().UUID {
		t.Logf("\t\t Received: %#v", clusters[0])
		t.Logf("\t\t Expected: %#v", server.CLContact())
		tests.Failed("Should have successfully added server.UUID cluster to server2 cluster list.")
	}
	tests.Passed("Should have successfully added server.UUID cluster to server2 cluster list.")
}

// TestClusterServerSendAll tests the validity of our server code.
func TestClusterServerSendAll(t *testing.T) {
	system := mockSystem{t: t}
	inst := instruments.Instruments(mock.NewTestLogger(), nil)

	server := tcp.New(inst, tcp.ServerAttr{
		Addr:        ":6050",
		ClusterAddr: ":6060",
	})

	server2 := tcp.New(inst, tcp.ServerAttr{
		Addr:        ":7050",
		ClusterAddr: ":7060",
	})

	server.Listen(system)
	server2.Listen(system)

	if err := server2.RelateWithCluster(":6060"); err != nil {
		tests.Failed("Should have successfully connected with cluster: %s.", err)
	}
	tests.Passed("Should have successfully connected with cluster.")

	defer server2.Close()
	defer server.Close()

	client, err := mock.NewTCPClient(":6050", nil)
	if err != nil {
		tests.Failed("Should have successfully connected to host server ':4050': %s.", err)
	}
	tests.Passed("Should have successfully connected to host server ':4050'.")

	client2, err := mock.NewTCPClient(":7050", nil)
	if err != nil {
		tests.Failed("Should have successfully connected to host server ':4050': %s.", err)
	}
	tests.Passed("Should have successfully connected to host server ':4050'.")

	defer client.Close()
	defer client2.Close()

	t.Logf("\tWhen 'BULL' is mass sent to the server")
	{
		if errx := client.Write(commando.WrapResponseBlock([]byte("BULL"), []byte("LOTTER")), true); err != nil {
			tests.Failed("Should have delivered 'PING' message: %s.", errx)
		}
		tests.Passed("Should have delivered 'PING' message.")

		response, errm := client2.Read()
		if errm != nil {
			tests.Failed("Should have successfully received response from server: %s.", errm)
		}
		tests.Passed("Should have successfully received response from server.")

		validateResponseHeader(t, response, []byte("PRINT"))
		validateResponse(t, response, []byte("LOTTER"))
	}
}

func validateResponseHeader(t *testing.T, data []byte, target []byte) {
	rcm, err := commando.Parser.Decode(data)
	if err != nil {
		tests.Failed("Should have successfully parsed response from server: %s.", err)
	}
	tests.Passed("Should have successfully parsed response from server.")

	receivedMessages, ok := rcm.([]commando.CommandMessage)
	if !ok {
		tests.Failed("Should have successfully parsed response from server: %s.", consts.ErrUnservable)
	}
	tests.Passed("Should have successfully parsed response from server.")

	if len(receivedMessages) < 1 {
		tests.Failed("Should have successfully received atleast 1 response from server: %s.", err)
	}
	tests.Passed("Should have successfully received atleast 1 response from server.")

	if !bytes.Equal([]byte(receivedMessages[0].Name), target) {
		tests.Failed("Should have successfully matched response header as %+q but got %+q.", target, receivedMessages[0].Name)
	}
	tests.Passed("Should have successfully matched response header as %+q.", target)
}

func validateResponse(t *testing.T, data []byte, target []byte) {
	rcm, err := commando.Parser.Decode(data)
	if err != nil {
		tests.Failed("Should have successfully parsed response from server: %s.", err)
	}
	tests.Passed("Should have successfully parsed response from server.")

	receivedMessages, ok := rcm.([]commando.CommandMessage)
	if !ok {
		tests.Failed("Should have successfully parsed response from server: %s.", consts.ErrUnservable)
	}
	tests.Passed("Should have successfully parsed response from server.")

	if len(receivedMessages) < 1 {
		tests.Failed("Should have successfully received atleast 1 response from server: %s.", err)
	}
	tests.Passed("Should have successfully received atleast 1 response from server.")

	if !bytes.Equal(bytes.Join(receivedMessages[0].Data, []byte("")), target) {
		tests.Failed("Should have successfully matched response header as %+q but got %+q.", target, receivedMessages[0].Name)
	}
	tests.Passed("Should have successfully matched response header as %+q.", target)
}
