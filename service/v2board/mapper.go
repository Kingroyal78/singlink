package v2board

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sagernet/sing/common/json/badoption"
	C "github.com/singlink/singlink/constant"
	"github.com/singlink/singlink/option"
)

type MapperOptions struct {
	Tag           string
	Listen        *badoption.Addr
	ListenAddress string
	TCPFastOpen   bool
	TLS           *option.V2BoardTLSOptions
	InboundTLS    *option.InboundTLSOptions
	Multiplex     *option.InboundMultiplexOptions
}

func BuildInbound(tag string, node *NodeInfo, users []UserInfo, mapperOptions MapperOptions) (option.Inbound, error) {
	if mapperOptions.Tag == "" {
		mapperOptions.Tag = tag
	}
	return MapInbound(node, users, mapperOptions)
}

func MapUniProxyInbound(nodeType string, nodeConfig []byte, users []UserInfo, mapperOptions MapperOptions) (option.Inbound, error) {
	var config ServerConfig
	if err := json.Unmarshal(nodeConfig, &config); err != nil {
		return option.Inbound{}, fmt.Errorf("decode %s node: %w", nodeType, err)
	}
	if config.Protocol == "" {
		config.Protocol = serverConfigNodeType(&config, nodeType)
	}
	return MapServerConfigInbound(&config, users, mapperOptions)
}

func MapServerConfigInbound(config *ServerConfig, users []UserInfo, mapperOptions MapperOptions) (option.Inbound, error) {
	if config == nil {
		return option.Inbound{}, fmt.Errorf("missing server config")
	}
	nodeType := serverConfigNodeType(config, "")
	return MapInbound(&NodeInfo{
		Type:     nodeType,
		Security: config.TLS,
		Tag:      mapperOptions.Tag,
		Common:   config,
	}, users, mapperOptions)
}

