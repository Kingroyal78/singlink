package v2board

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGetServerConfigUsesQueryAndETag(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		if request.URL.Path != "/api/v1/server/UniProxy/config" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		assertUniProxyQuery(t, request)
		if calls == 1 {
			writer.Header().Set("ETag", `"config-etag"`)
			_, _ = writer.Write([]byte(`{
				"protocol":"vless",
				"server_port":443,
				"tls":2,
				"base_config":{"push_interval":60,"pull_interval":"120"}
			}`))
			return
		}
		if request.Header.Get("If-None-Match") != `"config-etag"` {
			t.Fatalf("missing If-None-Match: %s", request.Header.Get("If-None-Match"))
		}
		writer.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	node, err := client.GetNodeInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if node.Type != "vless" || node.Security != SecurityREALITY {
		t.Fatalf("unexpected node info: %#v", node)
	}
	if node.PushInterval != time.Minute || node.PullInterval != 2*time.Minute {
		t.Fatalf("unexpected intervals: %s %s", node.PushInterval, node.PullInterval)
	}
	_, err = client.GetServerConfig(context.Background())
	if !errors.Is(err, ErrNotModified) {
		t.Fatalf("expected ErrNotModified, got %v", err)
	}
}

func TestDecodeUserListTracksEnabledPresence(t *testing.T) {
	userList, err := decodeUserList([]byte(`{"users":[
		{"id":1,"uuid":"00000000-0000-0000-0000-000000000001"},
		{"id":2,"uuid":"00000000-0000-0000-0000-000000000002","enabled":false},
		{"id":3,"uuid":"00000000-0000-0000-0000-000000000003","enabled":true}
	]}`), "application/json")
	if err != nil {
		t.Fatal(err)
	}
	if userList.Users[0].EnabledSet {
		t.Fatal("missing enabled field should not be marked as present")
	}
	if !userList.Users[1].EnabledSet || userList.Users[1].Enabled {
		t.Fatalf("explicit disabled user was not decoded correctly: %#v", userList.Users[1])
	}
	if !userList.Users[2].EnabledSet || !userList.Users[2].Enabled {
		t.Fatalf("explicit enabled user was not decoded correctly: %#v", userList.Users[2])
	}
}

func TestGetV2ServerConfigUsesV2Path(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		if request.URL.Path != "/api/v2/server/config" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		query := request.URL.Query()
		if query.Get("node_type") != "" {
			t.Fatalf("unexpected node_type on v2 config request: %s", query.Get("node_type"))
		}
		if query.Get("node_id") != "23" {
			t.Fatalf("unexpected node_id: %s", query.Get("node_id"))
		}
		if query.Get("token") != "secret" {
			t.Fatalf("unexpected token: %s", query.Get("token"))
		}
		if calls > 1 {
			if request.Header.Get("If-None-Match") != `"v2-etag"` {
				t.Fatalf("unexpected v2 If-None-Match: %s", request.Header.Get("If-None-Match"))
			}
			writer.WriteHeader(http.StatusNotModified)
			return
		}
		writer.Header().Set("ETag", `"v2-etag"`)
		_, _ = writer.Write([]byte(`{
			"protocol":"vless",
			"listen_ip":"127.0.0.1",
			"server_port":443,
			"tls":0
		}`))
	}))
	defer server.Close()

	client, err := NewClient(Options{
		APIHost:    server.URL,
		APIVersion: 2,
		NodeConfig: NodeConfig{
			NodeID: 23,
			Token:  "secret",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	node, err := client.GetNodeInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if node.Type != "vless" {
		t.Fatalf("unexpected node type: %s", node.Type)
	}
	_, err = client.GetServerConfig(context.Background())
	if !errors.Is(err, ErrNotModified) {
		t.Fatalf("expected ErrNotModified, got %v", err)
	}
}

func TestResponseBodyIsClosed(t *testing.T) {
	body := &closeTrackingBody{Reader: bytes.NewReader([]byte(`{"protocol":"vless"}`))}
	client := newTestClientWithHTTPClient(t, "https://panel.example", &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       body,
				Request:    request,
			}, nil
		}),
	})

	if _, err := client.GetServerConfig(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !body.closed {
		t.Fatal("response body was not closed")
	}
}

