package rule

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/sagernet/sing/common/json/badoption"
	"github.com/sagernet/sing/common/logger"
	"github.com/singlink/singlink/adapter"
	C "github.com/singlink/singlink/constant"
	"github.com/singlink/singlink/option"

	"github.com/stretchr/testify/require"
)

func TestLocalRuleSetConcurrentReloadMatchCleanup(t *testing.T) {
	t.Parallel()

	ruleSet := &LocalRuleSet{
		ctx: context.Background(),
		tag: "concurrent-local",
	}
	require.NoError(t, ruleSet.reloadRules([]option.HeadlessRule{testDomainHeadlessRule("one.example")}))

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				_ = ruleSet.Match(&adapter.InboundContext{Domain: "one.example"})
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 100 {
			_ = ruleSet.reloadRules([]option.HeadlessRule{testDomainHeadlessRule("one.example")})
			_ = ruleSet.reloadRules([]option.HeadlessRule{testDomainHeadlessRule("two.example")})
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 200 {
			_ = ruleSet.Metadata()
			ruleSet.Cleanup()
		}
	}()
	wg.Wait()
}

func TestRemoteRuleSetConcurrentLoadMatchCleanup(t *testing.T) {
	t.Parallel()

	ruleSet := &RemoteRuleSet{
		ctx: context.Background(),
		options: option.RuleSet{
			Tag:    "concurrent-remote",
			Format: C.RuleSetFormatSource,
		},
	}
	require.NoError(t, ruleSet.loadBytes([]byte(`{"version":4,"rules":[{"domain":["one.example"]}]}`)))

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				_ = ruleSet.Match(&adapter.InboundContext{Domain: "one.example"})
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 100 {
			_ = ruleSet.loadBytes([]byte(`{"version":4,"rules":[{"domain":["one.example"]}]}`))
			_ = ruleSet.loadBytes([]byte(`{"version":4,"rules":[{"domain":["two.example"]}]}`))
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 200 {
			_ = ruleSet.Metadata()
			ruleSet.Cleanup()
		}
	}()
	wg.Wait()
}

func TestRuleSetCallbacksRunOutsideLock(t *testing.T) {
	t.Parallel()

	localRuleSet := &LocalRuleSet{
		ctx: context.Background(),
		tag: "callback-local",
	}
	localDone := make(chan struct{})
	localRuleSet.RegisterCallback(func(adapter.RuleSet) {
		_ = localRuleSet.Metadata()
		localRuleSet.Cleanup()
		close(localDone)
	})
	require.NoError(t, localRuleSet.reloadRules([]option.HeadlessRule{testDomainHeadlessRule("callback.example")}))
	require.Eventually(t, func() bool {
		select {
		case <-localDone:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)

	remoteRuleSet := &RemoteRuleSet{
		ctx: context.Background(),
		options: option.RuleSet{
			Tag:    "callback-remote",
			Format: C.RuleSetFormatSource,
		},
	}
	remoteDone := make(chan struct{})
	remoteRuleSet.RegisterCallback(func(adapter.RuleSet) {
		_ = remoteRuleSet.Metadata()
		remoteRuleSet.Cleanup()
		close(remoteDone)
	})
	require.NoError(t, remoteRuleSet.loadBytes([]byte(`{"version":4,"rules":[{"domain":["callback.example"]}]}`)))
	require.Eventually(t, func() bool {
		select {
		case <-remoteDone:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
}

func TestRemoteRuleSetFetchRejectsOversizedBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.FormatInt(remoteRuleSetMaxDownloadBytes+1, 10))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ruleSet, err := NewRemoteRuleSet(context.Background(), logger.NOP(), option.RuleSet{
		Tag:    "oversized",
		Format: C.RuleSetFormatSource,
		RemoteOptions: option.RemoteRuleSet{
			URL: server.URL,
		},
	})
	require.NoError(t, err)
	ruleSet.httpClient = server.Client()
	defer ruleSet.Close()

	err = ruleSet.fetch(context.Background(), true)
	require.ErrorContains(t, err, "download size exceeds limit")
}

func testDomainHeadlessRule(domain string) option.HeadlessRule {
	return option.HeadlessRule{
		Type: C.RuleTypeDefault,
		DefaultOptions: option.DefaultHeadlessRule{
			Domain: badoption.Listable[string]{domain},
		},
	}
}
