package rule

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	F "github.com/sagernet/sing/common/format"
	"github.com/sagernet/sing/common/json"
	"github.com/sagernet/sing/common/logger"
	"github.com/sagernet/sing/common/x/list"
	"github.com/sagernet/sing/service"
	"github.com/sagernet/sing/service/pause"
	"github.com/singlink/singlink/adapter"
	"github.com/singlink/singlink/common/srs"
	C "github.com/singlink/singlink/constant"
	"github.com/singlink/singlink/experimental/deprecated"
	"github.com/singlink/singlink/option"

	"go4.org/netipx"
)

var _ adapter.RuleSet = (*RemoteRuleSet)(nil)

const (
	remoteRuleSetDownloadTimeout  = 30 * time.Second
	remoteRuleSetMaxDownloadBytes = 32 * 1024 * 1024
)

type RemoteRuleSet struct {
	ctx            context.Context
	cancel         context.CancelFunc
	logger         logger.ContextLogger
	outbound       adapter.OutboundManager
	options        option.RuleSet
	updateInterval time.Duration
	httpClient     *http.Client
	access         sync.RWMutex
	rules          []adapter.HeadlessRule
	metadata       adapter.RuleSetMetadata
	lastUpdated    time.Time
	lastEtag       string
	cacheFile      adapter.CacheFile
	pauseManager   pause.Manager
	callbacks      list.List[adapter.RuleSetUpdateCallback]
	refs           atomic.Int32
	fetchWG        sync.WaitGroup
	closed         bool
}

func NewRemoteRuleSet(ctx context.Context, logger logger.ContextLogger, options option.RuleSet) (*RemoteRuleSet, error) {
	ctx, cancel := context.WithCancel(ctx)
	var updateInterval time.Duration
	if options.RemoteOptions.UpdateInterval > 0 {
		updateInterval = time.Duration(options.RemoteOptions.UpdateInterval)
	} else {
		updateInterval = 24 * time.Hour
	}
	return &RemoteRuleSet{
		ctx:            ctx,
		cancel:         cancel,
		outbound:       service.FromContext[adapter.OutboundManager](ctx),
		logger:         logger,
		options:        options,
		updateInterval: updateInterval,
		pauseManager:   service.FromContext[pause.Manager](ctx),
	}, nil
}

func (s *RemoteRuleSet) Name() string {
	return s.options.Tag
}

func (s *RemoteRuleSet) String() string {
	rules := s.ruleList()
	return strings.Join(F.MapToString(rules), " ")
}

func (s *RemoteRuleSet) StartContext(ctx context.Context, startContext *adapter.HTTPStartContext) error {
	s.cacheFile = service.FromContext[adapter.CacheFile](s.ctx)
	transport, err := s.resolveTransport()
	if err != nil {
		return E.Cause(err, "create rule-set http client")
	}
	startContext.Register(transport)
	s.httpClient = &http.Client{Transport: transport}
	if s.cacheFile != nil {
		if savedSet := s.cacheFile.LoadRuleSet(s.options.Tag); savedSet != nil {
			err = s.loadBytes(savedSet.Content)
			if err != nil {
				s.logger.Warn(E.Cause(err, "restore cached rule-set, will refetch"))
			} else {
				s.access.Lock()
				s.lastUpdated = savedSet.LastUpdated
				s.lastEtag = savedSet.LastEtag
				s.access.Unlock()
			}
		}
	}
	if s.lastUpdatedTime().IsZero() {
		err = s.fetch(ctx, true)
		if err != nil {
			return E.Cause(err, "initial rule-set: ", s.options.Tag)
		}
	}
	return nil
}

func (s *RemoteRuleSet) Metadata() adapter.RuleSetMetadata {
	s.access.RLock()
	defer s.access.RUnlock()
	return s.metadata
}

func (s *RemoteRuleSet) ExtractIPSet() []*netipx.IPSet {
	rules := s.ruleList()
	return common.FlatMap(rules, extractIPSetFromRule)
}

func (s *RemoteRuleSet) IncRef() {
	s.refs.Add(1)
}

func (s *RemoteRuleSet) DecRef() {
	if s.refs.Add(-1) < 0 {
		panic("rule-set: negative refs")
	}
}

func (s *RemoteRuleSet) Cleanup() {
	if s.refs.Load() == 0 {
		s.access.Lock()
		s.rules = nil
		s.access.Unlock()
	}
}

