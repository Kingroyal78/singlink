package v2board

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/singlink/singlink/adapter"
	C "github.com/singlink/singlink/constant"
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
		statusReset: make(chan struct{}, 1),
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
	select {
	case <-controller.statusReset:
	default:
		t.Fatal("push interval change did not request a status timer reset")
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

func TestApplyInboundRestoreFailureClearsAppliedStatus(t *testing.T) {
	var payload NodeStatus
	var statusCalls int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		statusCalls++
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()

	listenErr := errors.New("listen tcp :443: bind: address already in use")
	replaceErr := errors.New("invalid tls")
	restoreErr := errors.New("restore failed")
	manager := &fakeInboundManager{createErrors: []error{listenErr, replaceErr, restoreErr}}
	controller := &Controller{
		ctx:             context.Background(),
		logger:          log.NewNOPFactory().Logger(),
		inbound:         manager,
		client:          newTestClientWithStyle(t, server.URL, APIStyleUniProxy, C.TypeShadowsocks),
		options:         effectiveNodeOptions{Tag: "node"},
		node:            &NodeInfo{Type: C.TypeShadowsocks, Common: &ServerConfig{ConfigRevision: "sha256:abcdef"}},
		inboundReady:    true,
		statusReady:     true,
		appliedRevision: "sha256:abcdef",
		appliedFeatures: append([]string(nil), shadowsocksAppliedFeatures...),
		current:         option.Inbound{Type: "old", Options: "old-options"},
	}

	err := controller.applyInbound(option.Inbound{Type: "new", Options: "new-options"})
	if !errors.Is(err, restoreErr) {
		t.Fatalf("unexpected error: %v", err)
	}
	if controller.inboundReady || controller.statusReady || controller.appliedRevision != "" || controller.current.Type != "" {
		t.Fatalf("failed restore left a ready inbound status: %#v", controller)
	}
	if statusCalls != 1 || payload.Ready || payload.AppliedRevision != "" || len(payload.AppliedFeatures) != 0 {
		t.Fatalf("unexpected not-ready status report: calls=%d payload=%#v", statusCalls, payload)
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

	for range 100 {
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

func TestSyncUserFailureReportsInitialShadowsocksNotReady(t *testing.T) {
	var payload NodeStatus
	var statusCalls int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case strings.HasSuffix(request.URL.Path, "/config"):
			_, _ = writer.Write([]byte(`{
				"protocol":"shadowsocks",
				"server_port":8388,
				"cipher":"aes-128-gcm",
				"config_revision":"sha256:abcdef"
			}`))
		case strings.HasSuffix(request.URL.Path, "/user"):
			writer.WriteHeader(http.StatusBadGateway)
		case strings.HasSuffix(request.URL.Path, "/status"):
			statusCalls++
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	controller := &Controller{
		ctx:     context.Background(),
		logger:  log.NewNOPFactory().Logger(),
		inbound: &fakeInboundManager{},
		tracker: &TrafficTracker{nodes: make(map[string]*nodeTraffic)},
		client:  newTestClientWithStyle(t, server.URL, APIStyleUniProxy, C.TypeShadowsocks),
		options: effectiveNodeOptions{Tag: "node", NodeID: 23},
	}

	if err := controller.sync(); err == nil {
		t.Fatal("expected user list failure")
	}
	if statusCalls != 1 || payload.Ready || payload.AppliedRevision != "" || len(payload.AppliedFeatures) != 0 {
		t.Fatalf("user failure did not report initial not-ready state: calls=%d payload=%#v", statusCalls, payload)
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

func TestRebuildInboundReportsAppliedShadowsocksStatusImmediatelyBestEffort(t *testing.T) {
	var payload NodeStatus
	var statusCalls int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/server/UniProxy/status" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		statusCalls++
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		writer.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	client := newTestClientWithStyle(t, server.URL, APIStyleUniProxy, C.TypeShadowsocks)
	controller := &Controller{
		ctx:     context.Background(),
		logger:  log.NewNOPFactory().Logger(),
		inbound: &fakeInboundManager{},
		tracker: &TrafficTracker{nodes: make(map[string]*nodeTraffic)},
		client:  client,
		options: effectiveNodeOptions{Tag: "node", NodeID: 23},
		node: &NodeInfo{
			Type: C.TypeShadowsocks,
			Common: &ServerConfig{
				Protocol:       C.TypeShadowsocks,
				ConfigRevision: "sha256:abcdef",
				ServerPort:     8388,
				Cipher:         "aes-128-gcm",
			},
		},
		activeUsers: []UserInfo{{
			ID:   7,
			UUID: "00000000-0000-0000-0000-000000000007",
		}},
	}

	if err := controller.rebuildInbound(); err != nil {
		t.Fatal(err)
	}
	if statusCalls != 1 {
		t.Fatalf("expected immediate status report, got %d calls", statusCalls)
	}
	if !payload.Ready || payload.AppliedRevision != "sha256:abcdef" || payload.Version != C.Version {
		t.Fatalf("unexpected status payload: %#v", payload)
	}
	if len(payload.AppliedFeatures) != 3 ||
		payload.AppliedFeatures[0] != "shadowsocks-uot-v1" ||
		payload.AppliedFeatures[1] != "shadowsocks-uot-v2" ||
		payload.AppliedFeatures[2] != "shadowsocks-sing-mux-v1" {
		t.Fatalf("unexpected applied features: %#v", payload.AppliedFeatures)
	}
}

func TestRebuildInboundFailureDoesNotAcknowledgeRevision(t *testing.T) {
	var statusCalls int
	var payload NodeStatus
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		statusCalls++
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()

	applyErr := errors.New("invalid shadowsocks config")
	controller := &Controller{
		ctx:     context.Background(),
		logger:  log.NewNOPFactory().Logger(),
		inbound: &fakeInboundManager{createErrors: []error{applyErr}},
		tracker: &TrafficTracker{nodes: make(map[string]*nodeTraffic)},
		client:  newTestClientWithStyle(t, server.URL, APIStyleUniProxy, C.TypeShadowsocks),
		options: effectiveNodeOptions{Tag: "node", NodeID: 23},
		node: &NodeInfo{
			Type: C.TypeShadowsocks,
			Common: &ServerConfig{
				Protocol:       C.TypeShadowsocks,
				ConfigRevision: "sha256:new",
				ServerPort:     8388,
				Cipher:         "aes-128-gcm",
			},
		},
		activeUsers: []UserInfo{{
			ID:   7,
			UUID: "00000000-0000-0000-0000-000000000007",
		}},
		current:         option.Inbound{Type: C.TypeShadowsocks, Options: "old-options"},
		inboundReady:    true,
		statusReady:     true,
		appliedRevision: "sha256:old",
		appliedFeatures: append([]string(nil), shadowsocksAppliedFeatures...),
	}

	if err := controller.rebuildInbound(); !errors.Is(err, applyErr) {
		t.Fatalf("unexpected error: %v", err)
	}
	if statusCalls != 0 {
		t.Fatalf("failed apply must not report status, got %d calls", statusCalls)
	}
	if controller.appliedRevision != "sha256:old" || len(controller.appliedFeatures) != 3 || !controller.statusReady {
		t.Fatalf("failed apply replaced last successful status: revision=%q features=%#v ready=%v", controller.appliedRevision, controller.appliedFeatures, controller.statusReady)
	}
	controller.reportStatusBestEffort()
	if statusCalls != 1 || payload.AppliedRevision != "sha256:old" || !payload.Ready {
		t.Fatalf("failed apply acknowledged the new revision: calls=%d payload=%#v", statusCalls, payload)
	}
}

func TestInitialRebuildFailureReportsNotReadyImmediately(t *testing.T) {
	var payload NodeStatus
	var statusCalls int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		statusCalls++
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()

	applyErr := errors.New("invalid shadowsocks config")
	controller := &Controller{
		ctx:     context.Background(),
		logger:  log.NewNOPFactory().Logger(),
		inbound: &fakeInboundManager{createErrors: []error{applyErr}},
		tracker: &TrafficTracker{nodes: make(map[string]*nodeTraffic)},
		client:  newTestClientWithStyle(t, server.URL, APIStyleUniProxy, C.TypeShadowsocks),
		options: effectiveNodeOptions{Tag: "node", NodeID: 23},
		node: &NodeInfo{
			Type: C.TypeShadowsocks,
			Common: &ServerConfig{
				Protocol:       C.TypeShadowsocks,
				ConfigRevision: "sha256:new",
				ServerPort:     8388,
				Cipher:         "aes-128-gcm",
			},
		},
		activeUsers: []UserInfo{{
			ID:   7,
			UUID: "00000000-0000-0000-0000-000000000007",
		}},
	}

	if err := controller.rebuildInbound(); !errors.Is(err, applyErr) {
		t.Fatalf("unexpected error: %v", err)
	}
	if statusCalls != 1 || payload.Ready || payload.AppliedRevision != "" || len(payload.AppliedFeatures) != 0 {
		t.Fatalf("unavailable initial inbound did not report not-ready: calls=%d payload=%#v", statusCalls, payload)
	}
}

func TestFailedRemovalAndRestoreClearsAppliedStatus(t *testing.T) {
	var payload NodeStatus
	var statusCalls int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		statusCalls++
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()

	listenErr := errors.New("listen tcp :8388: bind: address already in use")
	removeErr := errors.New("close old inbound failed")
	restoreErr := errors.New("restore old inbound failed")
	manager := &fakeInboundManager{
		createErrors:  []error{listenErr, restoreErr},
		removeErr:     removeErr,
		removeDeletes: true,
		existingTags:  map[string]adapter.Inbound{"node": fakeInbound{tag: "node"}},
	}
	controller := &Controller{
		ctx:             context.Background(),
		logger:          log.NewNOPFactory().Logger(),
		inbound:         manager,
		tracker:         &TrafficTracker{nodes: make(map[string]*nodeTraffic)},
		client:          newTestClientWithStyle(t, server.URL, APIStyleUniProxy, C.TypeShadowsocks),
		options:         effectiveNodeOptions{Tag: "node"},
		node:            &NodeInfo{Type: C.TypeShadowsocks, Common: &ServerConfig{ConfigRevision: "sha256:old"}},
		inboundReady:    true,
		statusReady:     true,
		appliedRevision: "sha256:old",
		appliedFeatures: append([]string(nil), shadowsocksAppliedFeatures...),
		current:         option.Inbound{Type: C.TypeShadowsocks, Options: "old-options"},
	}

	err := controller.applyInbound(option.Inbound{Type: C.TypeShadowsocks, Options: "new-options"})
	if !errors.Is(err, removeErr) || !errors.Is(err, restoreErr) {
		t.Fatalf("unexpected replacement error: %v", err)
	}
	if controller.inboundReady || controller.current.Type != "" || controller.statusReady {
		t.Fatalf("failed removal/restore kept a false ready state: %#v", controller)
	}
	if statusCalls != 1 || payload.Ready || payload.AppliedRevision != "" {
		t.Fatalf("failed removal/restore did not report not-ready: calls=%d payload=%#v", statusCalls, payload)
	}
}

func TestFailedRemovalWithExistingInboundKeepsAppliedStatus(t *testing.T) {
	var statusCalls int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		statusCalls++
	}))
	defer server.Close()

	listenErr := errors.New("listen tcp :8388: bind: address already in use")
	removeErr := errors.New("close old inbound failed")
	manager := &fakeInboundManager{
		createErrors: []error{listenErr},
		removeErr:    removeErr,
		existingTags: map[string]adapter.Inbound{"node": fakeInbound{tag: "node"}},
	}
	controller := &Controller{
		ctx:             context.Background(),
		logger:          log.NewNOPFactory().Logger(),
		inbound:         manager,
		client:          newTestClientWithStyle(t, server.URL, APIStyleUniProxy, C.TypeShadowsocks),
		options:         effectiveNodeOptions{Tag: "node"},
		node:            &NodeInfo{Type: C.TypeShadowsocks, Common: &ServerConfig{ConfigRevision: "sha256:old"}},
		inboundReady:    true,
		statusReady:     true,
		appliedRevision: "sha256:old",
		appliedFeatures: append([]string(nil), shadowsocksAppliedFeatures...),
		current:         option.Inbound{Type: C.TypeShadowsocks, Options: "old-options"},
	}

	err := controller.applyInbound(option.Inbound{Type: C.TypeShadowsocks, Options: "new-options"})
	if !errors.Is(err, listenErr) || !errors.Is(err, removeErr) {
		t.Fatalf("unexpected replacement error: %v", err)
	}
	if len(manager.createOptions) != 1 {
		t.Fatalf("existing inbound should not be restored over itself: %#v", manager.createOptions)
	}
	if _, loaded := manager.Get("node"); !loaded {
		t.Fatal("remove error unexpectedly lost the existing inbound")
	}
	if !controller.inboundReady || !controller.statusReady || controller.appliedRevision != "sha256:old" || controller.current.Options != "old-options" {
		t.Fatalf("remove error discarded the live inbound status: %#v", controller)
	}
	if statusCalls != 0 {
		t.Fatalf("unchanged live inbound should not report not-ready, got %d calls", statusCalls)
	}
}

func TestFailedRemovalAfterDeletionRestoresAndKeepsAppliedStatus(t *testing.T) {
	var statusCalls int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		statusCalls++
	}))
	defer server.Close()

	listenErr := errors.New("listen tcp :8388: bind: address already in use")
	removeErr := errors.New("close old inbound failed")
	manager := &fakeInboundManager{
		createErrors:  []error{listenErr, nil},
		removeErr:     removeErr,
		removeDeletes: true,
		existingTags:  map[string]adapter.Inbound{"node": fakeInbound{tag: "node"}},
	}
	controller := &Controller{
		ctx:             context.Background(),
		logger:          log.NewNOPFactory().Logger(),
		inbound:         manager,
		client:          newTestClientWithStyle(t, server.URL, APIStyleUniProxy, C.TypeShadowsocks),
		options:         effectiveNodeOptions{Tag: "node"},
		node:            &NodeInfo{Type: C.TypeShadowsocks, Common: &ServerConfig{ConfigRevision: "sha256:old"}},
		inboundReady:    true,
		statusReady:     true,
		appliedRevision: "sha256:old",
		appliedFeatures: append([]string(nil), shadowsocksAppliedFeatures...),
		current:         option.Inbound{Type: C.TypeShadowsocks, Options: "old-options"},
	}

	err := controller.applyInbound(option.Inbound{Type: C.TypeShadowsocks, Options: "new-options"})
	if !errors.Is(err, listenErr) || !errors.Is(err, removeErr) {
		t.Fatalf("unexpected replacement error: %v", err)
	}
	if len(manager.createOptions) != 2 || manager.createOptions[1] != "old-options" {
		t.Fatalf("previous inbound was not restored: %#v", manager.createOptions)
	}
	if _, loaded := manager.Get("node"); !loaded {
		t.Fatal("successfully restored inbound is missing")
	}
	if !controller.inboundReady || !controller.statusReady || controller.appliedRevision != "sha256:old" || controller.current.Options != "old-options" {
		t.Fatalf("successful restore discarded the applied status: %#v", controller)
	}
	if statusCalls != 0 {
		t.Fatalf("restored live inbound should not report not-ready, got %d calls", statusCalls)
	}
}

