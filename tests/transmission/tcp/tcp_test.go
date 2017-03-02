package tcp_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/influx6/octo"
	"github.com/influx6/octo/mock"
	"github.com/influx6/octo/parsers/blockparser"
	"github.com/influx6/octo/parsers/byteutils"
	"github.com/influx6/octo/tests"
	"github.com/influx6/octo/transmission/tcp"
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
	cmds, err := blockparser.Blocks.Parse(message)
	if err != nil {
		return err
	}

	for _, command := range cmds {

		switch {
		// case bytes.Equal(consts.AuthRequest, command.Name):
		case bytes.Equal([]byte("PUMP"), command.Name):
			return tx.Send([]byte("RUMP"), true)
		case bytes.Equal([]byte("REX"), command.Name):
			return tx.Send([]byte("DEX"), true)
		case bytes.Equal([]byte("BULL"), command.Name):
			return tx.SendAll(byteutils.MakeByteMessage([]byte("PRINT"), command.Data...), true)
		case bytes.Equal([]byte("BONG"), command.Name):
			return tx.Send([]byte("BING"), true)
		default:
			break
		}

	}

	return errors.New("Invalid Command")
}

// TestServer tests the validity of our server code.
func TestServer(t *testing.T) {
	log := mock.NewLogger(t)
	system := mockSystem{t: t}

	server := tcp.New(log, tcp.ServerAttr{
		Addr: ":6050",
		// ClusterAddr: ":6060",
	})

	server.Listen(system)

	client, err := mock.NewTCPClient(":6050")
	if err != nil {
		tests.Failed(t, "Should have successfully connected to host server ':4050': %s.", err)
	}
	tests.Passed(t, "Should have successfully connected to host server ':4050'.")

	defer server.Close()
	defer client.Close()

	t.Logf("\tWhen 'PONG' is sent to the server")
	{
		if errx := client.Write(byteutils.WrapResponseBlock([]byte("PING"), nil), true); err != nil {
			tests.Failed(t, "Should have delivered 'PING' message: %s.", errx)
		}
		tests.Passed(t, "Should have delivered 'PING' message.")

		response, errm := client.Read()
		if errm != nil {
			tests.Failed(t, "Should have successfully received response from server: %s.", errm)
		}
		tests.Passed(t, "Should have successfully received response from server.")

		validateResponseHeader(t, response, []byte("PONG"))
	}

	t.Logf("\tWhen 'PING' is sent to the server")
	{
		if errx := client.Write(byteutils.WrapResponseBlock([]byte("PONG"), nil), true); err != nil {
			tests.Failed(t, "Should have delivered 'PONG' message: %s.", errx)
		}
		tests.Passed(t, "Should have delivered 'PONG' message.")

		response, errm := client.Read()
		if errm != nil {
			tests.Failed(t, "Should have successfully received response from server: %s.", errm)
		}
		tests.Passed(t, "Should have successfully received response from server.")

		validateResponseHeader(t, response, []byte("PING"))
	}

	t.Logf("\tWhen 'INFO' is sent to the server")
	{
		if errx := client.Write(byteutils.WrapResponseBlock([]byte("INFO"), nil), true); err != nil {
			tests.Failed(t, "Should have delivered 'CLOSE' message: %s.", errx)
		}
		tests.Passed(t, "Should have delivered 'CLOSE' message.")

		response, errm := client.Read()
		if errm != nil {
			tests.Failed(t, "Should have successfully received response from server: %s.", errm)
		}
		tests.Passed(t, "Should have successfully received response from server.")

		validateResponseHeader(t, response, []byte("INFORES"))
	}

	t.Logf("\tWhen 'CLOSED' is sent to the server")
	{
		if errx := client.Write(byteutils.WrapResponseBlock([]byte("CLOSE"), nil), true); err != nil {
			tests.Failed(t, "Should have delivered 'CLOSE' message: %s.", errx)
		}
		tests.Passed(t, "Should have delivered 'CLOSE' message.")

		response, errm := client.Read()
		if errm != nil {
			tests.Failed(t, "Should have successfully received response from server: %s.", errm)
		}
		tests.Passed(t, "Should have successfully received response from server.")

		validateResponseHeader(t, response, []byte("OK"))
	}
}

