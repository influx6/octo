package mock

import (
	"net"
	"time"

	"github.com/influx6/octo/netutils"
)

// UDPClient defines a interface for a type which connects to
// a UDP endpoint and provides read and write capabilities.
type UDPClient struct {
	conn *net.UDPConn
}

// NewUDPClient returns a new instance of a UDPClient.
func NewUDPClient(version string, userAddr string, serverAddr string) (*UDPClient, error) {
	udpServerAddr, err := net.ResolveUDPAddr(version, netutils.GetAddr(serverAddr))
	if err != nil {
		return nil, err
	}

	udpLocalAddr, err := net.ResolveUDPAddr(version, netutils.GetAddr(userAddr))
	if err != nil {
		return nil, err
	}

	var conn *net.UDPConn

	conn, err = net.DialUDP(version, udpLocalAddr, udpServerAddr)
	if err != nil {
		return nil, err
	}

	return &UDPClient{
		conn: conn,
	}, nil
}

// Read reads the current available data from the pipeline.
func (t *UDPClient) Read() ([]byte, *net.UDPAddr, error) {
	if t.conn == nil {
		return nil, nil, ErrClosedConnection
	}

	t.conn.SetReadDeadline(time.Now().Add(7 * time.Second))

	block := make([]byte, 6085)

	n, addr, err := t.conn.ReadFromUDP(block)
	if err != nil {
		return nil, nil, err
	}

	return block[:n], addr, nil
}

// Write writes the current available data from the pipeline.
func (t *UDPClient) Write(data []byte, addr *net.UDPAddr) error {
	if t.conn == nil {
		return ErrClosedConnection
	}

	t.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))

	if addr == nil {
		_, err := t.conn.Write(data)
		t.conn.SetWriteDeadline(time.Time{})
		return err
	}

	_, err := t.conn.WriteToUDP(data, addr)
	t.conn.SetWriteDeadline(time.Time{})

	return err
}

// Close ends and disposes of the internal connection, closing it and
// all reads and writers.
func (t *UDPClient) Close() error {
	if t.conn == nil {
		return ErrClosedConnection
	}

	err := t.conn.Close()
	t.conn = nil
	return err
}
