package tcp_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/influx6/faux/utils"
	"github.com/influx6/octo"
	"github.com/influx6/octo/mock"
	"github.com/influx6/octo/tests"
	"github.com/influx6/octo/transmission/tcp"
)

// TestServer tests the validity of our server code.
func TestServer(t *testing.T) {
	log := mock.NewLogger(t)
	system := mock.NewMSystem(map[string]mock.MessageHandler{
		"CLOSE": func(m utils.Message, tx octo.Transmission) error {
			defer tx.Close()

			return tx.Send(utils.WrapResponseBlock([]byte("OK"), nil), true)
		},
		"PONG": func(m utils.Message, tx octo.Transmission) error {
			return tx.Send(utils.WrapResponseBlock([]byte("PING"), nil), true)
		},
		"PING": func(m utils.Message, tx octo.Transmission) error {
			return tx.Send(utils.WrapResponseBlock([]byte("PONG"), nil), true)
		},
		"INFO": func(m utils.Message, tx octo.Transmission) error {
			_, serverInfo := tx.Info()

			infx, err := json.Marshal(serverInfo)
			if err != nil {
				return err
			}

			return tx.Send(utils.WrapResponseBlock([]byte("INFORES"), infx), true)
		},
	})

	server := tcp.NewServer(log, tcp.ServerAttr{
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
		if errx := client.Write(utils.WrapResponseBlock([]byte("PING"), nil), true); err != nil {
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
		if errx := client.Write(utils.WrapResponseBlock([]byte("PONG"), nil), true); err != nil {
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
		if errx := client.Write(utils.WrapResponseBlock([]byte("INFO"), nil), true); err != nil {
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
		if errx := client.Write(utils.WrapResponseBlock([]byte("CLOSE"), nil), true); err != nil {
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
// relating between clusters.
func TestClustereServers(t *testing.T) {
	log := mock.NewLogger(t)

	system := mock.NewMSystem(map[string]mock.MessageHandler{
		"CLOSE": func(m utils.Message, tx octo.Transmission) error {
			defer tx.Close()

			return tx.Send(utils.WrapResponseBlock([]byte("OK"), nil), true)
		},
		"PONG": func(m utils.Message, tx octo.Transmission) error {
			return tx.Send(utils.WrapResponseBlock([]byte("PING"), nil), true)
		},
		"PING": func(m utils.Message, tx octo.Transmission) error {
			return tx.Send(utils.WrapResponseBlock([]byte("PONG"), nil), true)
		},
		"Auth": func(m utils.Message, tx octo.Transmission) error {
			return tx.Send(utils.WrapResponseBlock([]byte("{}"), nil), true)
		},
		"INFO": func(m utils.Message, tx octo.Transmission) error {
			_, serverInfo := tx.Info()

			infx, err := json.Marshal(serverInfo)
			if err != nil {
				return err
			}

			return tx.Send(utils.WrapResponseBlock([]byte("INFORES"), infx), true)
		},
	})

	server := tcp.NewServer(log, tcp.ServerAttr{
		Addr:        ":6050",
		ClusterAddr: ":6060",
	})

	server2 := tcp.NewServer(log, tcp.ServerAttr{
		Addr:        ":7050",
		ClusterAddr: ":7060",
	})

	server.Listen(system)
	server2.Listen(system)

	if err := server2.RelateWithCluster(":6060"); err != nil {
		tests.Failed(t, "Should have successfully connected with cluster: %s.", err)
	}
	tests.Passed(t, "Should have successfully connected with cluster.")

	server.Wait()
}

func validateResponseHeader(t *testing.T, data []byte, target []byte) {
	receivedMessages, err := utils.BlockParser.Parse(data)
	if err != nil {
		tests.Failed(t, "Should have successfully parsed response from server: %s.", err)
	}
	tests.Passed(t, "Should have successfully parsed response from server.")

	if len(receivedMessages) < 1 {
		tests.Failed(t, "Should have successfully received atleast 1 response from server: %s.", err)
	}
	tests.Passed(t, "Should have successfully received atleast 1 response from server.")

	if !bytes.Equal(receivedMessages[0].Command, target) {
		tests.Failed(t, "Should have successfully matched response header as %+q but got %+q.", target, receivedMessages[0].Command)
	}
	tests.Passed(t, "Should have successfully matched response header as %+q.", target)
}