func (s *RemoteRuleSet) RegisterCallback(callback adapter.RuleSetUpdateCallback) *list.Element[adapter.RuleSetUpdateCallback] {
	s.access.Lock()
	defer s.access.Unlock()
	return s.callbacks.PushBack(callback)
}

func (s *RemoteRuleSet) UnregisterCallback(element *list.Element[adapter.RuleSetUpdateCallback]) {
	s.access.Lock()
	defer s.access.Unlock()
	s.callbacks.Remove(element)
}

func (s *RemoteRuleSet) loadBytes(content []byte) error {
	var (
		ruleSet option.PlainRuleSetCompat
		err     error
	)
	switch s.options.Format {
	case C.RuleSetFormatSource:
		ruleSet, err = json.UnmarshalExtended[option.PlainRuleSetCompat](content)
		if err != nil {
			return err
		}
	case C.RuleSetFormatBinary:
		ruleSet, err = srs.Read(bytes.NewReader(content), false)
		if err != nil {
			return err
		}
	default:
		return E.New("unknown rule-set format: ", s.options.Format)
	}
	plainRuleSet, err := ruleSet.Upgrade()
	if err != nil {
		return err
	}
	rules := make([]adapter.HeadlessRule, len(plainRuleSet.Rules))
	for i, ruleOptions := range plainRuleSet.Rules {
		rules[i], err = NewHeadlessRule(s.ctx, ruleOptions)
		if err != nil {
			return E.Cause(err, "parse rule_set.rules.[", i, "]")
		}
	}
	metadata := buildRuleSetMetadata(plainRuleSet.Rules)
	err = validateRuleSetMetadataUpdate(s.ctx, s.options.Tag, metadata)
	if err != nil {
		return err
	}
	s.access.Lock()
	if s.closed {
		s.access.Unlock()
		return os.ErrClosed
	}
	s.metadata = metadata
	s.rules = rules
	callbacks := s.callbacks.Array()
	s.access.Unlock()
	for _, callback := range callbacks {
		callback(s)
	}
	return nil
}

func (s *RemoteRuleSet) updateOnce(ctx context.Context) {
	err := s.fetch(ctx, false)
	if err != nil {
		s.logger.Error("fetch rule-set ", s.options.Tag, ": ", err)
	} else if s.refs.Load() == 0 {
		s.access.Lock()
		s.rules = nil
		s.access.Unlock()
	}
}

func (s *RemoteRuleSet) fetch(ctx context.Context, isStart bool) error {
	if !s.beginFetch() {
		return os.ErrClosed
	}
	defer s.fetchWG.Done()
	ctx, cancel := context.WithTimeout(ctx, remoteRuleSetDownloadTimeout)
	defer cancel()
	if s.ctx != nil {
		stopRuleSetCancel := context.AfterFunc(s.ctx, cancel)
		defer stopRuleSetCancel()
	}
	s.logger.Debug("updating rule-set ", s.options.Tag, " from URL: ", s.options.RemoteOptions.URL)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.options.RemoteOptions.URL, nil)
	if err != nil {
		return err
	}
	lastEtag := s.lastEtagValue()
	if lastEtag != "" {
		request.Header.Set("If-None-Match", lastEtag)
	}
	if !isStart {
		defer s.httpClient.CloseIdleConnections()
	}
	response, err := s.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusNotModified:
		lastUpdated := time.Now()
		s.access.Lock()
		if s.closed {
			s.access.Unlock()
			return os.ErrClosed
		}
		s.lastUpdated = lastUpdated
		s.access.Unlock()
		if s.cacheFile != nil {
			savedRuleSet := s.cacheFile.LoadRuleSet(s.options.Tag)
			if savedRuleSet != nil {
				savedRuleSet.LastUpdated = lastUpdated
				err = s.cacheFile.SaveRuleSet(s.options.Tag, savedRuleSet)
				if err != nil {
					s.logger.Error("save rule-set updated time: ", err)
					return nil
				}
			}
		}
		s.logger.Info("update rule-set ", s.options.Tag, ": not modified")
		return nil
	default:
		return E.New("unexpected status: ", response.Status)
	}
	content, err := readLimitedRuleSetBody(response)
	if err != nil {
		return err
	}
	err = s.loadBytes(content)
	if err != nil {
		return err
	}
	eTagHeader := response.Header.Get("Etag")
	lastUpdated := time.Now()
	s.access.Lock()
	if s.closed {
		s.access.Unlock()
		return os.ErrClosed
	}
	if eTagHeader != "" {
		s.lastEtag = eTagHeader
	}
	lastEtag = s.lastEtag
	s.lastUpdated = lastUpdated
	s.access.Unlock()
	if s.cacheFile != nil {
		err = s.cacheFile.SaveRuleSet(s.options.Tag, &adapter.SavedBinary{
			LastUpdated: lastUpdated,
			Content:     content,
			LastEtag:    lastEtag,
		})
		if err != nil {
			s.logger.Error("save rule-set cache: ", err)
		}
	}
	s.logger.Info("updated rule-set ", s.options.Tag)
	return nil
}

