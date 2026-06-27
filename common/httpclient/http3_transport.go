//go:build with_quic

package httpclient

import (
	"context"
	stdTLS "crypto/tls"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/sagernet/quic-go"
	"github.com/sagernet/quic-go/http3"
	"github.com/sagernet/sing/common/bufio"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/singlink/singlink/common/tls"
	"github.com/singlink/singlink/option"
)

type http3Transport struct {
	h3Transport *http3.Transport
}

type http3BrokenEntry struct {
	until   time.Time
	backoff time.Duration
}

type http3FallbackTransport struct {
	h3Transport   *http3.Transport
	h2Fallback    innerTransport
	fallbackDelay time.Duration
	brokenAccess  sync.Mutex
	broken        map[string]http3BrokenEntry
	brokenOrder   []string
}

type http3OwnedPacketConn struct {
	net.Conn
	closeOnce sync.Once
	closeErr  error
}

func (c *http3OwnedPacketConn) Close() error {
	c.closeOnce.Do(func() {
		c.closeErr = c.Conn.Close()
	})
	return c.closeErr
}

func newHTTP3RoundTripper(
	rawDialer N.Dialer,
	baseTLSConfig tls.Config,
	options option.QUICOptions,
) *http3.Transport {
	var handshakeTimeout time.Duration
	if baseTLSConfig != nil {
		handshakeTimeout = baseTLSConfig.HandshakeTimeout()
	}
	quicConfig := &quic.Config{
		InitialStreamReceiveWindow:     options.StreamReceiveWindow.Value(),
		MaxStreamReceiveWindow:         options.StreamReceiveWindow.Value(),
		InitialConnectionReceiveWindow: options.ConnectionReceiveWindow.Value(),
		MaxConnectionReceiveWindow:     options.ConnectionReceiveWindow.Value(),
		KeepAlivePeriod:                time.Duration(options.KeepAlivePeriod),
		MaxIdleTimeout:                 time.Duration(options.IdleTimeout),
		DisablePathMTUDiscovery:        options.DisablePathMTUDiscovery,
	}
	if options.InitialPacketSize > 0 {
		quicConfig.InitialPacketSize = uint16(options.InitialPacketSize)
	}
	if options.MaxConcurrentStreams > 0 {
		quicConfig.MaxIncomingStreams = int64(options.MaxConcurrentStreams)
	}
	if handshakeTimeout > 0 {
		quicConfig.HandshakeIdleTimeout = handshakeTimeout
	}
	h3Transport := &http3.Transport{
		TLSClientConfig: &stdTLS.Config{},
		QUICConfig:      quicConfig,
		Dial: func(ctx context.Context, addr string, tlsConfig *stdTLS.Config, quicConfig *quic.Config) (*quic.Conn, error) {
			if handshakeTimeout > 0 && quicConfig.HandshakeIdleTimeout == 0 {
				quicConfig = quicConfig.Clone()
				quicConfig.HandshakeIdleTimeout = handshakeTimeout
			}
			if baseTLSConfig != nil {
				var err error
				tlsConfig, err = buildSTDTLSConfig(baseTLSConfig, M.ParseSocksaddr(addr), []string{http3.NextProtoH3})
				if err != nil {
					return nil, err
				}
			} else {
				tlsConfig = tlsConfig.Clone()
				tlsConfig.NextProtos = []string{http3.NextProtoH3}
			}
			conn, err := rawDialer.DialContext(ctx, N.NetworkUDP, M.ParseSocksaddr(addr))
			if err != nil {
				return nil, err
			}
			ownedConn := &http3OwnedPacketConn{Conn: conn}
			quicConn, err := quic.DialEarly(ctx, bufio.NewUnbindPacketConn(ownedConn), conn.RemoteAddr(), tlsConfig, quicConfig)
			if err != nil {
				ownedConn.Close()
				return nil, err
			}
			go func() {
				<-quicConn.Context().Done()
				ownedConn.Close()
			}()
			return quicConn, nil
		},
	}
	return h3Transport
}