func TestClearInboundRemoveErrorAfterDeletionReportsNotReady(t *testing.T) {
	var payload NodeStatus
	var statusCalls int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		statusCalls++
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()

	removeErr := errors.New("close cleared inbound failed")
	manager := &fakeInboundManager{
		removeErr:     removeErr,
		removeDeletes: true,
		existingTags:  map[string]adapter.Inbound{"node": fakeInbound{tag: "node"}},
	}
	controller := &Controller{
		ctx:             context.Background(),
		logger:          log.NewNOPFactory().Logger(),
		inbound:         manager,
		tracker:         &TrafficTracker{nodes: make(map[string]*nodeTraffic)},
		client:          newTestClientWithStyle(t, server.URL, APIStyleUniProxy, C.TypeShadowsocks),
		options:         effectiveNodeOptions{Tag: "node"},
		node:            &NodeInfo{Type: C.TypeShadowsocks, Common: &ServerConfig{ConfigRevision: "sha256:old"}},
		inboundReady:    true,
		statusReady:     true,
		appliedRevision: "sha256:old",
		appliedFeatures: append([]string(nil), shadowsocksAppliedFeatures...),
		current:         option.Inbound{Type: C.TypeShadowsocks, Options: "old-options"},
	}

	if err := controller.clearInbound(nodeRules{}); !errors.Is(err, removeErr) {
		t.Fatalf("unexpected clear error: %v", err)
	}
	if controller.inboundReady || controller.current.Type != "" || controller.statusReady {
		t.Fatalf("failed clear removal kept a false ready state: %#v", controller)
	}
	if statusCalls != 1 || payload.Ready || payload.AppliedRevision != "" {
		t.Fatalf("failed clear removal did not report not-ready: calls=%d payload=%#v", statusCalls, payload)
	}
}

