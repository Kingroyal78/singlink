package v2board

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/singlink/singlink/adapter"
	"github.com/singlink/singlink/log"
	"github.com/singlink/singlink/option"
)

func TestApplyPanelIntervalsAllowsZeroTrafficThresholds(t *testing.T) {
	controller := &Controller{
		options: effectiveNodeOptions{
			PullInterval:           time.Minute,
			PushInterval:           time.Minute,
			NodeReportMinTraffic:   1024,
			DeviceOnlineMinTraffic: 2048,
		},
	}

	controller.applyPanelIntervals(&NodeInfo{
		PullInterval:           2 * time.Minute,
		PushInterval:           3 * time.Minute,
		NodeReportMinTraffic:   0,
		DeviceOnlineMinTraffic: 0,
		Common: &ServerConfig{
			BaseConfig: &ServerBaseConfig{},
		},
	})

	if controller.options.PullInterval != 2*time.Minute || controller.options.PushInterval != 3*time.Minute {
		t.Fatalf("unexpected intervals: %#v", controller.options)
	}
	if controller.options.NodeReportMinTraffic != 0 || controller.options.DeviceOnlineMinTraffic != 0 {
		t.Fatalf("zero thresholds were not applied: %#v", controller.options)
	}
}

func TestApplyPanelIntervalsClampsSmallIntervals(t *testing.T) {
	controller := &Controller{}
	controller.applyPanelIntervals(&NodeInfo{
		PullInterval: time.Millisecond,
		PushInterval: 2 * time.Millisecond,
	})

	if controller.options.PullInterval != minimumInterval || controller.options.PushInterval != minimumInterval {
		t.Fatalf("small panel intervals were not clamped: %#v", controller.options)
	}
}

func TestApplyInboundDoesNotRemoveCurrentOnValidationFailure(t *testing.T) {
	createErr := errors.New("invalid tls")
	manager := &fakeInboundManager{createErrors: []error{createErr}}
	controller := &Controller{
		ctx:          context.Background(),
		inbound:      manager,
		options:      effectiveNodeOptions{Tag: "node"},
		inboundReady: true,
		current:      option.Inbound{Type: "old", Options: "old-options"},
	}

	err := controller.applyInbound(option.Inbound{Type: "new", Options: "new-options"})
	if !errors.Is(err, createErr) {
		t.Fatalf("unexpected error: %v", err)
	}
	if manager.removeCalls != 0 {
		t.Fatalf("current inbound was removed after validation failure")
	}
	if controller.current.Type != "old" {
		t.Fatalf("current inbound changed unexpectedly: %#v", controller.current)
	}
}

func TestApplyInboundRestoresCurrentAfterReplacementFailure(t *testing.T) {
	listenErr := errors.New("listen tcp :443: bind: address already in use")
	replaceErr := errors.New("invalid tls")
	manager := &fakeInboundManager{createErrors: []error{listenErr, replaceErr, nil}}
	controller := &Controller{
		ctx:          context.Background(),
		inbound:      manager,
		options:      effectiveNodeOptions{Tag: "node"},
		inboundReady: true,
		current:      option.Inbound{Type: "old", Options: "old-options"},
	}

	err := controller.applyInbound(option.Inbound{Type: "new", Options: "new-options"})
	if !errors.Is(err, replaceErr) {
		t.Fatalf("unexpected error: %v", err)
	}
	if manager.removeCalls != 1 {
		t.Fatalf("expected one remove call, got %d", manager.removeCalls)
	}
	if got := manager.createTypes; len(got) != 3 || got[0] != "new" || got[1] != "new" || got[2] != "old" {
		t.Fatalf("unexpected create sequence: %#v", got)
	}
	if controller.current.Type != "old" {
		t.Fatalf("current inbound should remain old after failed replacement: %#v", controller.current)
	}
}