func MapInbound(node *NodeInfo, users []UserInfo, mapperOptions MapperOptions) (option.Inbound, error) {
	if node == nil {
		return option.Inbound{}, fmt.Errorf("missing node info")
	}
	config := node.Common
	if config == nil {
		return option.Inbound{}, fmt.Errorf("missing node server config")
	}
	nodeType := normalizeNodeType(firstNonEmpty(node.Type, config.Protocol))
	if nodeType == "" {
		return option.Inbound{}, fmt.Errorf("missing node type")
	}
	if !supportedNodeType(nodeType) {
		return option.Inbound{}, fmt.Errorf("unsupported node type %q by singlink", nodeType)
	}
	if config.ServerPort <= 0 || config.ServerPort > 65535 {
		return option.Inbound{}, fmt.Errorf("invalid server_port %d", config.ServerPort)
	}
	if len(users) == 0 {
		return option.Inbound{}, fmt.Errorf("missing users for %s inbound", nodeType)
	}
	if err := validateUsers(nodeType, users); err != nil {
		return option.Inbound{}, err
	}

	listen, err := listenOptions(config, mapperOptions)
	if err != nil {
		return option.Inbound{}, err
	}
	multiplex := cloneMultiplex(mapperOptions.Multiplex)
	tag := firstNonEmpty(mapperOptions.Tag, node.Tag)
	inbound := option.Inbound{Type: nodeType, Tag: tag}

	switch nodeType {
	case C.TypeVMess:
		transport, err := v2rayTransport(config.Network, config.NetworkSettings)
		if err != nil {
			return option.Inbound{}, fmt.Errorf("vmess transport: %w", err)
		}
		tlsOptions, err := inboundTLS(config.TLS, config, mapperOptions)
		if err != nil {
			return option.Inbound{}, err
		}
		inbound.Options = &option.VMessInboundOptions{
			ListenOptions:              listen,
			Users:                      vmessUsers(users),
			InboundTLSOptionsContainer: option.InboundTLSOptionsContainer{TLS: tlsOptions},
			Multiplex:                  multiplex,
			Transport:                  transport,
		}
	case C.TypeVLESS:
		if err := validateVLESSEncryption(config); err != nil {
			return option.Inbound{}, err
		}
		transport, err := v2rayTransport(config.Network, config.NetworkSettings)
		if err != nil {
			return option.Inbound{}, fmt.Errorf("vless transport: %w", err)
		}
		tlsOptions, err := inboundTLS(config.TLS, config, mapperOptions)
		if err != nil {
			return option.Inbound{}, err
		}
		inbound.Options = &option.VLESSInboundOptions{
			ListenOptions:              listen,
			Users:                      vlessUsers(users, config.Flow),
			InboundTLSOptionsContainer: option.InboundTLSOptionsContainer{TLS: tlsOptions},
			Multiplex:                  multiplex,
			Transport:                  transport,
		}
	case C.TypeShadowsocks:
		options, err := shadowsocksOptions(listen, config, users, multiplex)
		if err != nil {
			return option.Inbound{}, err
		}
		inbound.Options = options
	case C.TypeTrojan:
		transport, err := v2rayTransport(config.Network, config.NetworkSettings)
		if err != nil {
			return option.Inbound{}, fmt.Errorf("trojan transport: %w", err)
		}
		tlsOptions, err := requiredTLS(config, mapperOptions)
		if err != nil {
			return option.Inbound{}, fmt.Errorf("trojan tls: %w", err)
		}
		inbound.Options = &option.TrojanInboundOptions{
			ListenOptions:              listen,
			Users:                      trojanUsers(users),
			InboundTLSOptionsContainer: option.InboundTLSOptionsContainer{TLS: tlsOptions},
			Multiplex:                  multiplex,
			Transport:                  transport,
		}
	case C.TypeTUIC:
		tlsOptions, err := requiredTLS(config, mapperOptions)
		if err != nil {
			return option.Inbound{}, fmt.Errorf("tuic tls: %w", err)
		}
		ensureALPN(tlsOptions, "h3")
		inbound.Options = &option.TUICInboundOptions{
			ListenOptions:              listen,
			Users:                      tuicUsers(users),
			CongestionControl:          config.CongestionControl,
			ZeroRTTHandshake:           config.ZeroRTTHandshake,
			InboundTLSOptionsContainer: option.InboundTLSOptionsContainer{TLS: tlsOptions},
		}
	case C.TypeAnyTLS:
		tlsOptions, err := anyTLSTLS(config, mapperOptions)
		if err != nil {
			return option.Inbound{}, fmt.Errorf("anytls tls: %w", err)
		}
		inbound.Options = &option.AnyTLSInboundOptions{
			ListenOptions:              listen,
			InboundTLSOptionsContainer: option.InboundTLSOptionsContainer{TLS: tlsOptions},
			Users:                      anyTLSUsers(users),
			PaddingScheme:              badoption.Listable[string](append([]string(nil), config.PaddingScheme...)),
		}
	case C.TypeHysteria:
		tlsOptions, err := requiredTLS(config, mapperOptions)
		if err != nil {
			return option.Inbound{}, fmt.Errorf("hysteria tls: %w", err)
		}
		inbound.Options = &option.HysteriaInboundOptions{
			ListenOptions:              listen,
			UpMbps:                     config.UpMbps,
			DownMbps:                   config.DownMbps,
			Obfs:                       config.Obfs,
			Users:                      hysteriaUsers(users),
			InboundTLSOptionsContainer: option.InboundTLSOptionsContainer{TLS: tlsOptions},
		}
	case C.TypeHysteria2:
		tlsOptions, err := requiredTLS(config, mapperOptions)
		if err != nil {
			return option.Inbound{}, fmt.Errorf("hysteria2 tls: %w", err)
		}
		obfs, err := hysteria2Obfs(config)
		if err != nil {
			return option.Inbound{}, err
		}
		inbound.Options = &option.Hysteria2InboundOptions{
			ListenOptions:              listen,
			UpMbps:                     config.UpMbps,
			DownMbps:                   config.DownMbps,
			Obfs:                       obfs,
			Users:                      hysteria2Users(users),
			IgnoreClientBandwidth:      config.IgnoreClientBandwidth,
			InboundTLSOptionsContainer: option.InboundTLSOptionsContainer{TLS: tlsOptions},
		}
	default:
		return option.Inbound{}, fmt.Errorf("unsupported node type %q", nodeType)
	}
	return inbound, nil
}