func TestClearInboundRemoveErrorWithExistingInboundKeepsAppliedStatus(t *testing.T) {
	var statusCalls int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		statusCalls++
	}))
	defer server.Close()

	removeErr := errors.New("close cleared inbound failed")
	manager := &fakeInboundManager{
		removeErr:    removeErr,
		existingTags: map[string]adapter.Inbound{"node": fakeInbound{tag: "node"}},
	}
	controller := &Controller{
		ctx:             context.Background(),
		logger:          log.NewNOPFactory().Logger(),
		inbound:         manager,
		tracker:         &TrafficTracker{nodes: make(map[string]*nodeTraffic)},
		client:          newTestClientWithStyle(t, server.URL, APIStyleUniProxy, C.TypeShadowsocks),
		options:         effectiveNodeOptions{Tag: "node"},
		node:            &NodeInfo{Type: C.TypeShadowsocks, Common: &ServerConfig{ConfigRevision: "sha256:old"}},
		inboundReady:    true,
		statusReady:     true,
		appliedRevision: "sha256:old",
		appliedFeatures: append([]string(nil), shadowsocksAppliedFeatures...),
		current:         option.Inbound{Type: C.TypeShadowsocks, Options: "old-options"},
	}

	if err := controller.clearInbound(nodeRules{}); !errors.Is(err, removeErr) {
		t.Fatalf("unexpected clear error: %v", err)
	}
	if _, loaded := manager.Get("node"); !loaded {
		t.Fatal("remove error unexpectedly lost the existing inbound")
	}
	if !controller.inboundReady || !controller.statusReady || controller.appliedRevision != "sha256:old" || controller.current.Options != "old-options" {
		t.Fatalf("remove error discarded the live inbound status: %#v", controller)
	}
	if statusCalls != 0 {
		t.Fatalf("unchanged live inbound should not report not-ready, got %d calls", statusCalls)
	}
}

