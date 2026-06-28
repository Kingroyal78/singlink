//go:build v2board_live

package v2board

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/singlink/singlink/option"
)

type liveMatrixCase struct {
	Name              string `json:"name"`
	NodeID            int    `json:"node_id"`
	NodeType          string `json:"node_type"`
	APIVersion        int    `json:"api_version,omitempty"`
	APIStyle          string `json:"api_style,omitempty"`
	ExpectError       string `json:"expect_error,omitempty"`
	ExpectNotModified bool   `json:"expect_not_modified,omitempty"`
	NoMapperTLS       bool   `json:"no_mapper_tls,omitempty"`
}

func TestLiveV2BoardMatrix(t *testing.T) {
	baseURL := os.Getenv("V2BOARD_LIVE_BASE_URL")
	token := os.Getenv("V2BOARD_LIVE_TOKEN")
	rawCases := os.Getenv("V2BOARD_LIVE_CASES")
	if casesFile := os.Getenv("V2BOARD_LIVE_CASES_FILE"); casesFile != "" {
		content, err := os.ReadFile(casesFile)
		if err != nil {
			t.Fatalf("read V2BOARD_LIVE_CASES_FILE: %v", err)
		}
		rawCases = string(content)
	}
	if baseURL == "" || token == "" || strings.TrimSpace(rawCases) == "" {
		t.Skip("set V2BOARD_LIVE_BASE_URL, V2BOARD_LIVE_TOKEN, and V2BOARD_LIVE_CASES or V2BOARD_LIVE_CASES_FILE")
	}

	var cases []liveMatrixCase
	if err := json.Unmarshal([]byte(rawCases), &cases); err != nil {
		t.Fatalf("decode live cases: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("empty live case list")
	}

	mapperTLS, err := liveMapperTLS()
	if err != nil {
		t.Fatal(err)
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.Name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()

			client, err := NewClient(Options{
				APIHost:    baseURL,
				APIVersion: testCase.APIVersion,
				APIStyle:   testCase.APIStyle,
				NodeConfig: NodeConfig{
					NodeID:   testCase.NodeID,
					NodeType: testCase.NodeType,
					Token:    token,
				},
				Timeout: 10 * time.Second,
			})
			if err != nil {
				liveHandleError(t, testCase, "new client", err)
				return
			}
			defer client.Close()

			node, err := client.GetNodeInfo(ctx)
			if liveHandleError(t, testCase, "config", err) {
				return
			}
			t.Logf("node: id=%d type=%s security=%d port=%d", node.ID, node.Type, node.Security, node.Common.ServerPort)

			if testCase.ExpectNotModified {
				_, err = client.GetServerConfig(ctx)
				if !errors.Is(err, ErrNotModified) {
					t.Fatalf("expected second config request to return ErrNotModified, got %v", err)
				}
			}

			users, err := client.GetUserList(ctx)
			if liveHandleError(t, testCase, "user", err) {
				return
			}
			t.Logf("users: %d", len(users))

			alive, err := client.GetAliveList(ctx)
			if liveHandleError(t, testCase, "alivelist", err) {
				return
			}
			t.Logf("alive entries: %d", len(alive))

			tlsOptions := mapperTLS
			if testCase.NoMapperTLS {
				tlsOptions = nil
			}
			inbound, err := BuildInbound(testCase.Name, node, users, MapperOptions{
				Tag:        testCase.Name,
				InboundTLS: tlsOptions,
			})
			if liveHandleError(t, testCase, "mapper", err) {
				return
			}
			if testCase.ExpectError != "" {
				t.Fatalf("expected error containing %q, but mapped inbound type=%s tag=%s", testCase.ExpectError, inbound.Type, inbound.Tag)
			}
			t.Logf("mapped inbound: type=%s tag=%s", inbound.Type, inbound.Tag)

			if len(users) > 0 {
				err = client.ReportUserTraffic(ctx, []UserTraffic{{
					UID:      users[0].ID,
					Upload:   1,
					Download: 2,
				}})
				if liveHandleError(t, testCase, "push", err) {
					return
				}
				err = client.ReportOnlineUsers(ctx, []OnlineUser{{
					UID: users[0].ID,
					IP:  "127.0.0.1",
				}})
				if liveHandleError(t, testCase, "alive", err) {
					return
				}
			}
		})
	}
}

func liveMapperTLS() (*option.InboundTLSOptions, error) {
	certFile := os.Getenv("V2BOARD_LIVE_CERT_FILE")
	keyFile := os.Getenv("V2BOARD_LIVE_KEY_FILE")
	if certFile == "" && keyFile == "" {
		return nil, nil
	}
	if certFile == "" || keyFile == "" {
		return nil, fmt.Errorf("set both V2BOARD_LIVE_CERT_FILE and V2BOARD_LIVE_KEY_FILE")
	}
	return &option.InboundTLSOptions{
		Enabled:         true,
		ServerName:      firstNonEmpty(os.Getenv("V2BOARD_LIVE_SERVER_NAME"), "matrix.test"),
		CertificatePath: certFile,
		KeyPath:         keyFile,
	}, nil
}

func liveHandleError(t *testing.T, testCase liveMatrixCase, stage string, err error) bool {
	t.Helper()
	if err == nil {
		return false
	}
	if testCase.ExpectError != "" && strings.Contains(err.Error(), testCase.ExpectError) {
		t.Logf("expected %s error: %v", stage, err)
		return true
	}
	t.Fatalf("%s failed: %v", stage, err)
	return true
}