// TestClusterServers tests the validity of our server code in connecting and
// relating between clusters with authentication.
func TestClusterServers(t *testing.T) {
	log := mock.NewLogger(t)
	system := mockSystem{t: t}

	server := tcp.New(log, tcp.ServerAttr{
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

	server2 := tcp.New(log, tcp.ServerAttr{
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
		tests.Failed(t, "Should have successfully connected with cluster: %s.", err)
	}
	tests.Passed(t, "Should have successfully connected with cluster.")

	clusters := server2.Clusters()
	if len(clusters) == 0 {
		tests.Failed(t, "Should have successfully connected server2 with server1 cluster.")
	}
	tests.Passed(t, "Should have successfully connected server2 with server1 cluster.")

	if clusters[0].UUID != server.CInfo().UUID {
		t.Logf("\t\t Received: %#v", clusters[0])
		t.Logf("\t\t Expected: %#v", server.CInfo())
		tests.Failed(t, "Should have successfully added server.UUID cluster to server2 cluster list.")
	}
	tests.Passed(t, "Should have successfully added server.UUID cluster to server2 cluster list.")
}

// TestClusterServerSendAll tests the validity of our server code.
func TestClusterServerSendAll(t *testing.T) {
	log := mock.NewLogger(t)
	system := mockSystem{t: t}

	server := tcp.New(log, tcp.ServerAttr{
		Addr:        ":6050",
		ClusterAddr: ":6060",
	})

	server2 := tcp.New(log, tcp.ServerAttr{
		Addr:        ":7050",
		ClusterAddr: ":7060",
	})

	server.Listen(system)
	server2.Listen(system)

	if err := server2.RelateWithCluster(":6060"); err != nil {
		tests.Failed(t, "Should have successfully connected with cluster: %s.", err)
	}
	tests.Passed(t, "Should have successfully connected with cluster.")

	defer server2.Close()
	defer server.Close()

	client, err := mock.NewTCPClient(":6050")
	if err != nil {
		tests.Failed(t, "Should have successfully connected to host server ':4050': %s.", err)
	}
	tests.Passed(t, "Should have successfully connected to host server ':4050'.")

	client2, err := mock.NewTCPClient(":7050")
	if err != nil {
		tests.Failed(t, "Should have successfully connected to host server ':4050': %s.", err)
	}
	tests.Passed(t, "Should have successfully connected to host server ':4050'.")

	defer client.Close()
	defer client2.Close()

	t.Logf("\tWhen 'BULL' is mass sent to the server")
	{
		if errx := client.Write(byteutils.WrapResponseBlock([]byte("BULL"), []byte("LOTTER")), true); err != nil {
			tests.Failed(t, "Should have delivered 'PING' message: %s.", errx)
		}
		tests.Passed(t, "Should have delivered 'PING' message.")

		response, errm := client2.Read()
		if errm != nil {
			tests.Failed(t, "Should have successfully received response from server: %s.", errm)
		}
		tests.Passed(t, "Should have successfully received response from server.")

		validateResponseHeader(t, response, []byte("PRINT"))
		validateResponse(t, response, []byte("LOTTER"))
	}
}

func validateResponseHeader(t *testing.T, data []byte, target []byte) {
	receivedMessages, err := blockparser.Blocks.Parse(data)
	if err != nil {
		tests.Failed(t, "Should have successfully parsed response from server: %s.", err)
	}
	tests.Passed(t, "Should have successfully parsed response from server.")

	if len(receivedMessages) < 1 {
		tests.Failed(t, "Should have successfully received atleast 1 response from server: %s.", err)
	}
	tests.Passed(t, "Should have successfully received atleast 1 response from server.")

	if !bytes.Equal(receivedMessages[0].Name, target) {
		tests.Failed(t, "Should have successfully matched response header as %+q but got %+q.", target, receivedMessages[0].Name)
	}
	tests.Passed(t, "Should have successfully matched response header as %+q.", target)
}

func validateResponse(t *testing.T, data []byte, target []byte) {
	receivedMessages, err := blockparser.Blocks.Parse(data)
	if err != nil {
		tests.Failed(t, "Should have successfully parsed response from server: %s.", err)
	}
	tests.Passed(t, "Should have successfully parsed response from server.")

	if len(receivedMessages) < 1 {
		tests.Failed(t, "Should have successfully received atleast 1 response from server: %s.", err)
	}
	tests.Passed(t, "Should have successfully received atleast 1 response from server.")

	if !bytes.Equal(bytes.Join(receivedMessages[0].Data, []byte("")), target) {
		tests.Failed(t, "Should have successfully matched response header as %+q but got %+q.", target, receivedMessages[0].Name)
	}
	tests.Passed(t, "Should have successfully matched response header as %+q.", target)
}