func TestClearInboundReportsNotReadyWithoutAppliedRevision(t *testing.T) {
	var payload NodeStatus
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/server/UniProxy/status" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()

	controller := &Controller{
		ctx:          context.Background(),
		inbound:      &fakeInboundManager{},
		tracker:      &TrafficTracker{nodes: make(map[string]*nodeTraffic)},
		client:       newTestClientWithStyle(t, server.URL, APIStyleUniProxy, C.TypeShadowsocks),
		options:      effectiveNodeOptions{Tag: "node", NodeID: 23},
		node:         &NodeInfo{Type: C.TypeShadowsocks, Common: &ServerConfig{ConfigRevision: "sha256:abcdef"}},
		current:      option.Inbound{Type: C.TypeShadowsocks, Options: "old-options"},
		inboundReady: true,
	}

	if err := controller.rebuildInbound(); err != nil {
		t.Fatal(err)
	}
	if payload.Ready || payload.AppliedRevision != "" || payload.Version != C.Version {
		t.Fatalf("unexpected cleared status payload: %#v", payload)
	}
	if payload.AppliedFeatures == nil || len(payload.AppliedFeatures) != 0 {
		t.Fatalf("cleared status must contain an empty feature list: %#v", payload.AppliedFeatures)
	}
}

func TestStatusFailureDoesNotAffectTrafficAndAlivePush(t *testing.T) {
	var statusCalls int
	var trafficCalls int
	var aliveCalls int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/server/UniProxy/status":
			statusCalls++
			writer.WriteHeader(http.StatusBadGateway)
		case "/api/v1/server/UniProxy/push":
			trafficCalls++
		case "/api/v1/server/UniProxy/alive":
			aliveCalls++
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	tracker := &TrafficTracker{nodes: make(map[string]*nodeTraffic)}
	tracker.UpdateNode("node", 23, []UserInfo{{
		ID:   7,
		UUID: "00000000-0000-0000-0000-000000000007",
	}}, nil, nodeRules{})
	controller := &Controller{
		ctx:             context.Background(),
		logger:          log.NewNOPFactory().Logger(),
		tracker:         tracker,
		client:          newTestClientWithStyle(t, server.URL, APIStyleUniProxy, C.TypeShadowsocks),
		options:         effectiveNodeOptions{Tag: "node"},
		node:            &NodeInfo{Type: C.TypeShadowsocks, Common: &ServerConfig{ConfigRevision: "sha256:abcdef"}},
		inboundReady:    true,
		statusReady:     true,
		appliedRevision: "sha256:abcdef",
		appliedFeatures: append([]string(nil), shadowsocksAppliedFeatures...),
	}

	if err := controller.reportStatus(); err == nil {
		t.Fatal("expected the independent status report to fail")
	}
	for range 2 {
		if err := controller.push(); err != nil {
			t.Fatalf("status failure must not fail traffic/alive push: %v", err)
		}
	}
	if statusCalls != 1 || trafficCalls != 2 || aliveCalls != 2 {
		t.Fatalf("unexpected report calls: status=%d traffic=%d alive=%d", statusCalls, trafficCalls, aliveCalls)
	}
}