func (s *RemoteRuleSet) resolveTransport() (adapter.HTTPTransport, error) {
	httpClientManager := service.FromContext[adapter.HTTPClientManager](s.ctx)
	if s.options.RemoteOptions.HTTPClient != nil && !s.options.RemoteOptions.HTTPClient.IsEmpty() {
		if s.options.RemoteOptions.DownloadDetour != "" { //nolint:staticcheck
			return nil, E.New("http_client is conflict with deprecated download_detour field")
		}
		return httpClientManager.ResolveTransport(s.ctx, s.logger, *s.options.RemoteOptions.HTTPClient)
	}
	if s.options.RemoteOptions.DownloadDetour != "" { //nolint:staticcheck
		deprecated.Report(s.ctx, deprecated.OptionLegacyRuleSetDownloadDetour)
		return httpClientManager.ResolveTransport(s.ctx, s.logger, option.HTTPClientOptions{
			DialerOptions: option.DialerOptions{
				Detour: s.options.RemoteOptions.DownloadDetour, //nolint:staticcheck
			},
			DisableEmptyDirectCheck: true,
		})
	}
	defaultTransport := httpClientManager.DefaultTransport()
	if defaultTransport == nil {
		return nil, E.New("default http client transport is not initialized")
	}
	return defaultTransport, nil
}

func (s *RemoteRuleSet) Close() error {
	s.access.Lock()
	if s.closed {
		s.access.Unlock()
		return nil
	}
	s.closed = true
	s.rules = nil
	s.access.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
	if s.httpClient != nil {
		s.httpClient.CloseIdleConnections()
	}
	s.fetchWG.Wait()
	return nil
}

func (s *RemoteRuleSet) Match(metadata *adapter.InboundContext) bool {
	return !s.matchStates(metadata).isEmpty()
}

func (s *RemoteRuleSet) matchStates(metadata *adapter.InboundContext) ruleMatchStateSet {
	return s.matchStatesWithBase(metadata, 0)
}

func (s *RemoteRuleSet) matchStatesWithBase(metadata *adapter.InboundContext, base ruleMatchState) ruleMatchStateSet {
	rules := s.ruleList()
	var stateSet ruleMatchStateSet
	for _, rule := range rules {
		nestedMetadata := *metadata
		nestedMetadata.ResetRuleMatchCache()
		stateSet = stateSet.merge(matchHeadlessRuleStatesWithBase(rule, &nestedMetadata, base))
	}
	return stateSet
}

func (s *RemoteRuleSet) ruleList() []adapter.HeadlessRule {
	s.access.RLock()
	defer s.access.RUnlock()
	return append([]adapter.HeadlessRule(nil), s.rules...)
}

func (s *RemoteRuleSet) lastUpdatedTime() time.Time {
	s.access.RLock()
	defer s.access.RUnlock()
	return s.lastUpdated
}

func (s *RemoteRuleSet) lastEtagValue() string {
	s.access.RLock()
	defer s.access.RUnlock()
	return s.lastEtag
}

func (s *RemoteRuleSet) beginFetch() bool {
	s.access.Lock()
	defer s.access.Unlock()
	if s.closed {
		return false
	}
	s.fetchWG.Add(1)
	return true
}

func readLimitedRuleSetBody(response *http.Response) ([]byte, error) {
	if response.ContentLength > remoteRuleSetMaxDownloadBytes {
		return nil, E.New("rule-set download size exceeds limit: ", response.ContentLength, " > ", remoteRuleSetMaxDownloadBytes)
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, remoteRuleSetMaxDownloadBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > remoteRuleSetMaxDownloadBytes {
		return nil, E.New("rule-set download size exceeds limit: ", remoteRuleSetMaxDownloadBytes)
	}
	return content, nil
}