func TestRebuildInboundClearsExistingInboundWhenUserListIsEmpty(t *testing.T) {
	const user = "00000000-0000-0000-0000-000000000007"
	manager := &fakeInboundManager{}
	tracker := &TrafficTracker{nodes: make(map[string]*nodeTraffic)}
	tracker.UpdateNode("node", 23, []UserInfo{{
		ID:   7,
		UUID: user,
	}}, nil, nodeRules{})
	tracker.nodes["node"].counters[user].upload.Add(10)
	controller := &Controller{
		ctx:          context.Background(),
		inbound:      manager,
		tracker:      tracker,
		options:      effectiveNodeOptions{Tag: "node", NodeID: 23},
		node:         &NodeInfo{Common: &ServerConfig{}},
		current:      option.Inbound{Type: "old", Options: "old-options"},
		inboundReady: true,
	}

	if err := controller.rebuildInbound(); err != nil {
		t.Fatal(err)
	}
	if manager.removeCalls != 1 {
		t.Fatalf("expected existing inbound to be removed, got %d remove calls", manager.removeCalls)
	}
	if len(manager.createTypes) != 0 {
		t.Fatalf("empty user list should not create a replacement inbound: %#v", manager.createTypes)
	}
	if controller.current.Type != "" {
		t.Fatalf("current inbound was not cleared: %#v", controller.current)
	}
	traffic, _ := tracker.Snapshot("node", 0, 0)
	if len(traffic) != 1 || traffic[0].UID != 7 || traffic[0].Upload != 10 {
		t.Fatalf("pending traffic was not preserved: %#v", traffic)
	}
}

func TestSyncClearsShadowsocksTidalabInboundWhenUserListIsEmpty(t *testing.T) {
	const user = "00000000-0000-0000-0000-000000000007"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/server/ShadowsocksTidalab/user" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		_, _ = writer.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()
	client := newTestClientWithStyle(t, server.URL, APIStyleShadowsocksTidalab, "shadowsocks")
	manager := &fakeInboundManager{}
	tracker := &TrafficTracker{nodes: make(map[string]*nodeTraffic)}
	tracker.UpdateNode("node", 23, []UserInfo{{
		ID:   7,
		UUID: user,
	}}, nil, nodeRules{})
	controller := &Controller{
		ctx:          context.Background(),
		inbound:      manager,
		tracker:      tracker,
		client:       client,
		options:      effectiveNodeOptions{Tag: "node", NodeID: 23},
		node:         &NodeInfo{Common: &ServerConfig{}},
		current:      option.Inbound{Type: "old", Options: "old-options"},
		inboundReady: true,
		users:        []UserInfo{{ID: 7, UUID: user}},
	}

	if err := controller.sync(); err != nil {
		t.Fatal(err)
	}
	if manager.removeCalls != 1 {
		t.Fatalf("expected existing inbound to be removed, got %d remove calls", manager.removeCalls)
	}
	if controller.current.Type != "" {
		t.Fatalf("current inbound was not cleared: %#v", controller.current)
	}
}

func TestSyncStableConfigWithoutETagDoesNotRebuildRepeatedly(t *testing.T) {
	var aliveCount int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case strings.HasSuffix(request.URL.Path, "/config"):
			_, _ = writer.Write([]byte(`{"protocol":"vless","server_port":12345,"tls":0,"network":"tcp","routes":[]}`))
		case strings.HasSuffix(request.URL.Path, "/user"):
			_, _ = writer.Write([]byte(`{"users":[{"id":7,"uuid":"00000000-0000-0000-0000-000000000007"}]}`))
		case strings.HasSuffix(request.URL.Path, "/alivelist"):
			aliveCount++
			_, _ = writer.Write([]byte(`{"alive":{"7":1}}`))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	manager := &fakeInboundManager{}
	tracker := &TrafficTracker{nodes: make(map[string]*nodeTraffic)}
	controller := &Controller{
		ctx:     context.Background(),
		logger:  log.NewNOPFactory().Logger(),
		inbound: manager,
		tracker: tracker,
		client:  client,
		options: effectiveNodeOptions{Tag: "node", NodeID: 23},
	}

	for i := 0; i < 100; i++ {
		if err := controller.sync(); err != nil {
			t.Fatal(err)
		}
	}
	if len(manager.createTypes) != 1 {
		t.Fatalf("stable config should create inbound once, got %d creates", len(manager.createTypes))
	}
	if aliveCount != 100 {
		t.Fatalf("expected alive endpoint to stay polled, got %d", aliveCount)
	}
}