func TestNewClientRejectsNegativeTimeout(t *testing.T) {
	_, err := NewClient(Options{
		APIHost: "https://panel.example",
		NodeConfig: NodeConfig{
			NodeID:   23,
			NodeType: "vless",
			Token:    "secret",
		},
		Timeout: -time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "timeout must not be negative") {
		t.Fatalf("expected negative timeout error, got %v", err)
	}
}

func TestGetUserListRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assertUniProxyQuery(t, request)
		_, _ = writer.Write([]byte(`{"users":[{"id":1,"uuid":"00000000-0000-0000-0000-000000000001"}]}`))
	}))
	defer server.Close()

	client, err := NewClient(Options{
		APIHost: server.URL,
		NodeConfig: NodeConfig{
			NodeID:   23,
			NodeType: "vless",
			Token:    "secret",
		},
		UserListBodyLimit: 16,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.GetUserList(context.Background())
	if err == nil || !strings.Contains(err.Error(), "response body too large") {
		t.Fatalf("expected oversized response error, got %v", err)
	}
}

func TestDecodeMsgpackRejectsHugeContainer(t *testing.T) {
	_, err := decodeMsgpack([]byte{0xdd, 0x00, 0x10, 0x00, 0x01})
	if err == nil || !strings.Contains(err.Error(), "msgpack array too large") {
		t.Fatalf("expected huge msgpack array error, got %v", err)
	}
}

func TestPushUserTrafficDrainsSuccessBody(t *testing.T) {
	body := &closeTrackingBody{Reader: bytes.NewReader([]byte(`ok`))}
	client := newTestClientWithHTTPClient(t, "https://panel.example", &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       body,
				Request:    request,
			}, nil
		}),
	})

	if err := client.PushUserTraffic(context.Background(), []UserTraffic{{UID: 7, Upload: 11, Download: 13}}); err != nil {
		t.Fatal(err)
	}
	if !body.closed {
		t.Fatal("response body was not closed")
	}
	if remaining := body.Reader.Len(); remaining != 0 {
		t.Fatalf("response body was not drained, %d bytes remain", remaining)
	}
}

func TestGetUserListMsgpackAndJSONFallback(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		if request.URL.Path != "/api/v1/server/UniProxy/user" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		assertUniProxyQuery(t, request)
		if request.Header.Get("X-Response-Format") != "msgpack" {
			t.Fatalf("missing msgpack request header")
		}
		if calls == 1 {
			writer.Header().Set("Content-Type", "application/x-msgpack")
			_, _ = writer.Write(msgpackMap(map[string]any{
				"users": []any{
					map[string]any{
						"id":           int64(1),
						"uuid":         "00000000-0000-0000-0000-000000000001",
						"speed_limit":  int64(10),
						"device_limit": int64(2),
						"label":        "user-1",
						"secret":       "raw-secret",
						"link_secret":  "ddraw-secret",
						"quota_bytes":  int64(1024),
					},
				},
			}))
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"users":[{"id":2,"uuid":"00000000-0000-0000-0000-000000000002","speed_limit":10,"device_limit":2}]}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	users, err := client.GetUserList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].ID != 1 || users[0].SpeedLimit != 10 {
		t.Fatalf("unexpected msgpack users: %#v", users)
	}
	if users[0].Secret != "raw-secret" || users[0].LinkSecret != "ddraw-secret" || users[0].QuotaBytes != 1024 {
		t.Fatalf("missing extended user fields: %#v", users[0])
	}
	users, err = client.GetUserList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].ID != 2 {
		t.Fatalf("unexpected json users: %#v", users)
	}
}

