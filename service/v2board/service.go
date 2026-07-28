package v2board

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/json/badoption"
	"github.com/sagernet/sing/service"
	"github.com/singlink/singlink/adapter"
	boxService "github.com/singlink/singlink/adapter/service"
	C "github.com/singlink/singlink/constant"
	"github.com/singlink/singlink/log"
	"github.com/singlink/singlink/option"
)

const minimumInterval = 5 * time.Second

func RegisterService(registry *boxService.Registry) {
	boxService.Register[option.V2BoardServiceOptions](registry, C.TypeV2Board, NewService)
}

type Service struct {
	boxService.Adapter
	ctx         context.Context
	cancel      context.CancelFunc
	logger      log.ContextLogger
	inbound     adapter.InboundManager
	router      adapter.Router
	tracker     *TrafficTracker
	controllers []*Controller
	wg          sync.WaitGroup
	released    bool
}

func NewService(ctx context.Context, logger log.ContextLogger, tag string, options option.V2BoardServiceOptions) (adapter.Service, error) {
	if len(options.Nodes) == 0 {
		return nil, E.New("missing v2board nodes")
	}
	ctx, cancel := context.WithCancel(ctx)
	inboundManager := service.FromContext[adapter.InboundManager](ctx)
	if inboundManager == nil {
		cancel()
		return nil, E.New("missing inbound manager")
	}
	router := service.FromContext[adapter.Router](ctx)
	if router == nil {
		cancel()
		return nil, E.New("missing router")
	}
	tracker := retainTrafficTracker(router)
	s := &Service{
		Adapter: boxService.NewAdapter(C.TypeV2Board, tag),
		ctx:     ctx,
		cancel:  cancel,
		logger:  logger,
		inbound: inboundManager,
		router:  router,
		tracker: tracker,
	}
	cleanup := func() {
		for _, controller := range s.controllers {
			if controller.client != nil {
				_ = controller.client.Close()
			}
		}
		s.releaseTracker()
		cancel()
	}
	seenTags := make(map[string]int, len(options.Nodes))
	for index, nodeOptions := range options.Nodes {
		effective, err := resolveNodeOptions(tag, index, options, nodeOptions)
		if err != nil {
			cleanup()
			return nil, err
		}
		if previous, loaded := seenTags[effective.Tag]; loaded {
			cleanup()
			return nil, E.New("node[", index, "]: duplicate tag ", effective.Tag, " already used by node[", previous, "]")
		}
		if _, loaded := inboundManager.Get(effective.Tag); loaded {
			cleanup()
			return nil, E.New("node[", index, "]: tag conflicts with existing inbound: ", effective.Tag)
		}
		seenTags[effective.Tag] = index
		client, err := NewClient(Options{
			APIHost:           effective.APIHost,
			APISendIP:         effective.APISendIP,
			APIVersion:        effective.APIVersion,
			APIStyle:          effective.APIStyle,
			ErrorBodyLimit:    effective.ErrorBodyLimit,
			UserListBodyLimit: effective.UserListBodyLimit,
			NodeConfig: NodeConfig{
				NodeID:   effective.NodeID,
				NodeType: effective.NodeType,
				Token:    effective.APIKey,
			},
			Timeout: effective.Timeout,
		})
		if err != nil {
			cleanup()
			return nil, E.Cause(err, "node[", index, "]")
		}
		s.controllers = append(s.controllers, NewController(ctx, logger, inboundManager, router, tracker, client, effective))
	}
	return s, nil
}

func (s *Service) Start(stage adapter.StartStage) error {
	if stage != adapter.StartStateStart {
		return nil
	}
	for _, controller := range s.controllers {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			controller.Run()
		}()
	}
	return nil
}

func (s *Service) Close() error {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
	var err error
	for _, controller := range s.controllers {
		err = E.Append(err, controller.Close(), func(err error) error {
			return err
		})
	}
	s.releaseTracker()
	return err
}