func supportedNodeType(nodeType string) bool {
	switch nodeType {
	case C.TypeVMess,
		C.TypeVLESS,
		C.TypeShadowsocks,
		C.TypeTrojan,
		C.TypeTUIC,
		C.TypeAnyTLS,
		C.TypeHysteria,
		C.TypeHysteria2:
		return true
	default:
		return false
	}
}

func listenOptions(config *ServerConfig, mapperOptions MapperOptions) (option.ListenOptions, error) {
	listen := option.ListenOptions{
		ListenPort:  uint16(config.ServerPort),
		TCPFastOpen: mapperOptions.TCPFastOpen,
	}
	if mapperOptions.Listen != nil {
		listen.Listen = mapperOptions.Listen
		return listen, nil
	}
	listenAddress := firstNonEmpty(mapperOptions.ListenAddress, config.ListenIP)
	if listenAddress != "" {
		addr, err := netip.ParseAddr(listenAddress)
		if err != nil {
			return listen, fmt.Errorf("invalid listen address %q: %w", listenAddress, err)
		}
		listenAddr := badoption.Addr(addr)
		listen.Listen = &listenAddr
	}
	return listen, nil
}

func inboundTLS(security int, config *ServerConfig, mapperOptions MapperOptions) (*option.InboundTLSOptions, error) {
	switch security {
	case SecurityNone:
		return nil, nil
	case SecurityTLS:
		if panelTLSDisabled(config) {
			return nil, nil
		}
		return requiredTLS(config, mapperOptions)
	case SecurityREALITY:
		return realityTLS(config)
	default:
		return nil, fmt.Errorf("unsupported security value %d", security)
	}
}

func requiredTLS(config *ServerConfig, mapperOptions MapperOptions) (*option.InboundTLSOptions, error) {
	if panelTLSDisabled(config) {
		return nil, fmt.Errorf("TLS is disabled by v2board cert_mode=none")
	}
	if mapperOptions.InboundTLS != nil {
		tlsOptions := cloneTLS(mapperOptions.InboundTLS)
		if !tlsOptions.Enabled {
			return nil, fmt.Errorf("TLS options are disabled")
		}
		if tlsOptions.ServerName == "" {
			tlsOptions.ServerName = serverName(config)
		}
		if err := applyPanelECH(tlsOptions, config); err != nil {
			return nil, err
		}
		return tlsOptions, nil
	}
	if mapperOptions.TLS != nil {
		if strings.EqualFold(mapperOptions.TLS.Mode, "none") {
			return nil, fmt.Errorf("TLS options are disabled")
		}
		tlsOptions := &option.InboundTLSOptions{
			Enabled:    true,
			ServerName: firstNonEmpty(mapperOptions.TLS.ServerName, serverName(config)),
		}
		if mapperOptions.TLS.CertificateProvider != "" {
			tlsOptions.CertificateProvider = &option.CertificateProviderOptions{Tag: mapperOptions.TLS.CertificateProvider}
			if err := applyPanelECH(tlsOptions, config); err != nil {
				return nil, err
			}
			return tlsOptions, nil
		}
		if mapperOptions.TLS.CertFile == "" || mapperOptions.TLS.KeyFile == "" {
			return nil, fmt.Errorf("TLS cert_file and key_file are required")
		}
		tlsOptions.CertificatePath = mapperOptions.TLS.CertFile
		tlsOptions.KeyPath = mapperOptions.TLS.KeyFile
		if err := applyPanelECH(tlsOptions, config); err != nil {
			return nil, err
		}
		return tlsOptions, nil
	}
	if err := validatePanelTLSCertificateSettings(config); err != nil {
		return nil, err
	}
	if config.TLSSettings.CertFile == "" || config.TLSSettings.KeyFile == "" {
		return nil, fmt.Errorf("missing TLS options")
	}
	tlsOptions := &option.InboundTLSOptions{
		Enabled:         true,
		ServerName:      serverName(config),
		CertificatePath: config.TLSSettings.CertFile,
		KeyPath:         config.TLSSettings.KeyFile,
	}
	if err := applyPanelECH(tlsOptions, config); err != nil {
		return nil, err
	}
	return tlsOptions, nil
}

