package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	shadowsocks "github.com/sagernet/sing-shadowsocks"
	"github.com/sagernet/sing-shadowsocks/shadowaead"
	"github.com/sagernet/sing-shadowsocks/shadowaead_2022"
	"github.com/sagernet/sing/common/json/badoption"
	M "github.com/sagernet/sing/common/metadata"
	box "github.com/singlink/singlink"
	C "github.com/singlink/singlink/constant"
	"github.com/singlink/singlink/include"
	"github.com/singlink/singlink/option"
)

const (
	defaultTotalUsers = 100_000
	defaultIdleUsers  = 7_000
	defaultLightUsers = 2_000
	defaultMedUsers   = 800
	defaultHeavyUsers = 200
)

const errorSampleLimit = 12

type config struct {
	totalUsers      int
	idleUsers       int
	lightUsers      int
	medUsers        int
	heavyUsers      int
	duration        time.Duration
	ramp            time.Duration
	lifecycle       bool
	soakDay         time.Duration
	drain           time.Duration
	metricsInterval time.Duration
	metricsDir      string
	method          string
	ssPort          uint16
	heavyRate       int64
	serverKey       string
}

type stats struct {
	enabled             atomic.Bool
	shuttingDown        atomic.Bool
	idleConnected       atomic.Int64
	idleActive          atomic.Int64
	active              atomic.Int64
	requests            atomic.Int64
	success             atomic.Int64
	errors              atomic.Int64
	warmupErrors        atomic.Int64
	shutdownErrors      atomic.Int64
	bytes               atomic.Int64
	latencyTotal        atomic.Int64
	latencyMax          atomic.Int64
	latencyMu           sync.Mutex
	latencies           []time.Duration
	errorMu             sync.Mutex
	errorCounts         map[string]int64
	warmupErrorCounts   map[string]int64
	shutdownErrorCounts map[string]int64
	errorSamples        []string
}

type cpuSample struct {
	when  time.Time
	ticks uint64
}

type resourcePeak struct {
	rssBytes   uint64
	heapBytes  uint64
	goroutines int
	fdCount    int
}

type resourceSample struct {
	Timestamp      string         `json:"timestamp"`
	ElapsedSeconds float64        `json:"elapsed_seconds"`
	Phase          string         `json:"phase"`
	RSSBytes       uint64         `json:"rss_bytes"`
	HeapBytes      uint64         `json:"heap_bytes"`
	Goroutines     int            `json:"goroutines"`
	FDCount        int            `json:"fd_count"`
	Active         int64          `json:"active"`
	IdleActive     int64          `json:"idle_active"`
	IdleConnected  int64          `json:"idle_connected"`
	Requests       int64          `json:"requests"`
	Success        int64          `json:"success"`
	Errors         int64          `json:"errors"`
	WarmupErrors   int64          `json:"warmup_errors"`
	ShutdownErrors int64          `json:"shutdown_errors"`
	Bytes          int64          `json:"bytes"`
	TCPStates      map[string]int `json:"tcp_states,omitempty"`
}

type metricsRecorder struct {
	start   time.Time
	st      *stats
	file    *os.File
	path    string
	encoder *json.Encoder
	mu      sync.Mutex
	samples []resourceSample
	peak    resourcePeak
}

type lifecycleResult struct {
	pass     bool
	reasons  []string
	preload  resourceSample
	baseline resourceSample
	final    resourceSample
	drain    resourceSample
}

type targetHTTPServer struct {
	listener net.Listener
	chunk    []byte
	done     chan struct{}
	closed   atomic.Bool
	connsMu  sync.Mutex
	conns    map[net.Conn]struct{}
	wg       sync.WaitGroup
}

type targetHoldServer struct {
	listener net.Listener
	closed   atomic.Bool
	connsMu  sync.Mutex
	conns    map[net.Conn]struct{}
	wg       sync.WaitGroup
}

func main() {
	cfg := parseConfig()
	if cfg.idleUsers+cfg.lightUsers+cfg.medUsers+cfg.heavyUsers > cfg.totalUsers {
		fatalf("active user buckets exceed total users")
	}

	targetURL, stopTarget, err := startTargetHTTP()
	if err != nil {
		fatalf("start target http: %v", err)
	}
	defer stopTarget()
	idleTargetAddr, stopIdleTarget, err := startTargetHold()
	if err != nil {
		fatalf("start idle target: %v", err)
	}
	defer stopIdleTarget()

	targetAddr := strings.TrimPrefix(targetURL, "http://")
	ssPort, stopBox, err := startBox(cfg)
	if err != nil {
		fatalf("start singlink shadowsocks server: %v", err)
	}
	defer stopBox()

	fmt.Printf("ss_loadtest: total_users=%d online=%d idle=%d light=%d medium=%d heavy=%d method=%s ss=127.0.0.1:%d target=%s idle_target=%s duration=%s ramp=%s lifecycle=%t drain=%s heavy_rate=%s/s\n",
		cfg.totalUsers,
		cfg.idleUsers+cfg.lightUsers+cfg.medUsers+cfg.heavyUsers,
		cfg.idleUsers,
		cfg.lightUsers,
		cfg.medUsers,
		cfg.heavyUsers,
		cfg.method,
		ssPort,
		targetAddr,
		idleTargetAddr,
		cfg.duration,
		cfg.ramp,
		cfg.lifecycle,
		cfg.drain,
		formatBytes(uint64(cfg.heavyRate)),
	)

	if !runLoad(cfg, ssPort, targetAddr, idleTargetAddr) {
		os.Exit(2)
	}
}

