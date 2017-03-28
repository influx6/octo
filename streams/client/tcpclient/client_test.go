package tcpclient_test

import (
	"testing"

	"github.com/influx6/faux/tests"
	"github.com/influx6/octo"
	"github.com/influx6/octo/clients/goclient/tcpclient"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/instruments"
	"github.com/influx6/octo/mock"
	"github.com/influx6/octo/parsers/byteutils"
	"github.com/influx6/octo/transmission/tcp"
)

func TestClientConnectionWithBadServers(t *testing.T) {
	clientSystem := mock.NewClientSystem(octo.AuthCredential{
		Scheme: "XBot",
		Key:    "api-32",
		Token:  "auth-4531",
		Data:   []byte("BOMTx"),
	})

	inst := instruments.Instruments(mock.NewTestLogger(), nil)

	client := tcpclient.New(inst, tcpclient.Attr{
		Addr: ":7040",
	})

	err := client.Listen(clientSystem, mock.CommandEncoding{})
	if err == nil {
		tests.Failed("Should have successfully failed to connect to tcp server.")
	}
	tests.Passed("Should have successfully failed to connect to tcp server.")
}

func TestClientConnectionWithNoServers(t *testing.T) {
	clientSystem := mock.NewClientSystem(octo.AuthCredential{
		Scheme: "XBot",
		Key:    "api-32",
		Token:  "auth-4531",
		Data:   []byte("BOMTx"),
	})

	inst := instruments.Instruments(mock.NewTestLogger(), nil)

	client := tcpclient.New(inst, tcpclient.Attr{
		Addr: "",
	})

	if err := client.Listen(clientSystem, mock.CommandEncoding{}); err == nil {
		tests.Failed("Should have successfully failed to connect to tcp server.")
	}
	tests.Passed("Should have successfully failed to connect to tcp server.")
}

// TestClientConnectionWithoutAuth validates the behave of the tcp client for
// connecting to tcp servers.
func TestClientConnectionWithoutAuth(t *testing.T) {
	clientSystem := mock.NewClientSystem(octo.AuthCredential{
		Scheme: "XBot",
		Key:    "api-32",
		Token:  "auth-4531",
		Data:   []byte("BOMTx"),
	})

	system := mock.NewServerSystem(octo.AuthCredential{
		Scheme: "XBot",
		Key:    "api-32",
		Token:  "auth-4531",
		Data:   []byte("BOMTx"),
	})

	inst := instruments.Instruments(mock.NewTestLogger(), nil)

	server := tcp.New(inst, tcp.ServerAttr{
		Addr: ":6050",
	})

	if err := server.Listen(system); err != nil {
		tests.Failed("Should have successfully created conenction for tcp server: %+q.", err)
	}
	tests.Passed("Should have successfully created conenction for tcp server.")

	if err := server.Listen(system); err != nil {
		tests.Failed("Should have successfully  failed to recall listen for tcp server.")
	}
	tests.Passed("Should have successfully  failed to recall listen for tcp server.")

	defer server.Close()

	client := tcpclient.New(inst, tcpclient.Attr{
		Addr: ":6050",
	})

	defer client.Close()

	if err := client.Listen(clientSystem, mock.CommandEncoding{}); err != nil {
		tests.Failed("Should have successfully connected to tcp server with client: %+q.", err)
	}
	tests.Passed("Should have successfully connected to tcp server with client.")

}

// TestClientConnectionWithAuth validates the behave of the tcp client for
// connecting to tcp servers.
func TestClientConnectionWithAuth(t *testing.T) {
	clientSystem := mock.NewClientSystem(octo.AuthCredential{
		Scheme: "XBot",
		Key:    "api-32",
		Token:  "auth-4531",
		Data:   []byte("BOMTx"),
	})

	system := mock.NewServerSystem(octo.AuthCredential{
		Scheme: "XBot",
		Key:    "api-32",
		Token:  "auth-4531",
		Data:   []byte("BOMTx"),
	})

	inst := instruments.Instruments(mock.NewTestLogger(), nil)

	server := tcp.New(inst, tcp.ServerAttr{
		Addr:         ":7050",
		Authenticate: true,
	})

	if err := server.Listen(system); err != nil {
		tests.Failed("Should have successfully created conenction for tcp server: %+q.", err)
	}
	tests.Passed("Should have successfully created conenction for tcp server.")

	defer server.Close()

	client := tcpclient.New(inst, tcpclient.Attr{
		Addr:         ":7050",
		Authenticate: true,
	})

	defer client.Close()

	if err := client.Listen(clientSystem, mock.CommandEncoding{}); err != nil {
		tests.Failed("Should have successfully connected to tcp server with client: %+q.", err)
	}
	tests.Passed("Should have successfully connected to tcp server with client.")

	cmdData := byteutils.MakeByteMessage(consts.ContactRequest, nil)
	if err := client.Send(cmdData, true); err != nil {
		tests.Failed("Should have successfully delivered command to server: %+q.", err)
	}
	tests.Passed("Should have successfully delivered command to server.")

	clientSystem.Wait()
}
