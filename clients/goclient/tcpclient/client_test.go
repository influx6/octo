package tcpclient_test

import (
	"testing"

	"github.com/influx6/octo"
	"github.com/influx6/octo/clients/goclient/tcpclient"
	"github.com/influx6/octo/instruments"
	"github.com/influx6/octo/mock"
	"github.com/influx6/octo/tests"
	"github.com/influx6/octo/transmission/tcp"
)

// TestClientConnection validates the behave of the tcp client for connecting to
// tcp servers.
func TestClientConnection(t *testing.T) {
	system := mock.NewSystem(t, octo.AuthCredential{
		Scheme: "XBot",
		Key:    "api-32",
		Token:  "auth-4531",
		Data:   []byte("BOMTx"),
	})

	inst := instruments.Instrument(instruments.InstrumentAttr{Log: mock.NewLogger(t)})

	server := tcp.New(inst, tcp.ServerAttr{
		Addr: ":6050",
	})

	if err := server.Listen(system); err != nil {
		tests.Failed(t, "Should have successfully created conenction for tcp server: %+q.", err)
	}
	tests.Passed(t, "Should have successfully created conenction for tcp server.")

	client, err := tcpclient.New(inst, tcpclient.Attr{
		Addr: "6050",
	})

	if err != nil {
		tests.Failed(t, "Should have successfully connected to tcp server: %+q.", err)
	}
	tests.Passed(t, "Should have successfully connected to tcp server.")

	_ = client
}