func parseConfig() config {
	var cfg config
	flag.IntVar(&cfg.totalUsers, "total-users", defaultTotalUsers, "total configured SS users")
	flag.IntVar(&cfg.idleUsers, "idle", defaultIdleUsers, "idle online users")
	flag.IntVar(&cfg.lightUsers, "light", defaultLightUsers, "light online users")
	flag.IntVar(&cfg.medUsers, "medium", defaultMedUsers, "medium online users")
	flag.IntVar(&cfg.heavyUsers, "heavy", defaultHeavyUsers, "heavy online users")
	flag.DurationVar(&cfg.duration, "duration", 30*time.Second, "measured duration after ramp")
	flag.DurationVar(&cfg.ramp, "ramp", 15*time.Second, "warm-up ramp before measured duration")
	flag.BoolVar(&cfg.lifecycle, "lifecycle", false, "enable accelerated lifecycle/soak mode")
	flag.DurationVar(&cfg.soakDay, "soak-day", 0, "accelerated duration that represents one day of user lifecycle")
	flag.DurationVar(&cfg.drain, "drain", 60*time.Second, "drain window after lifecycle traffic stops")
	flag.DurationVar(&cfg.metricsInterval, "metrics-interval", 5*time.Second, "resource metrics sampling interval")
	flag.StringVar(&cfg.metricsDir, "metrics-dir", "", "directory for lifecycle metrics output")
	flag.StringVar(&cfg.method, "method", "2022-blake3-aes-128-gcm", "Shadowsocks method")
	flag.Int64Var(&cfg.heavyRate, "heavy-rate", 768*1024, "per-heavy-user read cap in bytes/sec")
	var ssPort int
	flag.IntVar(&ssPort, "ss-port", 0, "local SS listen port, 0 picks a free port")
	flag.Parse()
	if cfg.totalUsers <= 0 || cfg.idleUsers < 0 || cfg.lightUsers < 0 || cfg.medUsers < 0 || cfg.heavyUsers < 0 {
		fatalf("user counts must be positive/non-negative")
	}
	if cfg.duration <= 0 || cfg.ramp < 0 {
		fatalf("duration must be positive and ramp must be non-negative")
	}
	if cfg.soakDay > 0 {
		cfg.lifecycle = true
		cfg.duration = cfg.soakDay
	}
	if cfg.lifecycle && cfg.soakDay == 0 {
		cfg.soakDay = cfg.duration
	}
	if cfg.lifecycle && cfg.metricsInterval <= 0 {
		fatalf("metrics-interval must be positive")
	}
	if cfg.lifecycle && cfg.drain < 0 {
		fatalf("drain must be non-negative")
	}
	if cfg.heavyRate <= 0 {
		fatalf("heavy-rate must be positive")
	}
	if ssPort < 0 || ssPort > 65535 {
		fatalf("invalid ss-port")
	}
	cfg.ssPort = uint16(ssPort)
	if keyLength, is2022 := methodKeyLength(cfg.method); is2022 {
		cfg.serverKey = deterministicBase64Key("ss-loadtest-server", keyLength)
	} else if strings.HasPrefix(cfg.method, "2022-") {
		fatalf("method %s is not supported for multi-user EIH load testing", cfg.method)
	}
	return cfg
}

func startBox(cfg config) (uint16, func(), error) {
	port := cfg.ssPort
	if port == 0 {
		freePort, err := pickFreePort()
		if err != nil {
			return 0, nil, err
		}
		port = freePort
	}
	users := make([]option.ShadowsocksUser, cfg.totalUsers)
	for i := range users {
		password := userPassword(i, cfg.method)
		users[i] = option.ShadowsocksUser{Name: password, Password: password}
	}
	listen := badoption.Addr(netip.MustParseAddr("127.0.0.1"))
	ctx, cancel := context.WithCancel(include.Context(context.Background()))
	instance, err := box.New(box.Options{
		Context: ctx,
		Options: option.Options{
			Log: &option.LogOptions{Disabled: true},
			Inbounds: []option.Inbound{
				{
					Type: C.TypeShadowsocks,
					Tag:  "ss-in",
					Options: &option.ShadowsocksInboundOptions{
						ListenOptions: option.ListenOptions{
							Listen:     &listen,
							ListenPort: port,
						},
						Method:   cfg.method,
						Password: cfg.serverKey,
						Users:    users,
					},
				},
			},
			Outbounds: []option.Outbound{{Type: C.TypeDirect, Tag: "direct"}},
		},
	})
	if err != nil {
		cancel()
		return 0, nil, err
	}
	if err := instance.Start(); err != nil {
		instance.Close()
		cancel()
		return 0, nil, err
	}
	return port, func() {
		instance.Close()
		cancel()
	}, nil
}

func startTargetHTTP() (string, func(), error) {
	chunk := make([]byte, 32*1024)
	for i := range chunk {
		chunk[i] = byte('a' + i%26)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	server := &targetHTTPServer{
		listener: listener,
		chunk:    chunk,
		done:     make(chan struct{}),
		conns:    make(map[net.Conn]struct{}),
	}
	server.wg.Add(1)
	go server.serve()
	return "http://" + listener.Addr().String(), server.close, nil
}

func (s *targetHTTPServer) serve() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if s.closed.Load() {
				return
			}
			continue
		}
		s.connsMu.Lock()
		s.conns[conn] = struct{}{}
		s.connsMu.Unlock()
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer func() {
				s.connsMu.Lock()
				delete(s.conns, conn)
				s.connsMu.Unlock()
				conn.Close()
			}()
			s.handleConn(conn)
		}()
	}
}

