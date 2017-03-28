package udpclient_test

import (
	"testing"

	"github.com/influx6/faux/tests"
	"github.com/influx6/octo"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/instruments"
	"github.com/influx6/octo/messages/commando"
	"github.com/influx6/octo/mock"
	"github.com/influx6/octo/netutils"
	"github.com/influx6/octo/streams/client/udpclient"
	"github.com/influx6/octo/streams/server/udp"
)

func TestClientConnectionWithUnknownVersion(t *testing.T) {
	addr := netutils.GetAddr(":7050")
	caddr := netutils.GetAddr(":5050")

	inst := instruments.Instruments(mock.NewTestLogger(), nil)

	_, err := udpclient.New(inst, udpclient.Attr{
		MaxDrops:     6,
		MaxReconnets: 5,
		Addr:         addr,
		ClientAddr:   caddr,
		Authenticate: true,
		Version:      udpclient.Version(10),
	})

	if err == nil {
		tests.Failed("Should have successfully connected to udp server.")
	}
	tests.Passed("Should have successfully connected to udp server.")
}

func TestClientConnectionWithIP4(t *testing.T) {
	addr := netutils.GetAddr(":7050")
	caddr := netutils.GetAddr(":5050")

	inst := instruments.Instruments(mock.NewTestLogger(), nil)
	client, err := udpclient.New(inst, udpclient.Attr{
		MaxDrops:     6,
		MaxReconnets: 5,
		Addr:         addr,
		ClientAddr:   caddr,
		Authenticate: true,
		Version:      udpclient.Ver4,
	})

	if err != nil {
		tests.Failed("Should have successfully connected to udp server: %+q.", err)
	}
	tests.Passed("Should have successfully connected to udp server.")

	client.Close()
}

func TestClientConnectionWithBadServers(t *testing.T) {
	addr := netutils.GetAddr(":7050")
	caddr := netutils.GetAddr(":5050")

	clientSystem := mock.NewClientSystem(octo.AuthCredential{
		Scheme: "XBot",
		Key:    "api-32",
		Token:  "auth-4531",
		Data:   "BOMTx",
	})

	inst := instruments.Instruments(mock.NewTestLogger(), nil)
	client, err := udpclient.New(inst, udpclient.Attr{
		MaxDrops:     6,
		MaxReconnets: 5,
		Addr:         addr,
		ClientAddr:   caddr,
		Clusters:     []string{"67.90.43.12:4050", "50.3.1.4:50"},
		Authenticate: true,
		Version:      udpclient.Ver6,
	})

	if err != nil {
		tests.Failed("Should have successfully connected to udp server: %+q.", err)
	}
	tests.Passed("Should have successfully connected to udp server.")

	defer client.Close()

	if err := client.Listen(clientSystem, commando.Parser); err == nil {
		tests.Failed("Should have sucessfully failed to connect to any server.")
	}
	tests.Passed("Should have sucessfully failed to connect to any server.")

	if err := client.Listen(clientSystem, commando.Parser); err == nil {
		tests.Failed("Should have sucessfully failed to connect to any server.")
	}
	tests.Passed("Should have sucessfully failed to connect to any server.")
}

func TestClientConnectionWithIP6(t *testing.T) {
	addr := netutils.GetAddr(":7050")
	caddr := netutils.GetAddr(":5050")

	inst := instruments.Instruments(mock.NewTestLogger(), nil)
	client, err := udpclient.New(inst, udpclient.Attr{
		MaxDrops:     6,
		MaxReconnets: 5,
		Addr:         addr,
		ClientAddr:   caddr,
		Authenticate: true,
		Version:      udpclient.Ver6,
	})

	if err != nil {
		tests.Failed("Should have successfully connected to udp server: %+q.", err)
	}
	tests.Passed("Should have successfully connected to udp server.")

	client.Close()
}

// TestClientConnectionWithInvalidAuth validates the behave of the udp client for
// connecting to udp servers with an invalid credential.
func TestClientConnectionWithInvalidAuth(t *testing.T) {
	addr := netutils.GetAddr(":7050")
	caddr := netutils.GetAddr(":5050")

	clientSystem := mock.NewClientSystem(octo.AuthCredential{
		Scheme: "XBot",
		Key:    "api-56",
		Token:  "auth-4316",
		Data:   "BOMTx",
	})

	system := mock.NewServerSystem(octo.AuthCredential{
		Scheme: "XBot",
		Key:    "api-32",
		Token:  "auth-4531",
		Data:   "BOMTx",
	})

	inst := instruments.Instruments(mock.NewTestLogger(), nil)

	server := udp.New(inst, udp.ServerAttr{
		Version:      udp.Ver0,
		Authenticate: true,
		Addr:         addr,
	})

	if err := server.Listen(system); err != nil {
		tests.Failed("Should have successfully created connection for udp server: %+q.", err)
	}
	tests.Passed("Should have successfully created conenction for udp server.")

	defer server.Close()

	client, err := udpclient.New(inst, udpclient.Attr{
		MaxDrops:     6,
		MaxReconnets: 5,
		Addr:         addr,
		ClientAddr:   caddr,
		Authenticate: true,
		Version:      udpclient.Ver0,
	})

	if err != nil {
		tests.Failed("Should have successfully connected to udp server: %+q.", err)
	}
	tests.Passed("Should have successfully connected to udp server.")

	defer client.Close()

	if err := client.Listen(clientSystem, commando.Parser); err == nil {
		tests.Failed("Should have successfully failed to connect to udp server with invalid credentials: %+q.", err)
	}
	tests.Passed("Should have successfully failed to connect to udp server with invalid credentials.")
}

