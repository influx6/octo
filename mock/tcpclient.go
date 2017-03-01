package mock

import (
	"bufio"
	"errors"
	"net"
	"time"

	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/netutils"
)

// TCPClient defines a interface for a type which connects to
// a tcp endpoint and provides read and write capabilities.
type TCPClient struct {
	conn   net.Conn
	writer *bufio.Writer
	reader *bufio.Reader
}

// NewTCPClient returns a new instance of a TCPClient.
func NewTCPClient(addr string) (*TCPClient, error) {
	ip, port, _ := net.SplitHostPort(addr)
	if ip == "" || ip == consts.AnyIP {
		if realIP, err := netutils.GetMainIP(); err == nil {
			addr = net.JoinHostPort(realIP, port)
		}
	}

	conn, err := net.DialTimeout("tcp", addr, consts.MaxWaitTime)
	if err != nil {
		return nil, err
	}

	return &TCPClient{
		conn:   conn,
		writer: bufio.NewWriter(conn),
		reader: bufio.NewReader(conn),
	}, nil
}

// Write writes the current available data from the pipeline.
func (t *TCPClient) Write(data []byte, flush bool) error {
	if t.conn == nil {
		return ErrClosedConnection
	}

	var deadline bool

	if t.writer.Available() < len(data) {
		t.conn.SetWriteDeadline(time.Now().Add(1 * time.Second))
		deadline = true
	}

	_, err := t.writer.Write(data)
	if err == nil && flush {
		err = t.writer.Flush()
	}

	if deadline {
		t.conn.SetWriteDeadline(time.Time{})
	}

	return err
}

// Close ends and disposes of the internal connection, closing it and
// all reads and writers.
func (t *TCPClient) Close() error {
	if t.conn == nil {
		return ErrClosedConnection
	}

	err := t.conn.Close()
	t.conn = nil
	t.writer = nil
	t.reader = nil
	return err
}

// ErrClosedConnection is returned when the giving client connection
// has being closed.
var ErrClosedConnection = errors.New("Connection Closed")

// Read reads the current available data from the pipeline.
func (t *TCPClient) Read() ([]byte, error) {
	if t.conn == nil {
		return nil, ErrClosedConnection
	}

	t.conn.SetReadDeadline(time.Now().Add(7 * time.Second))

	block := make([]byte, 6085)

	n, err := t.reader.Read(block)
	if err != nil {
		t.conn.SetReadDeadline(time.Time{})
		return nil, err
	}

	t.conn.SetReadDeadline(time.Time{})

	return block[:n], nil
}