func (s *targetHTTPServer) close() {
	if s.closed.CompareAndSwap(false, true) {
		close(s.done)
		s.listener.Close()
		s.connsMu.Lock()
		for conn := range s.conns {
			conn.Close()
		}
		s.connsMu.Unlock()
	}
	s.wg.Wait()
}

func (s *targetHTTPServer) handleConn(conn net.Conn) {
	reader := bufio.NewReader(conn)
	request, err := http.ReadRequest(reader)
	if err != nil {
		return
	}
	request.Body.Close()
	if request.URL.Path == "/idle" {
		writeIdleResponse(conn, s.done)
		return
	}
	size := queryInt(request, "size", 64*1024)
	writer := bufio.NewWriterSize(conn, 32*1024)
	fmt.Fprintf(writer, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\nContent-Type: application/octet-stream\r\nConnection: close\r\n\r\n", size)
	writeBytes(writer, s.chunk, size)
	writer.Flush()
}

func writeIdleResponse(conn net.Conn, done <-chan struct{}) {
	writer := bufio.NewWriter(conn)
	fmt.Fprint(writer, "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nConnection: keep-alive\r\nTransfer-Encoding: chunked\r\n\r\n")
	if err := writeHTTPChunk(writer, "ok\n"); err != nil {
		return
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
		}
		if err := writeHTTPChunk(writer, " \n"); err != nil {
			return
		}
	}
}

func writeHTTPChunk(writer *bufio.Writer, value string) error {
	if _, err := fmt.Fprintf(writer, "%x\r\n%s\r\n", len(value), value); err != nil {
		return err
	}
	return writer.Flush()
}

func startTargetHold() (string, func(), error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	server := &targetHoldServer{
		listener: listener,
		conns:    make(map[net.Conn]struct{}),
	}
	server.wg.Add(1)
	go server.serve()
	return listener.Addr().String(), server.close, nil
}

func (s *targetHoldServer) serve() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if s.closed.Load() {
				return
			}
			continue
		}
		s.connsMu.Lock()
		s.conns[conn] = struct{}{}
		s.connsMu.Unlock()
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer func() {
				s.connsMu.Lock()
				delete(s.conns, conn)
				s.connsMu.Unlock()
				conn.Close()
			}()
			buffer := make([]byte, 1)
			for !s.closed.Load() {
				_ = conn.SetReadDeadline(time.Now().Add(time.Second))
				if _, err := conn.Read(buffer); err != nil {
					if isTimeout(err) {
						continue
					}
					return
				}
			}
		}()
	}
}

func (s *targetHoldServer) close() {
	if s.closed.CompareAndSwap(false, true) {
		s.listener.Close()
		s.connsMu.Lock()
		for conn := range s.conns {
			conn.Close()
		}
		s.connsMu.Unlock()
	}
	s.wg.Wait()
}

func newMetricsRecorder(cfg config, st *stats) (*metricsRecorder, error) {
	recorder := &metricsRecorder{start: time.Now(), st: st}
	if !cfg.lifecycle {
		return recorder, nil
	}
	metricsDir := cfg.metricsDir
	if metricsDir == "" {
		metricsDir = filepath.Join(os.TempDir(), "ss_loadtest-"+time.Now().UTC().Format("20060102T150405Z"))
	}
	if err := os.MkdirAll(metricsDir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(metricsDir, "metrics.jsonl")
	file, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	recorder.file = file
	recorder.path = path
	recorder.encoder = json.NewEncoder(file)
	return recorder, nil
}

func (r *metricsRecorder) Close() error {
	if r.file == nil {
		return nil
	}
	return r.file.Close()
}

func (r *metricsRecorder) sampleLoop(interval time.Duration, phase string, done <-chan struct{}) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r.record(phase)
		case <-done:
			return
		}
	}
}

func (r *metricsRecorder) drain(duration time.Duration, interval time.Duration) resourceSample {
	if duration <= 0 {
		return r.record("post_drain")
	}
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(duration)
	var sample resourceSample
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		sleep := min(remaining, interval)
		time.Sleep(sleep)
		runtime.GC()
		sample = r.record("drain")
	}
	return sample
}

func (r *metricsRecorder) record(phase string) resourceSample {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	sample := resourceSample{
		Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
		ElapsedSeconds: time.Since(r.start).Seconds(),
		Phase:          phase,
		RSSBytes:       readRSS(),
		HeapBytes:      memory.HeapAlloc,
		Goroutines:     runtime.NumGoroutine(),
		FDCount:        readFDCount(),
		Active:         r.st.active.Load(),
		IdleActive:     r.st.idleActive.Load(),
		IdleConnected:  r.st.idleConnected.Load(),
		Requests:       r.st.requests.Load(),
		Success:        r.st.success.Load(),
		Errors:         r.st.errors.Load(),
		WarmupErrors:   r.st.warmupErrors.Load(),
		ShutdownErrors: r.st.shutdownErrors.Load(),
		Bytes:          r.st.bytes.Load(),
	}
	if r.file != nil {
		sample.TCPStates = readTCPStates()
	}

	r.mu.Lock()
	if sample.RSSBytes > r.peak.rssBytes {
		r.peak.rssBytes = sample.RSSBytes
	}
	if sample.HeapBytes > r.peak.heapBytes {
		r.peak.heapBytes = sample.HeapBytes
	}
	if sample.Goroutines > r.peak.goroutines {
		r.peak.goroutines = sample.Goroutines
	}
	if sample.FDCount > r.peak.fdCount {
		r.peak.fdCount = sample.FDCount
	}
	r.samples = append(r.samples, sample)
	if r.encoder != nil {
		_ = r.encoder.Encode(sample)
	}
	r.mu.Unlock()
	return sample
}

