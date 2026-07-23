package tf_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net"
	"testing"
	"time"

	tf "github.com/singlink/singlink/common/tlsfragment"

	"github.com/stretchr/testify/require"
)

func TestTLSFragment(t *testing.T) {
	t.Parallel()
	tcpConn, err := net.Dial("tcp", "1.1.1.1:443")
	require.NoError(t, err)
	tlsConn := tls.Client(tf.NewConn(tcpConn, context.Background(), true, false, 0), &tls.Config{
		ServerName: "www.cloudflare.com",
	})
	require.NoError(t, tlsConn.Handshake())
}

func TestTLSRecordFragment(t *testing.T) {
	t.Parallel()
	tcpConn, err := net.Dial("tcp", "1.1.1.1:443")
	require.NoError(t, err)
	tlsConn := tls.Client(tf.NewConn(tcpConn, context.Background(), false, true, 0), &tls.Config{
		ServerName: "www.cloudflare.com",
	})
	require.NoError(t, tlsConn.Handshake())
}

func TestTLS2Fragment(t *testing.T) {
	t.Parallel()
	tcpConn, err := net.Dial("tcp", "1.1.1.1:443")
	require.NoError(t, err)
	tlsConn := tls.Client(tf.NewConn(tcpConn, context.Background(), true, true, 0), &tls.Config{
		ServerName: "www.cloudflare.com",
	})
	require.NoError(t, tlsConn.Handshake())
}

func TestTLSFragmentWriteHandlesEmptyServerNameLabel(t *testing.T) {
	captureConn := new(bufferConn)
	tlsConn := tls.Client(captureConn, &tls.Config{
		ServerName:         "x.github.com",
		InsecureSkipVerify: true,
	})
	_ = tlsConn.Handshake()
	payload := append([]byte(nil), captureConn.Bytes()...)
	serverName := tf.IndexTLSServerName(payload)
	require.NotNil(t, serverName)
	payload[serverName.Index] = '.'

	writeConn := new(bufferConn)
	fragmentConn := tf.NewConn(writeConn, context.Background(), true, false, 0)
	n, err := fragmentConn.Write(payload)
	require.NoError(t, err)
	require.Equal(t, len(payload), n)
	require.True(t, bytes.Equal(payload, writeConn.Bytes()))
}

type bufferConn struct {
	bytes.Buffer
}

func (c *bufferConn) Read(p []byte) (int, error) {
	return 0, io.EOF
}

func (c *bufferConn) Close() error {
	return nil
}

func (c *bufferConn) LocalAddr() net.Addr {
	return nil
}

func (c *bufferConn) RemoteAddr() net.Addr {
	return nil
}

func (c *bufferConn) SetDeadline(t time.Time) error {
	return nil
}

func (c *bufferConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (c *bufferConn) SetWriteDeadline(t time.Time) error {
	return nil
}