func TestPushAndAliveEndpoints(t *testing.T) {
	seen := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		seen[request.URL.Path] = true
		assertUniProxyQuery(t, request)
		if request.Method == http.MethodPost && request.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("missing content type")
		}
		switch request.URL.Path {
		case "/api/v1/server/UniProxy/push":
			var payload map[int][]int64
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if got := payload[7]; len(got) != 2 || got[0] != 11 || got[1] != 13 {
				t.Fatalf("unexpected push payload: %#v", payload)
			}
		case "/api/v1/server/UniProxy/alive":
			var payload map[int][]string
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if got := payload[7]; len(got) != 1 || got[0] != "192.0.2.1_23" {
				t.Fatalf("unexpected alive payload: %#v", payload)
			}
		case "/api/v1/server/UniProxy/alivelist":
			_, _ = writer.Write([]byte(`{"alive":{"7":1}}`))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	if err := client.PushUserTraffic(context.Background(), []UserTraffic{{UID: 7, Upload: 11, Download: 13}}); err != nil {
		t.Fatal(err)
	}
	if err := client.ReportOnlineUsers(context.Background(), []OnlineUser{{UID: 7, IP: "192.0.2.1"}}); err != nil {
		t.Fatal(err)
	}
	alive, err := client.GetAliveList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if alive[7] != 1 {
		t.Fatalf("unexpected alive list: %#v", alive)
	}
	for _, path := range []string{"/api/v1/server/UniProxy/push", "/api/v1/server/UniProxy/alive", "/api/v1/server/UniProxy/alivelist"} {
		if !seen[path] {
			t.Fatalf("missing request to %s", path)
		}
	}
}

func TestEmptyReportsSendEmptyJSONObjects(t *testing.T) {
	seen := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		seen[request.URL.Path] = true
		assertUniProxyQuery(t, request)
		switch request.URL.Path {
		case "/api/v1/server/UniProxy/push":
			var payload map[int][]int64
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if len(payload) != 0 {
				t.Fatalf("unexpected empty push payload: %#v", payload)
			}
		case "/api/v1/server/UniProxy/alive":
			var payload map[int][]string
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if len(payload) != 0 {
				t.Fatalf("unexpected empty alive payload: %#v", payload)
			}
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	if err := client.ReportUserTraffic(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	alive := map[int][]string{}
	if err := client.ReportNodeOnlineUsers(context.Background(), &alive); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v1/server/UniProxy/push", "/api/v1/server/UniProxy/alive"} {
		if !seen[path] {
			t.Fatalf("missing request to %s", path)
		}
	}
}

func TestLegacyDeepbworkConfigUsersAndSubmit(t *testing.T) {
	seen := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		seen[request.URL.Path] = true
		switch request.URL.Path {
		case "/api/v1/server/Deepbwork/config":
			assertLegacyQuery(t, request, true)
			_, _ = writer.Write([]byte(`{
				"inbounds":[{
					"port":443,
					"streamSettings":{
						"network":"ws",
						"security":"tls",
						"wsSettings":{"path":"/ws","headers":{"Host":"edge.example.com"}},
						"tlsSettings":{"serverName":"v.example.com"}
					}
				}]
			}`))
		case "/api/v1/server/Deepbwork/user":
			assertLegacyQuery(t, request, false)
			_, _ = writer.Write([]byte(`{"msg":"ok","data":[{
				"id":7,
				"speed_limit":10,
				"device_limit":2,
				"v2ray_user":{"uuid":"00000000-0000-0000-0000-000000000007","email":"u@v2board.user","alter_id":0,"level":0}
			}]}`))
		case "/api/v1/server/Deepbwork/submit":
			assertLegacyQuery(t, request, false)
			var payload []LegacyUserTraffic
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if len(payload) != 1 || payload[0].UserID != 7 || payload[0].Upload != 11 || payload[0].Download != 13 {
				t.Fatalf("unexpected legacy submit payload: %#v", payload)
			}
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	client := newTestClientWithStyle(t, server.URL, APIStyleDeepbwork, "vmess")
	config, err := client.GetServerConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if config.Protocol != "vmess" || config.ServerPort != 443 || config.Network != "ws" || config.TLS != SecurityTLS {
		t.Fatalf("unexpected deepbwork config: %#v", config)
	}
	if config.ServerName != "v.example.com" || !bytes.Contains(config.NetworkSettings, []byte(`"/ws"`)) {
		t.Fatalf("unexpected deepbwork settings: %#v", config)
	}
	users, err := client.GetUserList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].UUID != "00000000-0000-0000-0000-000000000007" || users[0].SpeedLimit != 10 || users[0].DeviceLimit != 2 {
		t.Fatalf("unexpected deepbwork users: %#v", users)
	}
	if err := client.PushUserTraffic(context.Background(), []UserTraffic{{UID: 7, Upload: 11, Download: 13}}); err != nil {
		t.Fatal(err)
	}
	alive, err := client.GetAliveList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if alive != nil {
		t.Fatalf("legacy deepbwork should not report alivelist: %#v", alive)
	}
	alivePayload := map[int][]string{7: {"192.0.2.1_23"}}
	if err := client.ReportNodeOnlineUsers(context.Background(), &alivePayload); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v1/server/Deepbwork/config", "/api/v1/server/Deepbwork/user", "/api/v1/server/Deepbwork/submit"} {
		if !seen[path] {
			t.Fatalf("missing request to %s", path)
		}
	}
	if seen["/api/v1/server/Deepbwork/alive"] || seen["/api/v1/server/Deepbwork/alivelist"] {
		t.Fatalf("legacy alive endpoints should not be called: %#v", seen)
	}
}