func anyTLSTLS(config *ServerConfig, mapperOptions MapperOptions) (*option.InboundTLSOptions, error) {
	var (
		tlsOptions *option.InboundTLSOptions
		err        error
	)
	switch config.TLS {
	case SecurityREALITY:
		tlsOptions, err = realityTLS(config)
	case SecurityTLS:
		tlsOptions, err = requiredTLS(config, mapperOptions)
	case SecurityNone:
		tlsOptions, err = requiredTLS(config, mapperOptions)
	default:
		return nil, fmt.Errorf("unsupported security value %d", config.TLS)
	}
	if err != nil {
		return nil, err
	}
	if tlsOptions == nil || !tlsOptions.Enabled {
		return nil, fmt.Errorf("missing TLS options")
	}
	return tlsOptions, nil
}

func panelTLSDisabled(config *ServerConfig) bool {
	return strings.EqualFold(strings.TrimSpace(config.TLSSettings.CertMode), "none")
}

func validatePanelTLSCertificateSettings(config *ServerConfig) error {
	settings := config.TLSSettings
	certMode := strings.ToLower(strings.TrimSpace(settings.CertMode))
	switch certMode {
	case "", "self":
	case "http", "dns":
		return fmt.Errorf("tls: unsupported v2board cert_mode %q by singlink inbound", settings.CertMode)
	case "none":
		return fmt.Errorf("TLS is disabled by v2board cert_mode=none")
	default:
		return fmt.Errorf("tls: unsupported v2board cert_mode %q", settings.CertMode)
	}
	if strings.TrimSpace(settings.Provider) != "" || strings.TrimSpace(settings.DNSEnv) != "" {
		return fmt.Errorf("tls: unsupported v2board certificate provider settings by singlink inbound")
	}
	return nil
}

func applyPanelECH(tlsOptions *option.InboundTLSOptions, config *ServerConfig) error {
	settings := config.TLSSettings
	echMode := strings.ToLower(strings.TrimSpace(settings.ECH))
	if echMode == "" {
		if settings.ECHKey != "" {
			return fmt.Errorf("tls: ech_key set without ech=custom")
		}
		return nil
	}
	if tlsOptions.Reality != nil && tlsOptions.Reality.Enabled {
		return fmt.Errorf("reality tls: ECH is unsupported")
	}
	switch echMode {
	case "custom":
		if strings.TrimSpace(settings.ECHKey) == "" {
			return fmt.Errorf("tls: missing ech_key for custom ECH")
		}
		tlsOptions.ECH = &option.InboundECHOptions{
			Enabled: true,
			Key:     badoption.Listable[string]{settings.ECHKey},
		}
		return nil
	case "cloudflare":
		return fmt.Errorf("tls: unsupported v2board ECH mode %q by singlink inbound", settings.ECH)
	default:
		return fmt.Errorf("tls: unsupported v2board ECH mode %q", settings.ECH)
	}
}