func (s *Service) releaseTracker() {
	if s.released {
		return
	}
	s.released = true
	releaseTrafficTracker(s.router, s.tracker)
}

type effectiveNodeOptions struct {
	Tag                    string
	NodeID                 int
	NodeType               string
	APIHost                string
	APIKey                 string
	APISendIP              string
	APIVersion             int
	APIStyle               string
	Timeout                time.Duration
	ErrorBodyLimit         int64
	UserListBodyLimit      int64
	Listen                 *badoption.Addr
	TCPFastOpen            bool
	PullInterval           time.Duration
	PushInterval           time.Duration
	NodeReportMinTraffic   int64
	DeviceOnlineMinTraffic int64
	TLS                    *option.InboundTLSOptions
	Multiplex              *option.InboundMultiplexOptions
}

func resolveNodeOptions(serviceTag string, index int, serviceOptions option.V2BoardServiceOptions, nodeOptions option.V2BoardNodeOptions) (effectiveNodeOptions, error) {
	apiVersion := nodeOptions.APIVersion
	if apiVersion == 0 {
		apiVersion = serviceOptions.APIVersion
	}
	if apiVersion == 0 {
		apiVersion = 1
	}
	if apiVersion != 1 && apiVersion != 2 {
		return effectiveNodeOptions{}, E.New("node[", index, "]: unsupported api_version ", apiVersion)
	}
	apiStyle := normalizeAPIStyleForNodeType(firstNonEmpty(nodeOptions.APIStyle, serviceOptions.APIStyle), nodeOptions.NodeType)
	if !validAPIStyle(apiStyle) {
		return effectiveNodeOptions{}, E.New("node[", index, "]: unsupported api_style ", apiStyle)
	}
	nodeType := normalizeNodeTypeForAPIStyle(nodeOptions.NodeType, apiStyle)
	if nodeOptions.NodeID <= 0 {
		return effectiveNodeOptions{}, E.New("node[", index, "]: missing node_id")
	}
	if nodeType == "" {
		if apiVersion == 2 {
			nodeType = "v2node"
		} else {
			return effectiveNodeOptions{}, E.New("node[", index, "]: missing node_type")
		}
	}
	tag := nodeOptions.Tag
	if tag == "" {
		tag = fmt.Sprintf("%s-%s-%d", serviceTag, nodeType, nodeOptions.NodeID)
	}
	apiHost := firstNonEmpty(nodeOptions.APIHost, serviceOptions.APIHost)
	apiKey := firstNonEmpty(nodeOptions.APIKey, serviceOptions.APIKey)
	timeout := time.Duration(nodeOptions.Timeout)
	if timeout == 0 {
		timeout = time.Duration(serviceOptions.Timeout)
	}
	if timeout < 0 {
		return effectiveNodeOptions{}, E.New("node[", index, "]: timeout must not be negative")
	}
	errorBodyLimit := nodeOptions.ErrorBodyLimit
	if errorBodyLimit == 0 {
		errorBodyLimit = serviceOptions.ErrorBodyLimit
	}
	if errorBodyLimit < 0 {
		return effectiveNodeOptions{}, E.New("node[", index, "]: error_body_limit must not be negative")
	}
	userListBodyLimit := nodeOptions.UserListBodyLimit
	if userListBodyLimit == 0 {
		userListBodyLimit = serviceOptions.UserListBodyLimit
	}
	if userListBodyLimit < 0 {
		return effectiveNodeOptions{}, E.New("node[", index, "]: user_list_body_limit must not be negative")
	}
	tcpFastOpen := false
	if serviceOptions.TCPFastOpen != nil {
		tcpFastOpen = *serviceOptions.TCPFastOpen
	}
	if nodeOptions.TCPFastOpen != nil {
		tcpFastOpen = *nodeOptions.TCPFastOpen
	}
	listen := serviceOptions.Listen
	if nodeOptions.Listen != nil {
		listen = nodeOptions.Listen
	}
	pullInterval := time.Duration(nodeOptions.PullInterval)
	if pullInterval == 0 {
		pullInterval = time.Duration(serviceOptions.PullInterval)
	}
	if pullInterval == 0 {
		pullInterval = time.Minute
	}
	if pullInterval < 0 {
		return effectiveNodeOptions{}, E.New("node[", index, "]: pull_interval must not be negative")
	}
	pullInterval = normalizeInterval(pullInterval)
	pushInterval := time.Duration(nodeOptions.PushInterval)
	if pushInterval == 0 {
		pushInterval = time.Duration(serviceOptions.PushInterval)
	}
	if pushInterval == 0 {
		pushInterval = time.Minute
	}
	if pushInterval < 0 {
		return effectiveNodeOptions{}, E.New("node[", index, "]: push_interval must not be negative")
	}
	pushInterval = normalizeInterval(pushInterval)
	nodeReportMinTraffic := nodeOptions.NodeReportMinTraffic
	if nodeReportMinTraffic == 0 {
		nodeReportMinTraffic = serviceOptions.NodeReportMinTraffic
	}
	deviceOnlineMinTraffic := nodeOptions.DeviceOnlineMinTraffic
	if deviceOnlineMinTraffic == 0 {
		deviceOnlineMinTraffic = serviceOptions.DeviceOnlineMinTraffic
	}
	tlsOptions := serviceOptions.TLS
	if nodeOptions.TLS != nil {
		tlsOptions = nodeOptions.TLS
	}
	inboundTLSOptions, err := resolveTLSOptions(tlsOptions)
	if err != nil {
		return effectiveNodeOptions{}, E.Cause(err, "node[", index, "]: tls")
	}
	multiplex := serviceOptions.Multiplex
	if nodeOptions.Multiplex != nil {
		multiplex = nodeOptions.Multiplex
	}
	return effectiveNodeOptions{
		Tag:                    tag,
		NodeID:                 nodeOptions.NodeID,
		NodeType:               nodeType,
		APIHost:                apiHost,
		APIKey:                 apiKey,
		APISendIP:              firstNonEmpty(nodeOptions.APISendIP, serviceOptions.APISendIP),
		APIVersion:             apiVersion,
		APIStyle:               apiStyle,
		Timeout:                timeout,
		ErrorBodyLimit:         errorBodyLimit,
		UserListBodyLimit:      userListBodyLimit,
		Listen:                 listen,
		TCPFastOpen:            tcpFastOpen,
		PullInterval:           pullInterval,
		PushInterval:           pushInterval,
		NodeReportMinTraffic:   nodeReportMinTraffic,
		DeviceOnlineMinTraffic: deviceOnlineMinTraffic,
		TLS:                    inboundTLSOptions,
		Multiplex:              multiplex,
	}, nil
}

