package v2board

import (
	"encoding/json"
	"strings"

	C "github.com/singlink/singlink/constant"
)

type RealityConfig struct {
	Xver         uint64 `json:"Xver"`
	MinClientVer string `json:"MinClientVer"`
	MaxClientVer string `json:"MaxClientVer"`
	MaxTimeDiff  string `json:"MaxTimeDiff"`
}

type HTTPNetworkConfig struct {
	AcceptProxyProtocol bool `json:"acceptProxyProtocol"`
	Header              struct {
		Type     string           `json:"type"`
		Request  *json.RawMessage `json:"request"`
		Response *json.RawMessage `json:"response"`
	} `json:"header"`
}

type HTTPRequest struct {
	Version string   `json:"version"`
	Method  string   `json:"method"`
	Path    []string `json:"path"`
	Headers struct {
		Host []string `json:"Host"`
	} `json:"headers"`
}

type HTTPTransportNetworkConfig struct {
	AcceptProxyProtocol bool                  `json:"acceptProxyProtocol"`
	Host                StringList            `json:"host"`
	HostAlias           StringList            `json:"Host"`
	Path                StringList            `json:"path"`
	Method              string                `json:"method"`
	Headers             map[string]StringList `json:"headers"`
}

type WebsocketNetworkConfig struct {
	AcceptProxyProtocol bool              `json:"acceptProxyProtocol"`
	Path                string            `json:"path"`
	Headers             map[string]string `json:"headers"`
}

type GRPCNetworkConfig struct {
	ServiceName string `json:"serviceName"`
}

type HTTPUpgradeNetworkConfig struct {
	AcceptProxyProtocol bool   `json:"acceptProxyProtocol"`
	Path                string `json:"path"`
	Host                string `json:"host"`
}

type proxyProtocolNetworkConfig struct {
	AcceptProxyProtocol bool `json:"acceptProxyProtocol"`
}

func normalizeNodeType(nodeType string) string {
	switch strings.ToLower(strings.TrimSpace(nodeType)) {
	case "v2ray":
		return "vmess"
	case "ss":
		return C.TypeShadowsocks
	case APIStyleDeepbwork:
		return C.TypeVMess
	case APIStyleTrojanTidalab:
		return C.TypeTrojan
	case APIStyleShadowsocksTidalab:
		return C.TypeShadowsocks
	default:
		return strings.ToLower(strings.TrimSpace(nodeType))
	}
}

func normalizeNodeTypeForAPIStyle(nodeType string, apiStyle string) string {
	switch normalizeAPIStyle(apiStyle) {
	case APIStyleDeepbwork:
		return C.TypeVMess
	case APIStyleTrojanTidalab:
		return C.TypeTrojan
	case APIStyleShadowsocksTidalab:
		return C.TypeShadowsocks
	default:
		return normalizeNodeType(nodeType)
	}
}

func serverConfigNodeType(config *ServerConfig, fallback string) string {
	nodeType := normalizeNodeType(firstNonEmpty(config.Protocol, fallback))
	if nodeType == C.TypeHysteria && config.Version == 2 {
		return C.TypeHysteria2
	}
	return nodeType
}

func panelNodeType(nodeType string) string {
	switch normalizeNodeType(nodeType) {
	case "vmess":
		return "v2ray"
	default:
		return normalizeNodeType(nodeType)
	}
}