func TestSyncAliveListFailureDoesNotBlockInboundUpdate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case strings.HasSuffix(request.URL.Path, "/config"):
			_, _ = writer.Write([]byte(`{"protocol":"vless","server_port":12345,"tls":0,"network":"tcp","routes":[]}`))
		case strings.HasSuffix(request.URL.Path, "/user"):
			_, _ = writer.Write([]byte(`{"users":[{"id":7,"uuid":"00000000-0000-0000-0000-000000000007"}]}`))
		case strings.HasSuffix(request.URL.Path, "/alivelist"):
			writer.WriteHeader(http.StatusBadGateway)
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	manager := &fakeInboundManager{}
	controller := &Controller{
		ctx:     context.Background(),
		logger:  log.NewNOPFactory().Logger(),
		inbound: manager,
		tracker: &TrafficTracker{nodes: make(map[string]*nodeTraffic)},
		client:  client,
		options: effectiveNodeOptions{Tag: "node", NodeID: 23},
	}

	if err := controller.sync(); err != nil {
		t.Fatal(err)
	}
	if len(manager.createTypes) != 1 {
		t.Fatalf("alive failure should not block inbound create: %#v", manager.createTypes)
	}
}

func TestSyncAliveOnlyChangeUpdatesTrackerWithoutRebuild(t *testing.T) {
	aliveValue := 1
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case strings.HasSuffix(request.URL.Path, "/config"):
			_, _ = writer.Write([]byte(`{"protocol":"vless","server_port":12345,"tls":0,"network":"tcp","routes":[]}`))
		case strings.HasSuffix(request.URL.Path, "/user"):
			_, _ = writer.Write([]byte(`{"users":[{"id":7,"uuid":"00000000-0000-0000-0000-000000000007"}]}`))
		case strings.HasSuffix(request.URL.Path, "/alivelist"):
			_, _ = writer.Write([]byte(`{"alive":{"7":` + strconv.Itoa(aliveValue) + `}}`))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	manager := &fakeInboundManager{}
	tracker := &TrafficTracker{nodes: make(map[string]*nodeTraffic)}
	controller := &Controller{
		ctx:     context.Background(),
		logger:  log.NewNOPFactory().Logger(),
		inbound: manager,
		tracker: tracker,
		client:  client,
		options: effectiveNodeOptions{Tag: "node", NodeID: 23},
	}

	if err := controller.sync(); err != nil {
		t.Fatal(err)
	}
	aliveValue = 2
	if err := controller.sync(); err != nil {
		t.Fatal(err)
	}
	if len(manager.createTypes) != 1 {
		t.Fatalf("alive-only change should not rebuild inbound: %#v", manager.createTypes)
	}
	if got := tracker.nodes["node"].alive[7]; got != 2 {
		t.Fatalf("alive list was not updated, got %d", got)
	}
}

func TestSyncFiltersInactivePanelUsers(t *testing.T) {
	const activeUser = "00000000-0000-0000-0000-000000000001"
	expiredAt := strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case strings.HasSuffix(request.URL.Path, "/config"):
			_, _ = writer.Write([]byte(`{"protocol":"vless","server_port":12345,"tls":0,"network":"tcp","routes":[]}`))
		case strings.HasSuffix(request.URL.Path, "/user"):
			_, _ = writer.Write([]byte(`{"users":[
				{"id":1,"uuid":"` + activeUser + `"},
				{"id":2,"uuid":"00000000-0000-0000-0000-000000000002","enabled":false},
				{"id":3,"uuid":"00000000-0000-0000-0000-000000000003","expires_at":` + expiredAt + `}
			]}`))
		case strings.HasSuffix(request.URL.Path, "/alivelist"):
			_, _ = writer.Write([]byte(`{"alive":{}}`))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	manager := &fakeInboundManager{}
	tracker := &TrafficTracker{nodes: make(map[string]*nodeTraffic)}
	controller := &Controller{
		ctx:     context.Background(),
		logger:  log.NewNOPFactory().Logger(),
		inbound: manager,
		tracker: tracker,
		client:  client,
		options: effectiveNodeOptions{Tag: "node", NodeID: 23, PullInterval: time.Minute},
	}

	if err := controller.sync(); err != nil {
		t.Fatal(err)
	}
	if len(manager.createOptions) != 1 {
		t.Fatalf("expected one inbound create, got %d", len(manager.createOptions))
	}
	options, ok := manager.createOptions[0].(*option.VLESSInboundOptions)
	if !ok {
		t.Fatalf("unexpected inbound options type %T", manager.createOptions[0])
	}
	if len(options.Users) != 1 || options.Users[0].UUID != activeUser {
		t.Fatalf("inactive users leaked into inbound: %#v", options.Users)
	}
	state := tracker.nodes["node"]
	if state == nil {
		t.Fatal("missing tracker state")
	}
	if _, loaded := state.users[activeUser]; !loaded || len(state.users) != 1 {
		t.Fatalf("inactive users leaked into tracker: %#v", state.users)
	}
}