func normalizeInterval(interval time.Duration) time.Duration {
	if interval > 0 && interval < minimumInterval {
		return minimumInterval
	}
	return interval
}

func resolveTLSOptions(tlsOptions *option.V2BoardTLSOptions) (*option.InboundTLSOptions, error) {
	if tlsOptions == nil {
		return nil, nil
	}
	if strings.EqualFold(tlsOptions.Mode, "none") {
		return &option.InboundTLSOptions{}, nil
	}
	inboundTLSOptions := &option.InboundTLSOptions{
		Enabled:    true,
		ServerName: tlsOptions.ServerName,
	}
	if tlsOptions.CertificateProvider != "" {
		inboundTLSOptions.CertificateProvider = &option.CertificateProviderOptions{Tag: tlsOptions.CertificateProvider}
		return inboundTLSOptions, nil
	}
	if tlsOptions.CertFile == "" && tlsOptions.KeyFile == "" {
		return nil, nil
	}
	if tlsOptions.CertFile == "" || tlsOptions.KeyFile == "" {
		return nil, fmt.Errorf("cert_file and key_file must be set together")
	}
	inboundTLSOptions.CertificatePath = tlsOptions.CertFile
	inboundTLSOptions.KeyPath = tlsOptions.KeyFile
	return inboundTLSOptions, nil
}
