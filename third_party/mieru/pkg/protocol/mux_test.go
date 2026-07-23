// Copyright (C) 2023  mieru authors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package protocol

import (
	"bytes"
	"context"
	"errors"
	"io"
	mrand "math/rand"
	"net"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/cipher"
	"github.com/enfein/mieru/v3/pkg/common"
	"github.com/enfein/mieru/v3/pkg/log"
	"github.com/enfein/mieru/v3/pkg/testtool"
	"google.golang.org/protobuf/proto"
)

var users = map[string]*appctlpb.User{
	"xiaochitang": {
		Name:     proto.String("xiaochitang"),
		Password: proto.String("kuiranbudong"),
	},
}

func runClient(t *testing.T, properties UnderlayProperties, username, password []byte, concurrent int) {
	clientMux := NewMux(true).
		SetClientUserNamePassword(string(username), cipher.HashPassword(password, username)).
		SetClientMultiplexFactor(2).
		SetEndpoints([]UnderlayProperties{properties})

	dialCtx, cancelFunc := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelFunc()

	var wg sync.WaitGroup
	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, err := clientMux.DialContext(dialCtx)
			if err != nil {
				t.Errorf("DialContext() failed: %v", err)
				return
			}
			defer conn.Close()
			for i := 0; i < 100; i++ {
				payloadSize := mrand.Intn(maxPDU) + 1
				payload := testtool.TestHelperGenRot13Input(payloadSize)
				if _, err := conn.Write(payload); err != nil {
					t.Errorf("Write() failed: %v", err)
				}
				resp := make([]byte, payloadSize)
				if _, err := io.ReadFull(conn, resp); err != nil {
					t.Errorf("io.ReadFull() failed: %v", err)
				}
				rot13, err := testtool.TestHelperRot13(resp)
				if err != nil {
					t.Errorf("TestHelperRot13() failed: %v", err)
				}
				if !bytes.Equal(payload, rot13) {
					t.Errorf("Received unexpected response")
				}
			}
			sessionInfoList := clientMux.ExportSessionInfoList()
			if len(sessionInfoList.Items) == 0 {
				t.Errorf("connection is not shown in the session info list")
			}
		}()
	}
	wg.Wait()

	if err := clientMux.Close(); err != nil {
		t.Errorf("Close client mux failed: %v", err)
	}
}

func TestIPv4TCPUnderlay(t *testing.T) {
	log.SetOutputToTest(t)
	log.SetLevel("DEBUG")
	port, err := common.UnusedTCPPort()
	if err != nil {
		t.Fatalf("common.UnusedTCPPort() failed: %v", err)
	}
	serverProperties := NewUnderlayProperties(1400, common.StreamTransport, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: port}, nil)
	serverMux := NewMux(false).
		SetServerUsers(users).
		SetEndpoints([]UnderlayProperties{serverProperties})
	testServer := testtool.NewTestHelperServer()

	if err := serverMux.Start(); err != nil {
		t.Fatalf("[%s] Start() failed: %v", time.Now().Format(testtool.TimeLayout), err)
	}
	time.Sleep(100 * time.Millisecond)
	go func() {
		if err := testServer.Serve(serverMux); err != nil {
			t.Errorf("[%s] Serve() failed: %v", time.Now().Format(testtool.TimeLayout), err)
		}
	}()
	defer testServer.Close()
	time.Sleep(100 * time.Millisecond)

	clientProperties := NewUnderlayProperties(1400, common.StreamTransport, nil, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: port})
	runClient(t, clientProperties, []byte("xiaochitang"), []byte("kuiranbudong"), 4)
	if err := serverMux.Close(); err != nil {
		t.Errorf("Server mux close failed: %v", err)
	}
}

