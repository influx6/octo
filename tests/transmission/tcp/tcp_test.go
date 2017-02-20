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

			return tx.Send([]byte("OK"), true)
		},
		"PONG": func(m utils.Message, tx octo.Transmission) error {
			return tx.Send([]byte("PING"), true)
		},
		"PING": func(m utils.Message, tx octo.Transmission) error {
			return tx.Send([]byte("PONG"), true)
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
		Addr: ":4050",
		// ClusterAddr: ":6060",
	})

	server.Listen(system)

	client, err := mock.NewTCPClient(":4050")
	if err != nil {
		tests.Failed(t, "Should have successfully connected to host server ':4050': %s.", err)
	}
	tests.Passed(t, "Should have successfully connected to host server ':4050'.")

	defer client.Close()

	if errx := client.Write(utils.WrapResponseBlock([]byte("PING"), nil), true); err != nil {
		tests.Failed(t, "Should have delivered 'PING' message: %s.", errx)
	}
	tests.Passed(t, "Should have delivered 'PING' message.")

	response, err := client.Read()
	if err != nil {
		tests.Failed(t, "Should have successfully received response from server: %s.", err)
	}
	tests.Passed(t, "Should have successfully received response from server.")

	validateResponseHeader(t, response, []byte("PONG"))

	if errx := client.Write(utils.WrapResponseBlock([]byte("PONG"), nil), true); err != nil {
		tests.Failed(t, "Should have delivered 'PONG' message: %s.", errx)
	}
	tests.Passed(t, "Should have delivered 'PONG' message.")

	response, err = client.Read()
	if err != nil {
		tests.Failed(t, "Should have successfully received response from server: %s.", err)
	}
	tests.Passed(t, "Should have successfully received response from server.")

	validateResponseHeader(t, response, []byte("PING"))

	if errx := client.Write(utils.WrapResponseBlock([]byte("INFO"), nil), true); err != nil {
		tests.Failed(t, "Should have delivered 'CLOSE' message: %s.", errx)
	}
	tests.Passed(t, "Should have delivered 'CLOSE' message.")

	response, err = client.Read()
	if err != nil {
		tests.Failed(t, "Should have successfully received response from server: %s.", err)
	}
	tests.Passed(t, "Should have successfully received response from server.")

	validateResponseHeader(t, response, []byte("INFORES"))

	if errx := client.Write(utils.WrapResponseBlock([]byte("CLOSE"), nil), true); err != nil {
		tests.Failed(t, "Should have delivered 'CLOSE' message: %s.", errx)
	}
	tests.Passed(t, "Should have delivered 'CLOSE' message.")

	response, err = client.Read()
	if err != nil {
		tests.Failed(t, "Should have successfully received response from server: %s.", err)
	}
	tests.Passed(t, "Should have successfully received response from server.")

	validateResponseHeader(t, response, []byte("OK"))
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

	if bytes.Equal(receivedMessages[0].Command, target) {
		tests.Failed(t, "Should have successfully matched response header as %+q.", target)
	}
	tests.Passed(t, "Should have successfully matched response header as %+q.", target)
}