func realityTLS(config *ServerConfig) (*option.InboundTLSOptions, error) {
	settings := config.TLSSettings
	if strings.TrimSpace(settings.ECH) != "" || strings.TrimSpace(settings.ECHKey) != "" {
		return nil, fmt.Errorf("reality tls: ECH is unsupported")
	}
	xver := settings.Xver.Uint64()
	if xver == 0 {
		xver = config.RealityConfig.Xver
	}
	if xver != 0 {
		return nil, fmt.Errorf("reality tls: unsupported xver %d by singlink inbound", xver)
	}
	serverName := serverName(config)
	if serverName == "" {
		return nil, fmt.Errorf("reality tls: missing server_name")
	}
	if settings.PrivateKey == "" {
		return nil, fmt.Errorf("reality tls: missing private_key")
	}
	shortIDs := settings.EffectiveShortIDs()
	if len(shortIDs) == 0 {
		return nil, fmt.Errorf("reality tls: missing short_id")
	}
	dest := firstNonEmpty(settings.Dest, serverName)
	handshakeServer := dest
	handshakePortText := settings.ServerPort
	if host, port, err := net.SplitHostPort(dest); err == nil {
		handshakeServer = host
		if handshakePortText == "" {
			handshakePortText = port
		}
	}
	handshakePort, err := parsePort(handshakePortText)
	if err != nil {
		return nil, fmt.Errorf("reality tls: invalid server_port: %w", err)
	}
	var maxTimeDiff time.Duration
	if config.RealityConfig.MaxTimeDiff != "" {
		maxTimeDiff, err = time.ParseDuration(config.RealityConfig.MaxTimeDiff)
		if err != nil {
			return nil, fmt.Errorf("reality tls: invalid MaxTimeDiff %q: %w", config.RealityConfig.MaxTimeDiff, err)
		}
	}
	return &option.InboundTLSOptions{
		Enabled:    true,
		ServerName: serverName,
		Reality: &option.InboundRealityOptions{
			Enabled:           true,
			PrivateKey:        settings.PrivateKey,
			ShortID:           badoption.Listable[string](append([]string(nil), shortIDs...)),
			MaxTimeDifference: badoption.Duration(maxTimeDiff),
			Handshake: option.InboundRealityHandshakeOptions{
				ServerOptions: option.ServerOptions{
					Server:     handshakeServer,
					ServerPort: handshakePort,
				},
			},
		},
	}, nil
}

func v2rayTransport(network string, raw json.RawMessage) (*option.V2RayTransportOptions, error) {
	network = strings.ToLower(strings.TrimSpace(network))
	if err := rejectProxyProtocol(raw); err != nil {
		return nil, err
	}
	switch network {
	case "", "tcp":
		if len(raw) == 0 {
			return nil, nil
		}
		var config HTTPNetworkConfig
		if err := json.Unmarshal(raw, &config); err != nil {
			return nil, fmt.Errorf("decode tcp network_settings: %w", err)
		}
		headerType := strings.ToLower(strings.TrimSpace(config.Header.Type))
		switch headerType {
		case "":
			return nil, nil
		case C.V2RayTransportTypeHTTP:
			if config.Header.Request != nil {
				var request HTTPRequest
				if err := json.Unmarshal(*config.Header.Request, &request); err != nil {
					return nil, fmt.Errorf("decode http request: %w", err)
				}
				return httpTransport(request.Headers.Host, firstString(request.Path), request.Method, nil), nil
			}
			return httpTransport(nil, "", "", nil), nil
		default:
			return nil, fmt.Errorf("unsupported tcp header type %q", config.Header.Type)
		}
	case C.V2RayTransportTypeHTTP:
		var config HTTPTransportNetworkConfig
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &config); err != nil {
				return nil, fmt.Errorf("decode http network_settings: %w", err)
			}
		}
		hosts := config.Host
		if len(hosts) == 0 {
			hosts = config.HostAlias
		}
		return httpTransport(hosts, firstString(config.Path), config.Method, httpHeader(config.Headers)), nil
	case C.V2RayTransportTypeWebsocket:
		var config WebsocketNetworkConfig
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &config); err != nil {
				return nil, fmt.Errorf("decode websocket network_settings: %w", err)
			}
		}
		path, earlyData, err := websocketPath(config.Path)
		if err != nil {
			return nil, err
		}
		return &option.V2RayTransportOptions{
			Type: C.V2RayTransportTypeWebsocket,
			WebsocketOptions: option.V2RayWebsocketOptions{
				Path:                path,
				Headers:             stringHeader(config.Headers),
				MaxEarlyData:        earlyData,
				EarlyDataHeaderName: earlyDataHeaderName(earlyData),
			},
		}, nil
	case C.V2RayTransportTypeGRPC:
		var config GRPCNetworkConfig
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &config); err != nil {
				return nil, fmt.Errorf("decode grpc network_settings: %w", err)
			}
		}
		return &option.V2RayTransportOptions{
			Type:        C.V2RayTransportTypeGRPC,
			GRPCOptions: option.V2RayGRPCOptions{ServiceName: config.ServiceName},
		}, nil
	case C.V2RayTransportTypeHTTPUpgrade:
		var config HTTPUpgradeNetworkConfig
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &config); err != nil {
				return nil, fmt.Errorf("decode httpupgrade network_settings: %w", err)
			}
		}
		return &option.V2RayTransportOptions{
			Type: C.V2RayTransportTypeHTTPUpgrade,
			HTTPUpgradeOptions: option.V2RayHTTPUpgradeOptions{
				Path: config.Path,
				Host: config.Host,
			},
		}, nil
	case C.V2RayTransportTypeQUIC:
		return &option.V2RayTransportOptions{Type: C.V2RayTransportTypeQUIC}, nil
	default:
		return nil, fmt.Errorf("unsupported network %q", network)
	}
}