func TestIPv6TCPUnderlay(t *testing.T) {
	log.SetOutputToTest(t)
	log.SetLevel("DEBUG")
	port, err := common.UnusedTCPPort()
	if err != nil {
		t.Fatalf("common.UnusedTCPPort() failed: %v", err)
	}
	serverProperties := NewUnderlayProperties(1400, common.StreamTransport, &net.TCPAddr{IP: net.ParseIP("::1"), Port: port}, nil)
	serverMux := NewMux(false).
		SetServerUsers(users).
		SetEndpoints([]UnderlayProperties{serverProperties})
	testServer := testtool.NewTestHelperServer()

	if err := serverMux.Start(); err != nil {
		t.Fatalf("[%s] Start() failed: %v", time.Now().Format(testtool.TimeLayout), err)
	}
	time.Sleep(100 * time.Millisecond)
	go func() {
		if err := testServer.Serve(serverMux); err != nil {
			t.Errorf("[%s] Serve() failed: %v", time.Now().Format(testtool.TimeLayout), err)
		}
	}()
	defer testServer.Close()
	time.Sleep(100 * time.Millisecond)

	clientProperties := NewUnderlayProperties(1400, common.StreamTransport, nil, &net.TCPAddr{IP: net.ParseIP("::1"), Port: port})
	runClient(t, clientProperties, []byte("xiaochitang"), []byte("kuiranbudong"), 4)
	if err := serverMux.Close(); err != nil {
		t.Errorf("Server mux close failed: %v", err)
	}
}

func TestIPv4UDPUnderlay(t *testing.T) {
	log.SetOutputToTest(t)
	log.SetLevel("DEBUG")
	port, err := common.UnusedUDPPort()
	if err != nil {
		t.Fatalf("common.UnusedUDPPort() failed: %v", err)
	}
	serverProperties := NewUnderlayProperties(1400, common.PacketTransport, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: port}, nil)
	serverMux := NewMux(false).
		SetServerUsers(users).
		SetEndpoints([]UnderlayProperties{serverProperties})
	testServer := testtool.NewTestHelperServer()

	if err := serverMux.Start(); err != nil {
		t.Fatalf("[%s] Start() failed: %v", time.Now().Format(testtool.TimeLayout), err)
	}
	time.Sleep(100 * time.Millisecond)
	go func() {
		if err := testServer.Serve(serverMux); err != nil {
			t.Errorf("[%s] Serve() failed: %v", time.Now().Format(testtool.TimeLayout), err)
		}
	}()
	defer testServer.Close()
	time.Sleep(100 * time.Millisecond)

	clientProperties := NewUnderlayProperties(1400, common.PacketTransport, nil, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: port})
	runClient(t, clientProperties, []byte("xiaochitang"), []byte("kuiranbudong"), 4)
	if err := serverMux.Close(); err != nil {
		t.Errorf("Server mux close failed: %v", err)
	}
}

func TestIPv6UDPUnderlay(t *testing.T) {
	log.SetOutputToTest(t)
	log.SetLevel("DEBUG")
	port, err := common.UnusedUDPPort()
	if err != nil {
		t.Fatalf("common.UnusedUDPPort() failed: %v", err)
	}
	serverProperties := NewUnderlayProperties(1400, common.PacketTransport, &net.UDPAddr{IP: net.ParseIP("::1"), Port: port}, nil)
	serverMux := NewMux(false).
		SetServerUsers(users).
		SetEndpoints([]UnderlayProperties{serverProperties})
	testServer := testtool.NewTestHelperServer()

	if err := serverMux.Start(); err != nil {
		t.Fatalf("[%s] Start() failed: %v", time.Now().Format(testtool.TimeLayout), err)
	}
	time.Sleep(100 * time.Millisecond)
	go func() {
		if err := testServer.Serve(serverMux); err != nil {
			t.Errorf("[%s] Serve() failed: %v", time.Now().Format(testtool.TimeLayout), err)
		}
	}()
	defer testServer.Close()
	time.Sleep(100 * time.Millisecond)

	clientProperties := NewUnderlayProperties(1400, common.PacketTransport, nil, &net.UDPAddr{IP: net.ParseIP("::1"), Port: port})
	runClient(t, clientProperties, []byte("xiaochitang"), []byte("kuiranbudong"), 4)
	if err := serverMux.Close(); err != nil {
		t.Errorf("Server mux close failed: %v", err)
	}
}