func TestLegacyTrojanTidalabInferredFromNodeType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/server/TrojanTidalab/config":
			assertLegacyQuery(t, request, true)
			_, _ = writer.Write([]byte(`{
				"local_port":443,
				"ssl":{"sni":"trojan.example.com"}
			}`))
		case "/api/v1/server/TrojanTidalab/user":
			assertLegacyQuery(t, request, false)
			_, _ = writer.Write([]byte(`{"msg":"ok","data":[{
				"id":8,
				"trojan_user":{"password":"00000000-0000-0000-0000-000000000008"}
			}]}`))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	client := newTestClientWithStyle(t, server.URL, "", "trojan_tidalab")
	node, err := client.GetNodeInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if node.Type != "trojan" || node.Security != SecurityTLS || node.Common.ServerName != "trojan.example.com" {
		t.Fatalf("unexpected trojan tidalab node: %#v", node)
	}
	users, err := client.GetUserList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].UUID != "00000000-0000-0000-0000-000000000008" {
		t.Fatalf("unexpected trojan tidalab users: %#v", users)
	}
}

func TestLegacyShadowsocksTidalabDerivesConfigFromUsers(t *testing.T) {
	var submits int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/server/ShadowsocksTidalab/user":
			assertLegacyQuery(t, request, false)
			_, _ = writer.Write([]byte(`{"data":[{
				"id":9,
				"port":8388,
				"cipher":"chacha20-ietf-poly1305",
				"secret":"00000000-0000-0000-0000-000000000009"
			}]}`))
		case "/api/v1/server/ShadowsocksTidalab/submit":
			assertLegacyQuery(t, request, false)
			submits++
			var payload []LegacyUserTraffic
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if len(payload) != 1 || payload[0].UserID != 9 || payload[0].Upload != 1 || payload[0].Download != 2 {
				t.Fatalf("unexpected shadowsocks tidalab submit payload: %#v", payload)
			}
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	client := newTestClientWithStyle(t, server.URL, APIStyleShadowsocksTidalab, "shadowsocks")
	config, err := client.GetServerConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if config.Protocol != "shadowsocks" || config.ServerPort != 8388 || config.Cipher != "chacha20-ietf-poly1305" {
		t.Fatalf("unexpected shadowsocks tidalab config: %#v", config)
	}
	users, err := client.GetUserList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].UUID != "00000000-0000-0000-0000-000000000009" || users[0].Port != 8388 || users[0].Cipher != "chacha20-ietf-poly1305" {
		t.Fatalf("unexpected shadowsocks tidalab users: %#v", users)
	}
	if err := client.PushUserTraffic(context.Background(), []UserTraffic{{UID: 9, Upload: 1, Download: 2}}); err != nil {
		t.Fatal(err)
	}
	if submits != 1 {
		t.Fatalf("expected one submit, got %d", submits)
	}
}

