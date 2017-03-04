package tcpclient

import (
	"bufio"
	"errors"
	"net"
	"time"

	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/netutils"
)

// TCPConn defines a interface for a type which connects to
// a tcp endpoint and provides read and write capabilities.
type TCPConn struct {
	net.Conn
	writer *bufio.Writer
	reader *bufio.Reader
}

// New returns a new instance of a TCPConn.
func New(addr string) (*TCPConn, error) {
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

	return &TCPConn{
		Conn:   conn,
		writer: bufio.NewWriter(conn),
		reader: bufio.NewReader(conn),
	}, nil
}

// Write writes the current available data from the pipeline.
func (t *TCPConn) Write(data []byte, flush bool) error {
	if t.Conn == nil {
		return ErrClosedConnection
	}

	var deadline bool

	if t.writer.Available() < len(data) {
		t.Conn.SetWriteDeadline(time.Now().Add(1 * time.Second))
		deadline = true
	}

	_, err := t.writer.Write(data)
	if err == nil && flush {
		err = t.writer.Flush()
	}

	if deadline {
		t.Conn.SetWriteDeadline(time.Time{})
	}

	return err
}

// Close ends and disposes of the internal connection, closing it and
// all reads and writers.
func (t *TCPConn) Close() error {
	if t.Conn == nil {
		return nil
	}

	err := t.Conn.Close()
	t.Conn = nil
	t.writer = nil
	t.reader = nil
	return err
}

// ErrClosedConnection is returned when the giving client connection
// has being closed.
var ErrClosedConnection = errors.New("Connection Closed")

// Read reads the current available data from the pipeline.
func (t *TCPConn) Read() ([]byte, error) {
	if t.Conn == nil {
		return nil, ErrClosedConnection
	}

	block := make([]byte, 512)

	n, err := t.reader.Read(block)
	if err != nil {
		return nil, err
	}

	return block[:n], nil
}