func newHTTP3Transport(
	rawDialer N.Dialer,
	baseTLSConfig tls.Config,
	options option.QUICOptions,
) (innerTransport, error) {
	return &http3Transport{
		h3Transport: newHTTP3RoundTripper(rawDialer, baseTLSConfig, options),
	}, nil
}

func newHTTP3FallbackTransport(
	rawDialer N.Dialer,
	baseTLSConfig tls.Config,
	h2Fallback innerTransport,
	options option.QUICOptions,
	fallbackDelay time.Duration,
) (innerTransport, error) {
	return &http3FallbackTransport{
		h3Transport:   newHTTP3RoundTripper(rawDialer, baseTLSConfig, options),
		h2Fallback:    h2Fallback,
		fallbackDelay: fallbackDelay,
		broken:        make(map[string]http3BrokenEntry),
	}, nil
}

func (t *http3Transport) RoundTrip(request *http.Request) (*http.Response, error) {
	return t.h3Transport.RoundTrip(request)
}

func (t *http3Transport) CloseIdleConnections() {
	t.h3Transport.CloseIdleConnections()
}

func (t *http3Transport) Close() error {
	t.CloseIdleConnections()
	return t.h3Transport.Close()
}

func (t *http3FallbackTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme != "https" || requestRequiresHTTP1(request) {
		return t.h2Fallback.RoundTrip(request)
	}
	return t.roundTripHTTP3(request)
}

func (t *http3FallbackTransport) roundTripHTTP3(request *http.Request) (*http.Response, error) {
	authority := requestAuthority(request)
	if t.h3Broken(authority) {
		return t.h2FallbackRoundTrip(request)
	}
	response, err := t.h3Transport.RoundTripOpt(request, http3.RoundTripOpt{OnlyCachedConn: true})
	if err == nil {
		t.clearH3Broken(authority)
		return response, nil
	}
	if !errors.Is(err, http3.ErrNoCachedConn) {
		t.markH3Broken(authority)
		return t.h2FallbackRoundTrip(cloneRequestForRetry(request))
	}
	if !requestReplayable(request) {
		response, err = t.h3Transport.RoundTrip(request)
		if err == nil {
			t.clearH3Broken(authority)
			return response, nil
		}
		t.markH3Broken(authority)
		return nil, err
	}
	return t.roundTripHTTP3Race(request, authority)
}

func (t *http3FallbackTransport) roundTripHTTP3Race(request *http.Request, authority string) (*http.Response, error) {
	ctx, cancel := context.WithCancel(request.Context())
	defer cancel()
	type result struct {
		response *http.Response
		err      error
		h3       bool
	}
	results := make(chan result)
	done := make(chan struct{})
	defer close(done)
	startRoundTrip := func(request *http.Request, useH3 bool) {
		request = request.WithContext(ctx)
		var (
			response *http.Response
			err      error
		)
		if useH3 {
			response, err = t.h3Transport.RoundTrip(request)
		} else {
			response, err = t.h2FallbackRoundTrip(request)
		}
		select {
		case results <- result{response: response, err: err, h3: useH3}:
		case <-done:
			if response != nil && response.Body != nil {
				response.Body.Close()
			}
		}
	}
	goroutines := 1
	received := 0
	finishRace := func() {
		cancel()
	}
	go startRoundTrip(cloneRequestForRetry(request), true)
	timer := time.NewTimer(t.fallbackDelay)
	defer timer.Stop()
	var (
		h3Err       error
		fallbackErr error
	)
	for {
		select {
		case <-timer.C:
			if goroutines == 1 {
				goroutines++
				go startRoundTrip(cloneRequestForRetry(request), false)
			}
		case raceResult := <-results:
			received++
			if raceResult.err == nil {
				if raceResult.h3 {
					t.clearH3Broken(authority)
				}
				finishRace()
				return raceResult.response, nil
			}
			if raceResult.h3 {
				t.markH3Broken(authority)
				h3Err = raceResult.err
				if goroutines == 1 {
					goroutines++
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
					go startRoundTrip(cloneRequestForRetry(request), false)
				}
			} else {
				fallbackErr = raceResult.err
			}
			if received < goroutines {
				continue
			}
			finishRace()
			switch {
			case h3Err != nil && fallbackErr != nil:
				return nil, E.Errors(h3Err, fallbackErr)
			case fallbackErr != nil:
				return nil, fallbackErr
			default:
				return nil, h3Err
			}
		}
	}
}