func (r *metricsRecorder) samplesSnapshot() []resourceSample {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]resourceSample(nil), r.samples...)
}

func runLoad(cfg config, ssPort uint16, targetAddr string, idleTargetAddr string) bool {
	methods := make([]shadowsocks.Method, cfg.idleUsers+cfg.lightUsers+cfg.medUsers+cfg.heavyUsers)
	for i := range methods {
		method, err := clientMethod(cfg.method, cfg.serverKey, userPassword(i, cfg.method))
		if err != nil {
			fatalf("create client method: %v", err)
		}
		methods[i] = method
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st := &stats{
		errorCounts:         make(map[string]int64),
		warmupErrorCounts:   make(map[string]int64),
		shutdownErrorCounts: make(map[string]int64),
	}
	recorder, err := newMetricsRecorder(cfg, st)
	if err != nil {
		fatalf("start metrics recorder: %v", err)
	}
	defer recorder.Close()
	if recorder.path != "" {
		fmt.Printf("metrics: %s\n", recorder.path)
	}
	destination := M.ParseSocksaddr(targetAddr)
	idleDestination := M.ParseSocksaddr(idleTargetAddr)
	ssAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(ssPort)))
	preloadSample := recorder.record("preload")

	var wg sync.WaitGroup
	userIndex := 0
	spawn := func(count int, kind string, destination M.Socksaddr, fn func(context.Context, config, *stats, shadowsocks.Method, string, M.Socksaddr, int, string)) {
		for i := 0; i < count; i++ {
			index := userIndex
			userIndex++
			wg.Add(1)
			go func() {
				defer wg.Done()
				sleepJitter(ctx, cfg.ramp)
				fn(ctx, cfg, st, methods[index], ssAddr, destination, index, kind)
			}()
		}
	}

	spawn(cfg.idleUsers, "idle", idleDestination, idleUser)
	spawn(cfg.lightUsers, "light", destination, lightUser)
	spawn(cfg.medUsers, "medium", destination, mediumUser)
	for i := 0; i < cfg.heavyUsers; i++ {
		index := userIndex
		userIndex++
		wg.Add(1)
		go func() {
			defer wg.Done()
			sleepJitter(ctx, cfg.ramp)
			heavyUser(ctx, st, methods[index], ssAddr, destination, index, cfg.heavyRate, "heavy")
		}()
	}

	fmt.Printf("warming up for %s...\n", cfg.ramp)
	time.Sleep(cfg.ramp)
	runtime.GC()
	baselineSample := recorder.record("baseline")
	st.enabled.Store(true)
	startCPU := readCPUSample()
	start := time.Now()
	fmt.Printf("measuring for %s...\n", cfg.duration)

	monitorDone := make(chan struct{})
	go recorder.sampleLoop(cfg.metricsInterval, "measure", monitorDone)

	time.Sleep(cfg.duration)
	elapsed := time.Since(start)
	finalSample := recorder.record("final")
	st.enabled.Store(false)
	st.shuttingDown.Store(true)
	cancel()
	close(monitorDone)
	endCPU := readCPUSample()

	waitDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitDone)
	}()
	select {
	case <-waitDone:
	case <-time.After(30 * time.Second):
		fmt.Fprintln(os.Stderr, "warning: workers did not stop within 30s")
	}

	methods = nil
	runtime.GC()
	drainSample := recorder.record("post_workers")
	if cfg.lifecycle && cfg.drain > 0 {
		drainSample = recorder.drain(cfg.drain, cfg.metricsInterval)
	}
	runtime.GC()
	runtime.GC()
	drainSample = recorder.record("post_drain")

	printSummary(st, recorder.peak, cpuPercent(startCPU, endCPU), elapsed)
	if cfg.lifecycle {
		result := evaluateLifecycle(cfg, st, recorder.samplesSnapshot(), preloadSample, baselineSample, finalSample, drainSample)
		printLifecycleResult(result, recorder.path)
		return result.pass
	}
	return true
}

func idleUser(ctx context.Context, cfg config, st *stats, method shadowsocks.Method, ssAddr string, destination M.Socksaddr, index int, profile string) {
	for ctx.Err() == nil {
		conn, stage, err := dialSS(ctx, method, ssAddr, destination)
		if err != nil {
			st.recordError(stage, profile, "", index, err)
			sleepRange(ctx, 500*time.Millisecond, 1500*time.Millisecond)
			continue
		}
		st.idleConnected.Add(1)
		st.idleActive.Add(1)
		st.active.Add(1)
		hold := cfg.duration + cfg.ramp
		if cfg.lifecycle {
			hold = lifecycleRange(cfg, 30*time.Minute, 8*time.Hour)
		}
		timer := time.NewTimer(hold)
		select {
		case <-ctx.Done():
		case <-timer.C:
		}
		timer.Stop()
		conn.Close()
		st.idleActive.Add(-1)
		st.active.Add(-1)
		if !cfg.lifecycle {
			return
		}
		sleepRange(ctx, lifecycleDuration(cfg, time.Minute), lifecycleDuration(cfg, 30*time.Minute))
	}
}