func TestActivePanelUsersChangesWhenUserExpires(t *testing.T) {
	now := time.Unix(2_000, 0)
	users := []UserInfo{{
		ID:        1,
		UUID:      "00000000-0000-0000-0000-000000000001",
		ExpiresAt: now.Add(time.Minute).Unix(),
	}}

	if active := activePanelUsers(users, now); len(active) != 1 {
		t.Fatalf("future expiry should stay active: %#v", active)
	}
	if active := activePanelUsers(users, now.Add(2*time.Minute)); len(active) != 0 {
		t.Fatalf("expired user should be filtered: %#v", active)
	}
}

func TestControllerBackoffIncreasesAndResets(t *testing.T) {
	var backoff controllerBackoff
	first := backoff.Next(10*time.Second, errors.New("boom"), "node/pull")
	second := backoff.Next(10*time.Second, errors.New("boom"), "node/pull")
	if first < 8*time.Second || first > 12*time.Second {
		t.Fatalf("first backoff outside jitter range: %s", first)
	}
	if second < 16*time.Second || second > 24*time.Second {
		t.Fatalf("second backoff outside jitter range: %s", second)
	}
	if reset := backoff.Next(10*time.Second, nil, "node/pull"); reset != 10*time.Second {
		t.Fatalf("success did not reset backoff: %s", reset)
	}
}

func TestPushRestoresAliveWhenTrafficReportFails(t *testing.T) {
	const user = "00000000-0000-0000-0000-000000000007"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !strings.HasSuffix(request.URL.Path, "/push") {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		writer.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	client, err := NewClient(Options{
		APIHost: server.URL,
		NodeConfig: NodeConfig{
			NodeID:   23,
			NodeType: "vless",
			Token:    "secret",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	tracker := &TrafficTracker{nodes: make(map[string]*nodeTraffic)}
	tracker.UpdateNode("node", 23, []UserInfo{{
		ID:   7,
		UUID: user,
	}}, nil, nodeRules{})
	state := tracker.nodes["node"]
	state.markOnline(user, "192.0.2.1")

	controller := &Controller{
		ctx:          context.Background(),
		tracker:      tracker,
		client:       client,
		options:      effectiveNodeOptions{Tag: "node"},
		inboundReady: true,
	}
	if err := controller.push(); err == nil {
		t.Fatal("expected push error")
	}
	_, alive := tracker.Snapshot("node", 0, 0)
	if got := alive[7]; len(got) != 1 || got[0] != "192.0.2.1_23" {
		t.Fatalf("alive state was not restored: %#v", alive)
	}
}

type fakeInboundManager struct {
	createErrors  []error
	createTypes   []string
	createOptions []any
	removeCalls   int
	existingTags  map[string]adapter.Inbound
}

func (m *fakeInboundManager) Start(adapter.StartStage) error {
	return nil
}

func (m *fakeInboundManager) Close() error {
	return nil
}

func (m *fakeInboundManager) Inbounds() []adapter.Inbound {
	return nil
}

func (m *fakeInboundManager) Get(tag string) (adapter.Inbound, bool) {
	if m.existingTags == nil {
		return nil, false
	}
	inbound, loaded := m.existingTags[tag]
	return inbound, loaded
}

func (m *fakeInboundManager) Remove(string) error {
	m.removeCalls++
	return nil
}

func (m *fakeInboundManager) Create(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, inboundType string, options any) error {
	m.createTypes = append(m.createTypes, inboundType)
	m.createOptions = append(m.createOptions, options)
	if len(m.createErrors) == 0 {
		return nil
	}
	err := m.createErrors[0]
	m.createErrors = m.createErrors[1:]
	return err
}

type fakeInbound struct {
	tag string
}

func (i fakeInbound) Start(adapter.StartStage) error {
	return nil
}

func (i fakeInbound) Close() error {
	return nil
}

func (i fakeInbound) Type() string {
	return "fake"
}

func (i fakeInbound) Tag() string {
	return i.tag
}