func TestRunBlockedStatusDoesNotBlockTrafficAndAlivePush(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	statusStarted := make(chan struct{}, 1)
	releaseStatus := make(chan struct{})
	trafficCalled := make(chan struct{}, 1)
	aliveCalled := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/server/UniProxy/config":
			_, _ = writer.Write([]byte(`{
				"protocol":"shadowsocks",
				"server_port":8388,
				"cipher":"aes-128-gcm",
				"config_revision":"sha256:abcdef",
				"routes":[]
			}`))
		case "/api/v1/server/UniProxy/user":
			_, _ = writer.Write([]byte(`{"users":[]}`))
		case "/api/v1/server/UniProxy/alivelist":
			_, _ = writer.Write([]byte(`{"alive":{}}`))
		case "/api/v1/server/UniProxy/status":
			select {
			case statusStarted <- struct{}{}:
			default:
			}
			select {
			case <-releaseStatus:
			case <-request.Context().Done():
			}
		case "/api/v1/server/UniProxy/push":
			select {
			case trafficCalled <- struct{}{}:
			default:
			}
		case "/api/v1/server/UniProxy/alive":
			select {
			case aliveCalled <- struct{}{}:
			default:
			}
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	controller := NewController(
		ctx,
		log.NewNOPFactory().Logger(),
		&fakeInboundManager{},
		nil,
		&TrafficTracker{nodes: make(map[string]*nodeTraffic)},
		newTestClientWithStyle(t, server.URL, APIStyleUniProxy, C.TypeShadowsocks),
		effectiveNodeOptions{
			Tag:          "node",
			NodeID:       23,
			PullInterval: time.Hour,
			PushInterval: 20 * time.Millisecond,
		},
	)
	runDone := make(chan struct{})
	go func() {
		controller.Run()
		close(runDone)
	}()

	select {
	case <-statusStarted:
	case <-time.After(time.Second):
		t.Fatal("status report did not start")
	}
	select {
	case <-trafficCalled:
	case <-time.After(time.Second):
		t.Fatal("blocked status report stopped traffic reporting")
	}
	select {
	case <-aliveCalled:
	case <-time.After(time.Second):
		t.Fatal("blocked status report stopped alive reporting")
	}

	close(releaseStatus)
	cancel()
	select {
	case <-runDone:
	case <-time.After(time.Second):
		t.Fatal("controller did not stop")
	}
}