func lightUser(ctx context.Context, cfg config, st *stats, method shadowsocks.Method, ssAddr string, destination M.Socksaddr, index int, profile string) {
	paths := []string{
		"/bytes?size=32768",
		"/bytes?size=65536",
		"/bytes?size=131072",
	}
	for ctx.Err() == nil {
		path := paths[rand.IntN(len(paths))]
		fetch(ctx, st, method, ssAddr, destination, path, 0, profile, index)
		if cfg.lifecycle {
			sleepRange(ctx, lifecycleDuration(cfg, 15*time.Minute), lifecycleDuration(cfg, 45*time.Minute))
		} else {
			sleepRange(ctx, 10*time.Second, 20*time.Second)
		}
	}
}

func mediumUser(ctx context.Context, cfg config, st *stats, method shadowsocks.Method, ssAddr string, destination M.Socksaddr, index int, profile string) {
	paths := []string{
		"/bytes?size=65536",
		"/bytes?size=262144",
		"/bytes?size=1048576",
		"/bytes?size=524288",
	}
	for ctx.Err() == nil {
		path := paths[rand.IntN(len(paths))]
		fetch(ctx, st, method, ssAddr, destination, path, 0, profile, index)
		if cfg.lifecycle {
			sleepRange(ctx, lifecycleDuration(cfg, 2*time.Minute), lifecycleDuration(cfg, 8*time.Minute))
		} else {
			sleepRange(ctx, 1200*time.Millisecond, 2600*time.Millisecond)
		}
	}
}

func heavyUser(ctx context.Context, st *stats, method shadowsocks.Method, ssAddr string, destination M.Socksaddr, index int, rate int64, profile string) {
	for ctx.Err() == nil {
		fetch(ctx, st, method, ssAddr, destination, "/bytes?size=67108864", rate, profile, index)
	}
}

func fetch(ctx context.Context, st *stats, method shadowsocks.Method, ssAddr string, destination M.Socksaddr, path string, rate int64, profile string, index int) {
	start := time.Now()
	conn, stage, err := dialSS(ctx, method, ssAddr, destination)
	if err != nil {
		st.recordError(stage, profile, path, index, err)
		return
	}
	defer conn.Close()
	stopCancelWatcher := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-stopCancelWatcher:
		}
	}()
	defer close(stopCancelWatcher)
	st.active.Add(1)
	defer st.active.Add(-1)

	if _, err = fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: load.local\r\nConnection: close\r\n\r\n", path); err != nil {
		st.recordError("write_request", profile, path, index, err)
		return
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
	if err != nil {
		st.recordError("read_response_header", profile, path, index, err)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		st.recordError("http_status", profile, path, index, fmt.Errorf("http status %d", response.StatusCode))
		return
	}
	buffer := make([]byte, 32*1024)
	var total int64
	contentLength := response.ContentLength
	for {
		if ctx.Err() != nil {
			return
		}
		n, err := response.Body.Read(buffer)
		if n > 0 {
			total += int64(n)
			if st.enabled.Load() {
				st.bytes.Add(int64(n))
			}
			if rate > 0 {
				throttle(start, total, rate)
			}
			if contentLength >= 0 && total >= contentLength {
				st.recordSuccess(total, time.Since(start))
				return
			}
		}
		if err == nil {
			continue
		}
		if isTimeout(err) {
			continue
		}
		if errors.Is(err, io.EOF) {
			st.recordSuccess(total, time.Since(start))
		} else if ctx.Err() == nil {
			st.recordError("read_body", profile, path, index, err)
		}
		return
	}
}

func dialSS(ctx context.Context, method shadowsocks.Method, ssAddr string, destination M.Socksaddr) (net.Conn, string, error) {
	dialer := net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", ssAddr)
	if err != nil {
		return nil, "dial_tcp", err
	}
	ssConn, err := method.DialConn(conn, destination)
	if err != nil {
		conn.Close()
		return nil, "ss_handshake", err
	}
	return ssConn, "", nil
}

func (s *stats) recordSuccess(bytes int64, latency time.Duration) {
	if !s.enabled.Load() {
		return
	}
	s.requests.Add(1)
	s.success.Add(1)
	s.latencyTotal.Add(int64(latency))
	for {
		old := s.latencyMax.Load()
		if int64(latency) <= old || s.latencyMax.CompareAndSwap(old, int64(latency)) {
			break
		}
	}
	s.latencyMu.Lock()
	s.latencies = append(s.latencies, latency)
	s.latencyMu.Unlock()
}