func rejectProxyProtocol(raw json.RawMessage) error {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return nil
	}
	var config proxyProtocolNetworkConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		return err
	}
	if config.AcceptProxyProtocol {
		return fmt.Errorf("unsupported acceptProxyProtocol by singlink inbound")
	}
	return nil
}

func httpTransport(hosts []string, path string, method string, headers badoption.HTTPHeader) *option.V2RayTransportOptions {
	return &option.V2RayTransportOptions{
		Type: C.V2RayTransportTypeHTTP,
		HTTPOptions: option.V2RayHTTPOptions{
			Host:    badoption.Listable[string](append([]string(nil), hosts...)),
			Path:    path,
			Method:  method,
			Headers: headers,
		},
	}
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func httpHeader(values map[string]StringList) badoption.HTTPHeader {
	if len(values) == 0 {
		return nil
	}
	header := make(badoption.HTTPHeader, len(values))
	for key, value := range values {
		header[key] = badoption.Listable[string](append([]string(nil), value...))
	}
	return header
}

func shadowsocksOptions(listen option.ListenOptions, config *ServerConfig, users []UserInfo, multiplex *option.InboundMultiplexOptions) (*option.ShadowsocksInboundOptions, error) {
	if config.Cipher == "" {
		return nil, fmt.Errorf("shadowsocks: missing cipher")
	}
	network := strings.ToLower(strings.TrimSpace(config.Network))
	if network != "" && network != "tcp" {
		return nil, fmt.Errorf("shadowsocks: unsupported network %q by singlink inbound", config.Network)
	}
	obfs := strings.ToLower(strings.TrimSpace(config.Obfs))
	if obfs != "" && obfs != "none" {
		return nil, fmt.Errorf("shadowsocks: unsupported obfs %q by singlink inbound", config.Obfs)
	}
	if rawJSONHasValue(config.ObfsSettings) {
		return nil, fmt.Errorf("shadowsocks: unsupported obfs_settings by singlink inbound")
	}
	options := &option.ShadowsocksInboundOptions{
		ListenOptions: listen,
		Method:        config.Cipher,
		Users:         make([]option.ShadowsocksUser, len(users)),
		Multiplex:     multiplex,
	}
	keyLength, is2022, err := shadowsocks2022KeyLength(config.Cipher)
	if err != nil {
		return nil, err
	}
	if is2022 {
		if config.ServerKey == "" {
			return nil, fmt.Errorf("shadowsocks: missing server_key for %s", config.Cipher)
		}
		options.Password = config.ServerKey
	}
	for i, user := range users {
		password := user.UUID
		if is2022 {
			if len(password) < keyLength {
				return nil, fmt.Errorf("shadowsocks: user %d uuid is too short for %s", user.ID, config.Cipher)
			}
			password = base64.StdEncoding.EncodeToString([]byte(password[:keyLength]))
		}
		options.Users[i] = option.ShadowsocksUser{Name: user.UUID, Password: password}
	}
	return options, nil
}

func validateVLESSEncryption(config *ServerConfig) error {
	encryption := strings.ToLower(strings.TrimSpace(config.Encryption))
	if encryption == "" || encryption == "none" {
		if encryption == "" && config.EncryptionSettings.HasValue() {
			return fmt.Errorf("vless: unsupported encryption_settings by singlink inbound")
		}
		return nil
	}
	return fmt.Errorf("vless: unsupported encryption %q by singlink inbound", config.Encryption)
}

func rawJSONHasValue(raw json.RawMessage) bool {
	value := strings.TrimSpace(string(raw))
	return value != "" && value != "null" && value != "{}" && value != "[]"
}

func shadowsocks2022KeyLength(cipher string) (int, bool, error) {
	switch cipher {
	case "2022-blake3-aes-128-gcm":
		return 16, true, nil
	case "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305":
		return 32, true, nil
	case "aes-128-gcm", "aes-192-gcm", "aes-256-gcm", "chacha20-ietf-poly1305", "xchacha20-ietf-poly1305", "none":
		return 0, false, nil
	default:
		return 0, false, fmt.Errorf("shadowsocks: unsupported cipher %q", cipher)
	}
}

func hysteria2Obfs(config *ServerConfig) (*option.Hysteria2Obfs, error) {
	obfsType := strings.TrimSpace(config.Obfs)
	obfsPassword := config.ObfsPassword
	if obfsType == "" && obfsPassword == "" {
		return nil, nil
	}
	if obfsType == "" {
		return nil, fmt.Errorf("hysteria2: obfs-password set without obfs type")
	}
	switch obfsType {
	case C.Hysteria2ObfsTypeSalamander, C.Hysteria2ObfsTypeGecko:
		if obfsPassword == "" {
			return nil, fmt.Errorf("hysteria2: missing obfs-password for %s obfs", obfsType)
		}
		return &option.Hysteria2Obfs{Type: obfsType, Password: obfsPassword}, nil
	default:
		if obfsPassword != "" {
			return nil, fmt.Errorf("hysteria2: unsupported obfs type %q", obfsType)
		}
		return &option.Hysteria2Obfs{Type: C.Hysteria2ObfsTypeSalamander, Password: obfsType}, nil
	}
}

func validateUsers(nodeType string, users []UserInfo) error {
	for i, user := range users {
		if strings.TrimSpace(user.UUID) == "" {
			return fmt.Errorf("%s: user at index %d has empty uuid", nodeType, i)
		}
	}
	return nil
}

func vmessUsers(users []UserInfo) []option.VMessUser {
	result := make([]option.VMessUser, len(users))
	for i, user := range users {
		result[i] = option.VMessUser{Name: user.UUID, UUID: user.UUID}
	}
	return result
}

func vlessUsers(users []UserInfo, flow string) []option.VLESSUser {
	result := make([]option.VLESSUser, len(users))
	for i, user := range users {
		result[i] = option.VLESSUser{Name: user.UUID, UUID: user.UUID, Flow: flow}
	}
	return result
}

func trojanUsers(users []UserInfo) []option.TrojanUser {
	result := make([]option.TrojanUser, len(users))
	for i, user := range users {
		result[i] = option.TrojanUser{Name: user.UUID, Password: user.UUID}
	}
	return result
}

func tuicUsers(users []UserInfo) []option.TUICUser {
	result := make([]option.TUICUser, len(users))
	for i, user := range users {
		result[i] = option.TUICUser{Name: user.UUID, UUID: user.UUID, Password: user.UUID}
	}
	return result
}

func anyTLSUsers(users []UserInfo) []option.AnyTLSUser {
	result := make([]option.AnyTLSUser, len(users))
	for i, user := range users {
		result[i] = option.AnyTLSUser{Name: user.UUID, Password: user.UUID}
	}
	return result
}

func hysteriaUsers(users []UserInfo) []option.HysteriaUser {
	result := make([]option.HysteriaUser, len(users))
	for i, user := range users {
		result[i] = option.HysteriaUser{Name: user.UUID, AuthString: user.UUID}
	}
	return result
}

func hysteria2Users(users []UserInfo) []option.Hysteria2User {
	result := make([]option.Hysteria2User, len(users))
	for i, user := range users {
		result[i] = option.Hysteria2User{Name: user.UUID, Password: user.UUID}
	}
	return result
}

func websocketPath(path string) (string, uint32, error) {
	if path == "" {
		return "", 0, nil
	}
	parsed, err := url.Parse(path)
	if err != nil {
		return "", 0, fmt.Errorf("parse websocket path: %w", err)
	}
	earlyDataText := parsed.Query().Get("ed")
	if earlyDataText == "" {
		return parsed.Path, 0, nil
	}
	earlyData, err := strconv.ParseUint(earlyDataText, 10, 32)
	if err != nil {
		return "", 0, fmt.Errorf("invalid websocket early data %q: %w", earlyDataText, err)
	}
	return parsed.Path, uint32(earlyData), nil
}

func earlyDataHeaderName(earlyData uint32) string {
	if earlyData == 0 {
		return ""
	}
	return "Sec-WebSocket-Protocol"
}

func stringHeader(headers map[string]string) badoption.HTTPHeader {
	if len(headers) == 0 {
		return nil
	}
	result := make(badoption.HTTPHeader, len(headers))
	for key, value := range headers {
		result[key] = badoption.Listable[string]{value}
	}
	return result
}

func parsePort(portText string) (uint16, error) {
	if portText == "" {
		return 0, fmt.Errorf("missing port")
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil {
		return 0, err
	}
	if port == 0 {
		return 0, fmt.Errorf("port must be greater than zero")
	}
	return uint16(port), nil
}

func serverName(config *ServerConfig) string {
	return firstNonEmpty(config.TLSSettings.PrimaryServerName(), config.ServerName)
}

func cloneTLS(tlsOptions *option.InboundTLSOptions) *option.InboundTLSOptions {
	if tlsOptions == nil {
		return nil
	}
	cloned := *tlsOptions
	cloned.ALPN = append(badoption.Listable[string](nil), tlsOptions.ALPN...)
	cloned.Certificate = append(badoption.Listable[string](nil), tlsOptions.Certificate...)
	cloned.ClientCertificate = append(badoption.Listable[string](nil), tlsOptions.ClientCertificate...)
	cloned.ClientCertificatePath = append(badoption.Listable[string](nil), tlsOptions.ClientCertificatePath...)
	cloned.CipherSuites = append(badoption.Listable[string](nil), tlsOptions.CipherSuites...)
	cloned.CurvePreferences = append(badoption.Listable[option.CurvePreference](nil), tlsOptions.CurvePreferences...)
	cloned.Key = append(badoption.Listable[string](nil), tlsOptions.Key...)
	return &cloned
}

func cloneMultiplex(multiplex *option.InboundMultiplexOptions) *option.InboundMultiplexOptions {
	if multiplex == nil {
		return nil
	}
	cloned := *multiplex
	if multiplex.Brutal != nil {
		brutal := *multiplex.Brutal
		cloned.Brutal = &brutal
	}
	return &cloned
}

func ensureALPN(tlsOptions *option.InboundTLSOptions, protocol string) {
	if slices.Contains(tlsOptions.ALPN, protocol) {
		return
	}
	tlsOptions.ALPN = append(tlsOptions.ALPN, protocol)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