func TestStartReturnsErrorAndClosesStartedListeners(t *testing.T) {
	listener := newBlockingListener()
	listenErr := errors.New("listen failed")
	streamListenerFactory := &scriptedStreamListenerFactory{
		listeners: []net.Listener{listener},
		err:       listenErr,
	}
	serverMux := NewMux(false).
		SetServerUsers(users).
		SetStreamListenerFactory(streamListenerFactory).
		SetEndpoints([]UnderlayProperties{
			NewUnderlayProperties(1400, common.StreamTransport, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 10001}, nil),
			NewUnderlayProperties(1400, common.StreamTransport, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 10002}, nil),
		})

	err := serverMux.Start()
	if err == nil || !strings.Contains(err.Error(), listenErr.Error()) {
		t.Fatalf("Start() error = %v, want %v", err, listenErr)
	}
	select {
	case <-listener.closed:
	case <-time.After(time.Second):
		t.Fatal("started listener was not closed after Start() failure")
	}
}

func TestSetEndpointsRollsBackStartedListenersOnFailure(t *testing.T) {
	initialListener := newBlockingListener()
	addedListener := newBlockingListener()
	listenErr := errors.New("listen failed")
	streamListenerFactory := &scriptedStreamListenerFactory{
		listeners: []net.Listener{initialListener, addedListener},
		err:       listenErr,
	}
	initialEndpoint := NewUnderlayProperties(1400, common.StreamTransport, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 10001}, nil)
	serverMux := NewMux(false).
		SetServerUsers(users).
		SetStreamListenerFactory(streamListenerFactory).
		SetEndpoints([]UnderlayProperties{initialEndpoint})
	if err := serverMux.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer serverMux.Close()

	serverMux.SetEndpoints([]UnderlayProperties{
		initialEndpoint,
		NewUnderlayProperties(1400, common.StreamTransport, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 10002}, nil),
		NewUnderlayProperties(1400, common.StreamTransport, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 10003}, nil),
	})
	select {
	case <-addedListener.closed:
	case <-time.After(time.Second):
		t.Fatal("added listener was not closed after SetEndpoints() failure")
	}
	select {
	case <-initialListener.closed:
		t.Fatal("existing listener was closed after SetEndpoints() failure")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestSetEndpointsRollsBackStartedPacketListenersOnFailure(t *testing.T) {
	initialConn := newBlockingPacketConn(10001)
	addedConn := newBlockingPacketConn(10002)
	listenErr := errors.New("listen failed")
	packetListenerFactory := &scriptedPacketListenerFactory{
		conns: []net.PacketConn{initialConn, addedConn},
		err:   listenErr,
	}
	initialEndpoint := NewUnderlayProperties(1400, common.PacketTransport, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 10001}, nil)
	serverMux := NewMux(false).
		SetServerUsers(users).
		SetPacketListenerFactory(packetListenerFactory).
		SetEndpoints([]UnderlayProperties{initialEndpoint})
	if err := serverMux.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer serverMux.Close()

	serverMux.SetEndpoints([]UnderlayProperties{
		initialEndpoint,
		NewUnderlayProperties(1400, common.PacketTransport, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 10002}, nil),
		NewUnderlayProperties(1400, common.PacketTransport, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 10003}, nil),
	})
	select {
	case <-addedConn.closed:
	case <-time.After(time.Second):
		t.Fatal("added packet listener was not closed after SetEndpoints() failure")
	}
	select {
	case <-initialConn.closed:
		t.Fatal("existing packet listener was closed after SetEndpoints() failure")
	case <-time.After(100 * time.Millisecond):
	}
}

type scriptedStreamListenerFactory struct {
	mu        sync.Mutex
	listeners []net.Listener
	err       error
}

func (f *scriptedStreamListenerFactory) Listen(ctx context.Context, network string, address string) (net.Listener, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.listeners) > 0 {
		listener := f.listeners[0]
		f.listeners = f.listeners[1:]
		return listener, nil
	}
	return nil, f.err
}

type scriptedPacketListenerFactory struct {
	mu    sync.Mutex
	conns []net.PacketConn
	err   error
}