func (s *stats) recordError(stage string, profile string, path string, index int, err error) {
	key := errorKey(stage, profile, path, err)
	sample := errorSample(stage, profile, path, index, err)
	s.errorMu.Lock()
	switch {
	case s.enabled.Load():
		s.requests.Add(1)
		s.errors.Add(1)
		s.errorCounts[key]++
		if len(s.errorSamples) < errorSampleLimit {
			s.errorSamples = append(s.errorSamples, sample)
		}
	case s.shuttingDown.Load():
		s.shutdownErrors.Add(1)
		s.shutdownErrorCounts[key]++
	default:
		s.warmupErrors.Add(1)
		s.warmupErrorCounts[key]++
	}
	s.errorMu.Unlock()
}

func printSummary(st *stats, peak resourcePeak, cpu float64, elapsed time.Duration) {
	requests := st.requests.Load()
	success := st.success.Load()
	errors := st.errors.Load()
	bytes := st.bytes.Load()
	latencies := st.snapshotLatencies()
	var avgLatency time.Duration
	if success > 0 {
		avgLatency = time.Duration(st.latencyTotal.Load() / success)
	}
	fmt.Println("summary:")
	fmt.Printf("  elapsed: %s\n", elapsed.Round(time.Millisecond))
	fmt.Printf("  idle connected: %d active_idle=%d active_total=%d\n", st.idleConnected.Load(), st.idleActive.Load(), st.active.Load())
	fmt.Printf("  requests: %d success=%d errors=%d error_rate=%.4f%%\n", requests, success, errors, percent(errors, maxInt64(requests, 1)))
	fmt.Printf("  throughput: %s/s total=%s\n", formatBytes(uint64(float64(bytes)/elapsed.Seconds())), formatBytes(uint64(bytes)))
	fmt.Printf("  req/s: %.2f\n", float64(success)/elapsed.Seconds())
	fmt.Printf("  latency avg=%s p50=%s p95=%s p99=%s max=%s\n", avgLatency.Round(time.Millisecond), percentile(latencies, 50), percentile(latencies, 95), percentile(latencies, 99), time.Duration(st.latencyMax.Load()).Round(time.Millisecond))
	fmt.Printf("  peak rss=%s heap=%s goroutines=%d fd=%d\n", formatBytes(peak.rssBytes), formatBytes(peak.heapBytes), peak.goroutines, peak.fdCount)
	fmt.Printf("  process cpu: %.1f%% of all cores\n", cpu)
	for _, entry := range st.topErrors(st.errorCounts, 8) {
		fmt.Printf("  error[%d]: %s\n", entry.count, entry.message)
	}
	for _, sample := range st.errorSamplesSnapshot() {
		fmt.Printf("  error_sample: %s\n", sample)
	}
	if warmupErrors := st.warmupErrors.Load(); warmupErrors > 0 {
		fmt.Printf("  warmup_errors: %d\n", warmupErrors)
		for _, entry := range st.topErrors(st.warmupErrorCounts, 4) {
			fmt.Printf("  warmup_error[%d]: %s\n", entry.count, entry.message)
		}
	}
	if shutdownErrors := st.shutdownErrors.Load(); shutdownErrors > 0 {
		fmt.Printf("  shutdown_errors: %d\n", shutdownErrors)
		for _, entry := range st.topErrors(st.shutdownErrorCounts, 4) {
			fmt.Printf("  shutdown_error[%d]: %s\n", entry.count, entry.message)
		}
	}
}

func evaluateLifecycle(cfg config, st *stats, samples []resourceSample, preload resourceSample, baseline resourceSample, final resourceSample, drain resourceSample) lifecycleResult {
	result := lifecycleResult{
		pass:     true,
		preload:  preload,
		baseline: baseline,
		final:    final,
		drain:    drain,
	}
	addFailure := func(format string, args ...any) {
		result.pass = false
		result.reasons = append(result.reasons, fmt.Sprintf(format, args...))
	}
	if errors := st.errors.Load(); errors > 0 {
		addFailure("measured errors=%d", errors)
	}
	if drain.Active != 0 {
		addFailure("active connections after drain=%d", drain.Active)
	}
	if drain.IdleActive != 0 {
		addFailure("idle connections after drain=%d", drain.IdleActive)
	}
	fdTolerance := maxInt(25, preload.FDCount/20)
	if preload.FDCount >= 0 && drain.FDCount > preload.FDCount+fdTolerance {
		addFailure("fd leak suspected: preload=%d post_drain=%d tolerance=%d", preload.FDCount, drain.FDCount, fdTolerance)
	}
	goroutineTolerance := maxInt(200, preload.Goroutines/5)
	if drain.Goroutines > preload.Goroutines+goroutineTolerance {
		addFailure("goroutine leak suspected: preload=%d post_drain=%d tolerance=%d", preload.Goroutines, drain.Goroutines, goroutineTolerance)
	}
	if heapGrowthSuspected(samples) {
		addFailure("heap trend kept growing during measured lifecycle window")
	}
	if final.Errors != st.errors.Load() {
		addFailure("error counter changed unexpectedly: final=%d current=%d", final.Errors, st.errors.Load())
	}
	_ = cfg
	return result
}