func TestLegacyShadowsocksTidalabEmptyUsersReturnsSentinel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/server/ShadowsocksTidalab/user" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		assertLegacyQuery(t, request, false)
		_, _ = writer.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()

	client := newTestClientWithStyle(t, server.URL, APIStyleShadowsocksTidalab, "shadowsocks")
	_, err := client.GetNodeInfo(context.Background())
	if !errors.Is(err, ErrEmptyUserList) {
		t.Fatalf("expected ErrEmptyUserList, got %v", err)
	}
}

func TestErrorBodyIsLimited(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusBadGateway)
		_, _ = writer.Write([]byte("0123456789abcdef0123456789abcdef"))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	client.errorBodyLimit = 8
	_, err := client.GetAliveList(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "89abcdef") {
		t.Fatalf("error body was not limited: %v", err)
	}
}

func TestConfigBodySizeIsLimited(t *testing.T) {
	client := newTestClientWithHTTPClient(t, "https://panel.example", &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode:    http.StatusOK,
				Status:        "200 OK",
				ContentLength: DefaultConfigBodyLimit + 1,
				Header:        make(http.Header),
				Body:          io.NopCloser(strings.NewReader(`{}`)),
				Request:       request,
			}, nil
		}),
	})

	_, err := client.GetServerConfig(context.Background())
	if err == nil || !strings.Contains(err.Error(), "response body too large") {
		t.Fatalf("expected config body limit error, got %v", err)
	}
}

func TestAliveBodySizeIsLimited(t *testing.T) {
	client := newTestClientWithHTTPClient(t, "https://panel.example", &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode:    http.StatusOK,
				Status:        "200 OK",
				ContentLength: DefaultAliveBodyLimit + 1,
				Header:        make(http.Header),
				Body:          io.NopCloser(strings.NewReader(`{}`)),
				Request:       request,
			}, nil
		}),
	})

	_, err := client.GetAliveList(context.Background())
	if err == nil || !strings.Contains(err.Error(), "response body too large") {
		t.Fatalf("expected alive body limit error, got %v", err)
	}
}