func TestStatusWakeIsSingleFlightAndPublishesLatestSnapshot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	payloads := make(chan NodeStatus, 3)
	var calls atomic.Int32
	var inFlight atomic.Int32
	var maximumInFlight atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		current := inFlight.Add(1)
		for {
			maximum := maximumInFlight.Load()
			if current <= maximum || maximumInFlight.CompareAndSwap(maximum, current) {
				break
			}
		}
		defer inFlight.Add(-1)

		var payload NodeStatus
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		call := calls.Add(1)
		if call == 1 {
			close(firstStarted)
			select {
			case <-releaseFirst:
			case <-request.Context().Done():
				return
			}
		}
		payloads <- payload
	}))
	defer server.Close()

	controller := &Controller{
		ctx:             ctx,
		logger:          log.NewNOPFactory().Logger(),
		client:          newTestClientWithStyle(t, server.URL, APIStyleUniProxy, C.TypeShadowsocks),
		options:         effectiveNodeOptions{Tag: "node", PushInterval: time.Hour},
		node:            &NodeInfo{Type: C.TypeShadowsocks, Common: &ServerConfig{ConfigRevision: "sha256:old"}},
		statusSupported: true,
		statusReady:     true,
		appliedRevision: "sha256:old",
		appliedFeatures: append([]string(nil), shadowsocksAppliedFeatures...),
		statusReset:     make(chan struct{}, 1),
		statusWake:      make(chan struct{}, 1),
	}
	loopDone := make(chan struct{})
	go func() {
		controller.runStatusLoop()
		close(loopDone)
	}()

	controller.requestStatusReport()
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first status report did not start")
	}

	controller.lifecycle.Lock()
	controller.statusReady = false
	controller.appliedRevision = ""
	controller.appliedFeatures = nil
	controller.lifecycle.Unlock()
	controller.requestStatusReport()
	controller.requestStatusReport()
	close(releaseFirst)

	first := <-payloads
	second := <-payloads
	if !first.Ready || first.AppliedRevision != "sha256:old" {
		t.Fatalf("unexpected first status payload: %#v", first)
	}
	if second.Ready || second.AppliedRevision != "" || len(second.AppliedFeatures) != 0 {
		t.Fatalf("queued status did not publish the latest snapshot: %#v", second)
	}
	if maximumInFlight.Load() != 1 {
		t.Fatalf("status reports were not single-flight: max=%d", maximumInFlight.Load())
	}

	select {
	case unexpected := <-payloads:
		t.Fatalf("duplicate wake was not coalesced: %#v", unexpected)
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
	select {
	case <-loopDone:
	case <-time.After(time.Second):
		t.Fatal("status loop did not stop")
	}
}