func printLifecycleResult(result lifecycleResult, metricsPath string) {
	fmt.Println("lifecycle:")
	if metricsPath != "" {
		fmt.Printf("  metrics: %s\n", metricsPath)
	}
	fmt.Printf("  preload: rss=%s heap=%s goroutines=%d fd=%d active=%d idle_active=%d\n", formatBytes(result.preload.RSSBytes), formatBytes(result.preload.HeapBytes), result.preload.Goroutines, result.preload.FDCount, result.preload.Active, result.preload.IdleActive)
	fmt.Printf("  baseline: rss=%s heap=%s goroutines=%d fd=%d active=%d idle_active=%d\n", formatBytes(result.baseline.RSSBytes), formatBytes(result.baseline.HeapBytes), result.baseline.Goroutines, result.baseline.FDCount, result.baseline.Active, result.baseline.IdleActive)
	fmt.Printf("  final: rss=%s heap=%s goroutines=%d fd=%d active=%d idle_active=%d\n", formatBytes(result.final.RSSBytes), formatBytes(result.final.HeapBytes), result.final.Goroutines, result.final.FDCount, result.final.Active, result.final.IdleActive)
	fmt.Printf("  post_drain: rss=%s heap=%s goroutines=%d fd=%d active=%d idle_active=%d\n", formatBytes(result.drain.RSSBytes), formatBytes(result.drain.HeapBytes), result.drain.Goroutines, result.drain.FDCount, result.drain.Active, result.drain.IdleActive)
	if result.pass {
		fmt.Println("  result: PASS")
		return
	}
	fmt.Println("  result: FAIL")
	for _, reason := range result.reasons {
		fmt.Printf("  reason: %s\n", reason)
	}
}

func heapGrowthSuspected(samples []resourceSample) bool {
	var measured []resourceSample
	for _, sample := range samples {
		if sample.Phase == "measure" {
			measured = append(measured, sample)
		}
	}
	if len(measured) < 12 {
		return false
	}
	mid := len(measured) / 2
	firstAvg := averageHeap(measured[:mid])
	secondAvg := averageHeap(measured[mid:])
	if secondAvg <= firstAvg {
		return false
	}
	growth := secondAvg - firstAvg
	return growth > 256*1024*1024 && float64(secondAvg) > float64(firstAvg)*1.20
}

func averageHeap(samples []resourceSample) uint64 {
	if len(samples) == 0 {
		return 0
	}
	var total uint64
	for _, sample := range samples {
		total += sample.HeapBytes
	}
	return total / uint64(len(samples))
}

func (s *stats) snapshotLatencies() []time.Duration {
	s.latencyMu.Lock()
	defer s.latencyMu.Unlock()
	values := append([]time.Duration(nil), s.latencies...)
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return values
}

type errorEntry struct {
	message string
	count   int64
}

func (s *stats) errorSamplesSnapshot() []string {
	s.errorMu.Lock()
	defer s.errorMu.Unlock()
	return append([]string(nil), s.errorSamples...)
}

func (s *stats) topErrors(counts map[string]int64, limit int) []errorEntry {
	s.errorMu.Lock()
	defer s.errorMu.Unlock()
	entries := make([]errorEntry, 0, len(counts))
	for message, count := range counts {
		entries = append(entries, errorEntry{message: message, count: count})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].count == entries[j].count {
			return entries[i].message < entries[j].message
		}
		return entries[i].count > entries[j].count
	})
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries
}

func errorKey(stage string, profile string, path string, err error) string {
	if err == nil {
		return errorPrefix(stage, profile, path) + "<nil>"
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "cannot assign requested address"):
		return "dial: cannot assign requested address"
	case strings.Contains(message, "connection refused"):
		return "dial: connection refused"
	case strings.Contains(message, "i/o timeout"):
		return "i/o timeout"
	case strings.Contains(message, "connection reset by peer"):
		return "connection reset by peer"
	case strings.Contains(message, "broken pipe"):
		return "broken pipe"
	case strings.Contains(message, "unexpected EOF"):
		return "unexpected EOF"
	case strings.Contains(message, "EOF"):
		return "EOF"
	default:
		if len(message) > 160 {
			message = message[:160]
		}
	}
	return errorPrefix(stage, profile, path) + message
}

func errorPrefix(stage string, profile string, path string) string {
	if stage == "" {
		stage = "unknown"
	}
	if profile == "" {
		profile = "unknown"
	}
	if path == "" {
		return profile + " " + stage + ": "
	}
	return profile + " " + stage + " " + path + ": "
}

func errorSample(stage string, profile string, path string, index int, err error) string {
	message := "<nil>"
	if err != nil {
		message = err.Error()
		if len(message) > 220 {
			message = message[:220]
		}
	}
	if path == "" {
		return fmt.Sprintf("profile=%s user=%d stage=%s err=%s", profile, index+1, stage, message)
	}
	return fmt.Sprintf("profile=%s user=%d stage=%s path=%s err=%s", profile, index+1, stage, path, message)
}

func percentile(values []time.Duration, p int) time.Duration {
	if len(values) == 0 {
		return 0
	}
	index := (len(values)*p+99)/100 - 1
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index].Round(time.Millisecond)
}

func (p *resourcePeak) observe() {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	if memory.HeapAlloc > p.heapBytes {
		p.heapBytes = memory.HeapAlloc
	}
	if goroutines := runtime.NumGoroutine(); goroutines > p.goroutines {
		p.goroutines = goroutines
	}
	if rss := readRSS(); rss > p.rssBytes {
		p.rssBytes = rss
	}
}

func readRSS() uint64 {
	data, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return 0
	}
	pages, _ := strconv.ParseUint(fields[1], 10, 64)
	return pages * uint64(os.Getpagesize())
}

func readFDCount() int {
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return -1
	}
	return len(entries)
}