func TestNewClientRejectsInvalidAPISendIP(t *testing.T) {
	_, err := NewClient(Options{
		APIHost:   "https://panel.example",
		APISendIP: "not-an-ip",
		NodeConfig: NodeConfig{
			NodeID:   23,
			NodeType: "vless",
			Token:    "secret",
		},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "api_send_ip") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewClientUsesProductionBodyLimitDefaults(t *testing.T) {
	client := newTestClient(t, "https://panel.example")
	if client.errorBodyLimit != DefaultErrorBodyLimit {
		t.Fatalf("unexpected error body limit: %d", client.errorBodyLimit)
	}
	if client.userBodyLimit != 16<<20 {
		t.Fatalf("unexpected user body limit: %d", client.userBodyLimit)
	}
}

func TestClientCloseClosesOwnedHTTPClientIdleConnections(t *testing.T) {
	transport := &closeIdleTransport{}
	client := &Client{
		httpClient:     &http.Client{Transport: transport},
		ownsHTTPClient: true,
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if !transport.closed {
		t.Fatal("owned HTTP client idle connections were not closed")
	}
}

func TestClientCloseDoesNotCloseExternalHTTPClient(t *testing.T) {
	transport := &closeIdleTransport{}
	client := &Client{
		httpClient:     &http.Client{Transport: transport},
		ownsHTTPClient: false,
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if transport.closed {
		t.Fatal("external HTTP client idle connections should not be closed")
	}
}

func TestServerRouteMatchAcceptsString(t *testing.T) {
	var config ServerConfig
	if err := json.Unmarshal([]byte(`{
		"protocol":"vless",
		"server_port":443,
		"routes":[{
			"id":1,
			"match":"regexp:example\\.com, protocol:bittorrent",
			"action":"block"
		}]
	}`), &config); err != nil {
		t.Fatal(err)
	}
	if got := []string(config.Routes[0].Match); len(got) != 2 || got[0] != "regexp:example\\.com" || got[1] != "protocol:bittorrent" {
		t.Fatalf("unexpected route match: %#v", got)
	}
}

func newTestClient(t *testing.T, apiHost string) *Client {
	return newTestClientWithHTTPClient(t, apiHost, nil)
}

func newTestClientWithHTTPClient(t *testing.T, apiHost string, httpClient *http.Client) *Client {
	t.Helper()
	client, err := NewClient(Options{
		APIHost: apiHost,
		NodeConfig: NodeConfig{
			NodeID:   23,
			NodeType: "VLESS",
			Token:    "secret",
		},
		HTTPClient: httpClient,
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func newTestClientWithStyle(t *testing.T, apiHost string, apiStyle string, nodeType string) *Client {
	t.Helper()
	client, err := NewClient(Options{
		APIHost:  apiHost,
		APIStyle: apiStyle,
		NodeConfig: NodeConfig{
			NodeID:   23,
			NodeType: nodeType,
			Token:    "secret",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func assertUniProxyQuery(t *testing.T, request *http.Request) {
	t.Helper()
	query := request.URL.Query()
	if query.Get("node_type") != "vless" {
		t.Fatalf("unexpected node_type: %s", query.Get("node_type"))
	}
	if query.Get("node_id") != "23" {
		t.Fatalf("unexpected node_id: %s", query.Get("node_id"))
	}
	if query.Get("token") != "secret" {
		t.Fatalf("unexpected token: %s", query.Get("token"))
	}
}

func assertLegacyQuery(t *testing.T, request *http.Request, expectLocalPort bool) {
	t.Helper()
	query := request.URL.Query()
	if query.Get("node_type") != "" {
		t.Fatalf("legacy request should not include node_type: %s", query.Get("node_type"))
	}
	if query.Get("node_id") != "23" {
		t.Fatalf("unexpected node_id: %s", query.Get("node_id"))
	}
	if query.Get("token") != "secret" {
		t.Fatalf("unexpected token: %s", query.Get("token"))
	}
	if expectLocalPort && query.Get("local_port") == "" {
		t.Fatalf("missing legacy local_port")
	}
	if !expectLocalPort && query.Get("local_port") != "" {
		t.Fatalf("unexpected legacy local_port: %s", query.Get("local_port"))
	}
}

type closeTrackingBody struct {
	*bytes.Reader
	closed bool
}

func (b *closeTrackingBody) Close() error {
	b.closed = true
	return nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type closeIdleTransport struct {
	closed bool
}

func (t *closeIdleTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{}`)),
		Request:    request,
	}, nil
}

func (t *closeIdleTransport) CloseIdleConnections() {
	t.closed = true
}

func msgpackMap(values map[string]any) []byte {
	var buffer bytes.Buffer
	writeMsgpackMap(&buffer, values)
	return buffer.Bytes()
}

func writeMsgpackValue(w io.Writer, value any) {
	switch typedValue := value.(type) {
	case map[string]any:
		writeMsgpackMap(w, typedValue)
	case []any:
		_, _ = w.Write([]byte{0x90 | byte(len(typedValue))})
		for _, item := range typedValue {
			writeMsgpackValue(w, item)
		}
	case string:
		if len(typedValue) <= 31 {
			_, _ = w.Write([]byte{0xa0 | byte(len(typedValue))})
		} else {
			_, _ = w.Write([]byte{0xd9, byte(len(typedValue))})
		}
		_, _ = w.Write([]byte(typedValue))
	case int64:
		_, _ = w.Write([]byte{0xd3, byte(typedValue >> 56), byte(typedValue >> 48), byte(typedValue >> 40), byte(typedValue >> 32), byte(typedValue >> 24), byte(typedValue >> 16), byte(typedValue >> 8), byte(typedValue)})
	case bool:
		if typedValue {
			_, _ = w.Write([]byte{0xc3})
		} else {
			_, _ = w.Write([]byte{0xc2})
		}
	default:
		panic("unsupported msgpack test value")
	}
}

func writeMsgpackMap(w io.Writer, values map[string]any) {
	_, _ = w.Write([]byte{0x80 | byte(len(values))})
	for key, value := range values {
		writeMsgpackValue(w, key)
		writeMsgpackValue(w, value)
	}
}