func (f *scriptedPacketListenerFactory) ListenPacket(ctx context.Context, network string, address string) (net.PacketConn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.conns) > 0 {
		conn := f.conns[0]
		f.conns = f.conns[1:]
		return conn, nil
	}
	return nil, f.err
}

type blockingListener struct {
	closed chan struct{}
	once   sync.Once
}

func newBlockingListener() *blockingListener {
	return &blockingListener{closed: make(chan struct{})}
}

func (l *blockingListener) Accept() (net.Conn, error) {
	<-l.closed
	return nil, net.ErrClosed
}

func (l *blockingListener) Close() error {
	l.once.Do(func() {
		close(l.closed)
	})
	return nil
}

func (l *blockingListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 10001}
}

type blockingPacketConn struct {
	port             int
	closed           chan struct{}
	readDeadline     chan struct{}
	closeOnce        sync.Once
	readDeadlineOnce sync.Once
}

func newBlockingPacketConn(port int) *blockingPacketConn {
	return &blockingPacketConn{
		port:         port,
		closed:       make(chan struct{}),
		readDeadline: make(chan struct{}),
	}
}

func (c *blockingPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	select {
	case <-c.closed:
		return 0, nil, net.ErrClosed
	case <-c.readDeadline:
		return 0, nil, packetTimeoutError{}
	}
}

func (c *blockingPacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	return len(p), nil
}

func (c *blockingPacketConn) Close() error {
	c.closeOnce.Do(func() {
		close(c.closed)
	})
	return nil
}

func (c *blockingPacketConn) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: c.port}
}

func (c *blockingPacketConn) SetDeadline(t time.Time) error {
	return c.SetReadDeadline(t)
}

func (c *blockingPacketConn) SetReadDeadline(t time.Time) error {
	if !t.IsZero() && !t.After(time.Now()) {
		c.readDeadlineOnce.Do(func() {
			close(c.readDeadline)
		})
	}
	return nil
}

func (c *blockingPacketConn) SetWriteDeadline(t time.Time) error {
	return nil
}

type packetTimeoutError struct{}

func (packetTimeoutError) Error() string {
	return "i/o timeout"
}

func (packetTimeoutError) Timeout() bool {
	return true
}

func (packetTimeoutError) Temporary() bool {
	return true
}

func TestNewEndpoints(t *testing.T) {
	cases := []struct {
		old []UnderlayProperties
		new []UnderlayProperties
		res []UnderlayProperties
	}{
		{
			nil,
			nil,
			[]UnderlayProperties{},
		},
		{
			nil,
			[]UnderlayProperties{
				NewUnderlayProperties(1400, common.StreamTransport, common.NilNetAddr(), common.NilNetAddr()),
			},
			[]UnderlayProperties{
				NewUnderlayProperties(1400, common.StreamTransport, common.NilNetAddr(), common.NilNetAddr()),
			},
		},
		{
			[]UnderlayProperties{
				NewUnderlayProperties(1400, common.StreamTransport, common.NilNetAddr(), common.NilNetAddr()),
			},
			nil,
			[]UnderlayProperties{},
		},
		{
			[]UnderlayProperties{
				NewUnderlayProperties(1400, common.StreamTransport, common.NilNetAddr(), common.NilNetAddr()),
				NewUnderlayProperties(1400, common.StreamTransport, common.NilNetAddr(), common.NilNetAddr()),
			},
			[]UnderlayProperties{
				NewUnderlayProperties(1400, common.StreamTransport, common.NilNetAddr(), common.NilNetAddr()),
				NewUnderlayProperties(1400, common.PacketTransport, common.NilNetAddr(), common.NilNetAddr()),
			},
			[]UnderlayProperties{
				NewUnderlayProperties(1400, common.PacketTransport, common.NilNetAddr(), common.NilNetAddr()),
			},
		},
	}

	mux := NewMux(false)
	for _, tc := range cases {
		ep := mux.newEndpoints(tc.old, tc.new)
		if !reflect.DeepEqual(ep, tc.res) {
			t.Errorf("newEndpoints(): got %v, want %v", ep, tc.res)
		}
	}
}