// TestClientConnectionWithoutAuth validates the behave of the udp client for
// connecting to udp servers requiring authentication without  credential.
func TestClientConnectionWithoutAuth(t *testing.T) {
	addr := netutils.GetAddr(":7050")
	caddr := netutils.GetAddr(":5050")

	clientSystem := mock.NewClientSystem(octo.AuthCredential{
		Scheme: "XBot",
		Key:    "api-32",
		Token:  "auth-4531",
		Data:   "BOMTx",
	})

	system := mock.NewServerSystem(octo.AuthCredential{
		Scheme: "XBot",
		Key:    "api-32",
		Token:  "auth-4531",
		Data:   "BOMTx",
	})

	inst := instruments.Instruments(mock.NewTestLogger(), nil)

	server := udp.New(inst, udp.ServerAttr{
		Version:      udp.Ver0,
		Authenticate: true,
		Addr:         addr,
	})

	if err := server.Listen(system); err != nil {
		tests.Failed("Should have successfully created connection for udp server: %+q.", err)
	}
	tests.Passed("Should have successfully created conenction for udp server.")

	defer server.Close()

	client, err := udpclient.New(inst, udpclient.Attr{
		MaxDrops:     6,
		MaxReconnets: 5,
		Addr:         addr,
		ClientAddr:   caddr,
		Authenticate: true,
		Version:      udpclient.Ver0,
	})

	if err != nil {
		tests.Failed("Should have successfully preapred udp client: %+q.", err)
	}
	tests.Passed("Should have successfully prepared udp client.")

	defer client.Close()

	if err := client.Listen(clientSystem, commando.Parser); err != nil {
		tests.Failed("Should have successfully failed to connect to udp server with client: %+q.", err)
	}
	tests.Passed("Should have successfully failed to connect to udp server with client.")
}

// TestClientConnectionWithAuth validates the behave of the udp client for
// connecting to udp servers requiring authentication with valid credential.
func TestClientConnectionWithAuth(t *testing.T) {
	addr := netutils.GetAddr(":7050")
	caddr := netutils.GetAddr(":5050")

	clientSystem := mock.NewClientSystem(octo.AuthCredential{
		Scheme: "XBot",
		Key:    "api-32",
		Token:  "auth-4531",
		Data:   "BOMTx",
	})

	system := mock.NewServerSystem(octo.AuthCredential{
		Scheme: "XBot",
		Key:    "api-32",
		Token:  "auth-4531",
		Data:   "BOMTx",
	})

	inst := instruments.Instruments(mock.NewTestLogger(), nil)

	server := udp.New(inst, udp.ServerAttr{
		Version:      udp.Ver0,
		Authenticate: true,
		Addr:         addr,
	})

	if err := server.Listen(system); err != nil {
		tests.Failed("Should have successfully created connection for udp server: %+q.", err)
	}
	tests.Passed("Should have successfully created conenction for udp server.")

	defer server.Close()

	client, err := udpclient.New(inst, udpclient.Attr{
		MaxDrops:     6,
		MaxReconnets: 5,
		Addr:         addr,
		ClientAddr:   caddr,
		Authenticate: true,
		Version:      udpclient.Ver0,
	})

	if err != nil {
		tests.Failed("Should have successfully connected to udp server: %+q.", err)
	}
	tests.Passed("Should have successfully connected to udp server.")

	defer client.Close()

	if err := client.Listen(clientSystem, commando.Parser); err != nil {
		tests.Failed("Should have successfully connected to udp server with client: %+q.", err)
	}
	tests.Passed("Should have successfully connected to udp server with client.")

	cmdData := commando.NewCommand(string(consts.ContactRequest))
	if err != nil {
		tests.Failed("Should have successfully created command request: %+q.", err)
	}
	tests.Passed("Should have successfully created command request.")

	if err := client.Send(cmdData, true); err != nil {
		tests.Failed("Should have successfully delivered command to server: %+q.", err)
	}
	tests.Passed("Should have successfully delivered command to server.")

	clientSystem.Wait()
}