func (t *http3FallbackTransport) h2FallbackRoundTrip(request *http.Request) (*http.Response, error) {
	if fallback, isFallback := t.h2Fallback.(*http2FallbackTransport); isFallback {
		return fallback.roundTrip(request, true)
	}
	return t.h2Fallback.RoundTrip(request)
}

func (t *http3FallbackTransport) CloseIdleConnections() {
	t.h3Transport.CloseIdleConnections()
	t.h2Fallback.CloseIdleConnections()
}

func (t *http3FallbackTransport) Close() error {
	t.CloseIdleConnections()
	return t.h3Transport.Close()
}

func (t *http3FallbackTransport) h3Broken(authority string) bool {
	if authority == "" {
		return false
	}
	t.brokenAccess.Lock()
	defer t.brokenAccess.Unlock()
	entry, found := t.broken[authority]
	if !found {
		return false
	}
	if entry.until.IsZero() || !time.Now().Before(entry.until) {
		delete(t.broken, authority)
		t.removeH3BrokenOrderLocked(authority)
		return false
	}
	return true
}

func (t *http3FallbackTransport) clearH3Broken(authority string) {
	if authority == "" {
		return
	}
	t.brokenAccess.Lock()
	delete(t.broken, authority)
	t.removeH3BrokenOrderLocked(authority)
	t.brokenAccess.Unlock()
}

func (t *http3FallbackTransport) markH3Broken(authority string) {
	if authority == "" {
		return
	}
	t.brokenAccess.Lock()
	defer t.brokenAccess.Unlock()
	now := time.Now()
	entry, exists := t.broken[authority]
	if entry.backoff == 0 {
		entry.backoff = 5 * time.Minute
	} else {
		entry.backoff *= 2
		if entry.backoff > 48*time.Hour {
			entry.backoff = 48 * time.Hour
		}
	}
	entry.until = now.Add(entry.backoff)
	if !exists {
		t.pruneH3BrokenLocked(now)
	}
	t.broken[authority] = entry
	if !exists {
		t.brokenOrder = append(t.brokenOrder, authority)
	}
}

func (t *http3FallbackTransport) pruneH3BrokenLocked(now time.Time) {
	for authority, entry := range t.broken {
		if entry.until.IsZero() || !now.Before(entry.until) {
			delete(t.broken, authority)
		}
	}
	if len(t.broken) < maxFallbackAuthorityEntries {
		t.compactH3BrokenOrderLocked()
		return
	}
	if len(t.brokenOrder) == 0 {
		for authority := range t.broken {
			delete(t.broken, authority)
			if len(t.broken) < maxFallbackAuthorityEntries {
				return
			}
		}
		return
	}
	writeAt := 0
	for _, authority := range t.brokenOrder {
		if _, found := t.broken[authority]; !found {
			continue
		}
		if len(t.broken) >= maxFallbackAuthorityEntries {
			delete(t.broken, authority)
			continue
		}
		t.brokenOrder[writeAt] = authority
		writeAt++
	}
	t.brokenOrder = t.brokenOrder[:writeAt]
}

func (t *http3FallbackTransport) compactH3BrokenOrderLocked() {
	writeAt := 0
	for _, authority := range t.brokenOrder {
		if _, found := t.broken[authority]; found {
			t.brokenOrder[writeAt] = authority
			writeAt++
		}
	}
	t.brokenOrder = t.brokenOrder[:writeAt]
}

func (t *http3FallbackTransport) removeH3BrokenOrderLocked(authority string) {
	for index, entry := range t.brokenOrder {
		if entry == authority {
			copy(t.brokenOrder[index:], t.brokenOrder[index+1:])
			t.brokenOrder = t.brokenOrder[:len(t.brokenOrder)-1]
			return
		}
	}
}