func TestFinalNotReadyFollowsCanceledRegularStatusLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	readyStarted := make(chan struct{})
	events := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload NodeStatus
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		if payload.Ready {
			close(readyStarted)
			<-request.Context().Done()
			events <- "ready-canceled"
			return
		}
		events <- "final-not-ready"
	}))
	defer server.Close()

	controller := &Controller{
		ctx:             ctx,
		logger:          log.NewNOPFactory().Logger(),
		inbound:         &fakeInboundManager{},
		tracker:         &TrafficTracker{nodes: make(map[string]*nodeTraffic)},
		client:          newTestClientWithStyle(t, server.URL, APIStyleUniProxy, C.TypeShadowsocks),
		options:         effectiveNodeOptions{Tag: "node", PushInterval: time.Hour},
		node:            &NodeInfo{Type: C.TypeShadowsocks, Common: &ServerConfig{ConfigRevision: "sha256:old"}},
		statusSupported: true,
		statusReady:     true,
		appliedRevision: "sha256:old",
		appliedFeatures: append([]string(nil), shadowsocksAppliedFeatures...),
		statusReset:     make(chan struct{}, 1),
		statusWake:      make(chan struct{}, 1),
	}
	loopDone := make(chan struct{})
	go func() {
		controller.runStatusLoop()
		close(loopDone)
	}()
	controller.requestStatusReport()
	select {
	case <-readyStarted:
	case <-time.After(time.Second):
		t.Fatal("regular ready report did not start")
	}

	cancel()
	select {
	case <-loopDone:
	case <-time.After(time.Second):
		t.Fatal("regular status loop did not stop after cancellation")
	}
	if err := controller.Close(); err != nil {
		t.Fatal(err)
	}

	if event := <-events; event != "ready-canceled" {
		t.Fatalf("unexpected first status event: %s", event)
	}
	if event := <-events; event != "final-not-ready" {
		t.Fatalf("unexpected final status event: %s", event)
	}
	select {
	case event := <-events:
		t.Fatalf("status report occurred after final not-ready: %s", event)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestSuccessfulImmediateStatusReportRequestsScheduleReset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/server/UniProxy/status" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	controller := &Controller{
		ctx:             context.Background(),
		logger:          log.NewNOPFactory().Logger(),
		client:          newTestClientWithStyle(t, server.URL, APIStyleUniProxy, C.TypeShadowsocks),
		options:         effectiveNodeOptions{Tag: "node"},
		node:            &NodeInfo{Type: C.TypeShadowsocks, Common: &ServerConfig{ConfigRevision: "sha256:abcdef"}},
		statusReady:     true,
		appliedRevision: "sha256:abcdef",
		appliedFeatures: append([]string(nil), shadowsocksAppliedFeatures...),
		statusReset:     make(chan struct{}, 1),
	}

	controller.reportStatusBestEffort()

	select {
	case <-controller.statusReset:
	default:
		t.Fatal("successful immediate status report did not request a timer reset")
	}
}