func readTCPStates() map[string]int {
	states := make(map[string]int)
	readTCPStatesFile(states, "/proc/net/tcp")
	readTCPStatesFile(states, "/proc/net/tcp6")
	return states
}

func readTCPStatesFile(states map[string]int, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		states[tcpStateName(fields[3])]++
	}
}

func tcpStateName(hexState string) string {
	switch hexState {
	case "01":
		return "ESTABLISHED"
	case "02":
		return "SYN_SENT"
	case "03":
		return "SYN_RECV"
	case "04":
		return "FIN_WAIT1"
	case "05":
		return "FIN_WAIT2"
	case "06":
		return "TIME_WAIT"
	case "07":
		return "CLOSE"
	case "08":
		return "CLOSE_WAIT"
	case "09":
		return "LAST_ACK"
	case "0A":
		return "LISTEN"
	case "0B":
		return "CLOSING"
	default:
		return hexState
	}
}

func readCPUSample() cpuSample {
	data, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return cpuSample{when: time.Now()}
	}
	closeIndex := strings.LastIndexByte(string(data), ')')
	if closeIndex < 0 {
		return cpuSample{when: time.Now()}
	}
	fields := strings.Fields(string(data)[closeIndex+2:])
	if len(fields) < 15 {
		return cpuSample{when: time.Now()}
	}
	utime, _ := strconv.ParseUint(fields[11], 10, 64)
	stime, _ := strconv.ParseUint(fields[12], 10, 64)
	return cpuSample{when: time.Now(), ticks: utime + stime}
}

func cpuPercent(start, end cpuSample) float64 {
	const ticksPerSecond = 100
	elapsed := end.when.Sub(start.when).Seconds()
	if elapsed <= 0 || end.ticks <= start.ticks {
		return 0
	}
	return float64(end.ticks-start.ticks) / ticksPerSecond / elapsed * 100
}

func queryInt(r *http.Request, name string, fallback int) int {
	value, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil || value < 0 {
		return fallback
	}
	return value
}

func writeBytes(w io.Writer, chunk []byte, size int) {
	remaining := size
	for remaining > 0 {
		n := min(remaining, len(chunk))
		if _, err := w.Write(chunk[:n]); err != nil {
			return
		}
		remaining -= n
	}
}

func clientMethod(method string, serverKey string, userKey string) (shadowsocks.Method, error) {
	if _, is2022 := methodKeyLength(method); is2022 {
		serverPSK, err := base64.StdEncoding.DecodeString(serverKey)
		if err != nil {
			return nil, err
		}
		userPSK, err := base64.StdEncoding.DecodeString(userKey)
		if err != nil {
			return nil, err
		}
		return shadowaead_2022.New(method, [][]byte{serverPSK, userPSK}, nil)
	}
	return shadowaead.New(method, nil, userKey)
}

func userPassword(index int, method string) string {
	if keyLength, is2022 := methodKeyLength(method); is2022 {
		return deterministicBase64Key(fmt.Sprintf("ss-loadtest-user-%d", index+1), keyLength)
	}
	return fmt.Sprintf("00000000-0000-0000-0000-%012d", index+1)
}

func deterministicBase64Key(seed string, length int) string {
	sum := sha256.Sum256([]byte(seed))
	return base64.StdEncoding.EncodeToString(sum[:length])
}

func methodKeyLength(method string) (int, bool) {
	switch method {
	case "2022-blake3-aes-128-gcm":
		return 16, true
	case "2022-blake3-aes-256-gcm":
		return 32, true
	default:
		return 0, false
	}
}

func pickFreePort() (uint16, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return uint16(listener.Addr().(*net.TCPAddr).Port), nil
}

func sleepJitter(ctx context.Context, maxDelay time.Duration) {
	if maxDelay <= 0 {
		return
	}
	sleepRange(ctx, 0, maxDelay)
}

func sleepRange(ctx context.Context, minDelay time.Duration, maxDelay time.Duration) {
	delay := minDelay
	if maxDelay > minDelay {
		delay += time.Duration(rand.Int64N(int64(maxDelay - minDelay)))
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func throttle(start time.Time, bytes int64, rate int64) {
	targetElapsed := time.Duration(bytes * int64(time.Second) / rate)
	if sleep := targetElapsed - time.Since(start); sleep > 0 {
		time.Sleep(min(sleep, 200*time.Millisecond))
	}
}

func lifecycleDuration(cfg config, oneDayDuration time.Duration) time.Duration {
	if !cfg.lifecycle || cfg.soakDay <= 0 {
		return oneDayDuration
	}
	scaled := time.Duration(float64(oneDayDuration) * cfg.soakDay.Seconds() / (24 * time.Hour).Seconds())
	if scaled < 100*time.Millisecond {
		return 100 * time.Millisecond
	}
	return scaled
}

func lifecycleRange(cfg config, minDuration time.Duration, maxDuration time.Duration) time.Duration {
	minScaled := lifecycleDuration(cfg, minDuration)
	maxScaled := lifecycleDuration(cfg, maxDuration)
	if maxScaled <= minScaled {
		return minScaled
	}
	return minScaled + time.Duration(rand.Int64N(int64(maxScaled-minScaled)))
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func formatBytes(value uint64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	div, exp := uint64(unit), 0
	for n := value / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %ciB", float64(value)/float64(div), "KMGTPE"[exp])
}

func percent(value int64, total int64) float64 {
	return float64(value) * 100 / float64(total)
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