func TestCloseReportsShadowsocksNotReadyBeforeClosingClient(t *testing.T) {
	var payload NodeStatus
	var statusCalls int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		statusCalls++
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()

	controller := &Controller{
		ctx:             context.Background(),
		logger:          log.NewNOPFactory().Logger(),
		inbound:         &fakeInboundManager{},
		tracker:         &TrafficTracker{nodes: make(map[string]*nodeTraffic)},
		client:          newTestClientWithStyle(t, server.URL, APIStyleUniProxy, C.TypeShadowsocks),
		options:         effectiveNodeOptions{Tag: "node"},
		node:            &NodeInfo{Type: C.TypeShadowsocks, Common: &ServerConfig{ConfigRevision: "sha256:abcdef"}},
		inboundReady:    true,
		statusReady:     true,
		appliedRevision: "sha256:abcdef",
		appliedFeatures: append([]string(nil), shadowsocksAppliedFeatures...),
		current:         option.Inbound{Type: C.TypeShadowsocks, Options: "old-options"},
	}

	if err := controller.Close(); err != nil {
		t.Fatal(err)
	}
	if statusCalls != 1 || payload.Ready || payload.AppliedRevision != "" || len(payload.AppliedFeatures) != 0 {
		t.Fatalf("unexpected final status report: calls=%d payload=%#v", statusCalls, payload)
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
	removeErr     error
	removeDeletes bool
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

func (m *fakeInboundManager) Remove(tag string) error {
	m.removeCalls++
	if m.removeDeletes && m.existingTags != nil {
		delete(m.existingTags, tag)
	}
	return m.removeErr
}

func (m *fakeInboundManager) Create(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, inboundType string, options any) error {
	m.createTypes = append(m.createTypes, inboundType)
	m.createOptions = append(m.createOptions, options)
	if len(m.createErrors) == 0 {
		if m.existingTags != nil {
			m.existingTags[tag] = fakeInbound{tag: tag}
		}
		return nil
	}
	err := m.createErrors[0]
	m.createErrors = m.createErrors[1:]
	if err == nil && m.existingTags != nil {
		m.existingTags[tag] = fakeInbound{tag: tag}
	}
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
