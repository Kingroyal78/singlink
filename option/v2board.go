package option

import (
	"fmt"
	"strings"

	"github.com/sagernet/sing/common/json/badoption"
	C "github.com/singlink/singlink/constant"
)

type V2BoardServiceOptions struct {
	APIHost                string                   `json:"api_host,omitempty"`
	APIKey                 string                   `json:"api_key,omitempty"`
	APISendIP              string                   `json:"api_send_ip,omitempty"`
	APIVersion             int                      `json:"api_version,omitempty"`
	APIStyle               string                   `json:"api_style,omitempty"`
	Timeout                badoption.Duration       `json:"timeout,omitempty"`
	ErrorBodyLimit         int64                    `json:"error_body_limit,omitempty"`
	UserListBodyLimit      int64                    `json:"user_list_body_limit,omitempty"`
	Listen                 *badoption.Addr          `json:"listen,omitempty"`
	TCPFastOpen            *bool                    `json:"tcp_fast_open,omitempty"`
	PullInterval           badoption.Duration       `json:"pull_interval,omitempty"`
	PushInterval           badoption.Duration       `json:"push_interval,omitempty"`
	NodeReportMinTraffic   int64                    `json:"node_report_min_traffic,omitempty"`
	DeviceOnlineMinTraffic int64                    `json:"device_online_min_traffic,omitempty"`
	TLS                    *V2BoardTLSOptions       `json:"tls,omitempty"`
	Multiplex              *InboundMultiplexOptions `json:"multiplex,omitempty"`
	Nodes                  []V2BoardNodeOptions     `json:"nodes,omitempty"`
}

type V2BoardNodeOptions struct {
	Tag                    string                   `json:"tag,omitempty"`
	NodeID                 int                      `json:"node_id"`
	NodeType               string                   `json:"node_type"`
	APIHost                string                   `json:"api_host,omitempty"`
	APIKey                 string                   `json:"api_key,omitempty"`
	APISendIP              string                   `json:"api_send_ip,omitempty"`
	APIVersion             int                      `json:"api_version,omitempty"`
	APIStyle               string                   `json:"api_style,omitempty"`
	Timeout                badoption.Duration       `json:"timeout,omitempty"`
	ErrorBodyLimit         int64                    `json:"error_body_limit,omitempty"`
	UserListBodyLimit      int64                    `json:"user_list_body_limit,omitempty"`
	Listen                 *badoption.Addr          `json:"listen,omitempty"`
	TCPFastOpen            *bool                    `json:"tcp_fast_open,omitempty"`
	PullInterval           badoption.Duration       `json:"pull_interval,omitempty"`
	PushInterval           badoption.Duration       `json:"push_interval,omitempty"`
	NodeReportMinTraffic   int64                    `json:"node_report_min_traffic,omitempty"`
	DeviceOnlineMinTraffic int64                    `json:"device_online_min_traffic,omitempty"`
	TLS                    *V2BoardTLSOptions       `json:"tls,omitempty"`
	Multiplex              *InboundMultiplexOptions `json:"multiplex,omitempty"`
}

type V2BoardTLSOptions struct {
	Mode                string `json:"mode,omitempty"`
	CertFile            string `json:"cert_file,omitempty"`
	KeyFile             string `json:"key_file,omitempty"`
	ServerName          string `json:"server_name,omitempty"`
	CertificateProvider string `json:"certificate_provider,omitempty"`
}

func v2BoardNodeTag(serviceTag string, serviceOptions V2BoardServiceOptions, nodeOptions V2BoardNodeOptions) string {
	if nodeOptions.Tag != "" {
		return nodeOptions.Tag
	}
	apiStyle := v2BoardNormalizeAPIStyleForNodeType(v2BoardFirstNonEmpty(nodeOptions.APIStyle, serviceOptions.APIStyle), nodeOptions.NodeType)
	nodeType := v2BoardNormalizeNodeTypeForAPIStyle(nodeOptions.NodeType, apiStyle)
	if nodeType == "" {
		apiVersion := nodeOptions.APIVersion
		if apiVersion == 0 {
			apiVersion = serviceOptions.APIVersion
		}
		if apiVersion == 2 {
			nodeType = "v2node"
		}
	}
	return fmt.Sprintf("%s-%s-%d", serviceTag, nodeType, nodeOptions.NodeID)
}

func v2BoardNormalizeAPIStyleForNodeType(apiStyle string, nodeType string) string {
	apiStyle = v2BoardNormalizeAPIStyle(apiStyle)
	if apiStyle != "uniproxy" {
		return apiStyle
	}
	switch strings.ToLower(strings.TrimSpace(nodeType)) {
	case "deepbwork", "aurora":
		return "deepbwork"
	case "trojan_tidalab", "trojantidalab", "trojan-tidalab":
		return "trojan_tidalab"
	case "shadowsocks_tidalab", "shadowsockstidalab", "shadowsocks-tidalab", "ss_tidalab", "sstidalab":
		return "shadowsocks_tidalab"
	default:
		return apiStyle
	}
}

func v2BoardFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func v2BoardNormalizeAPIStyle(apiStyle string) string {
	switch strings.ToLower(strings.TrimSpace(apiStyle)) {
	case "", "uniproxy":
		return "uniproxy"
	case "deepbwork", "v2ray", "aurora":
		return "deepbwork"
	case "trojan_tidalab", "trojantidalab", "trojan-tidalab":
		return "trojan_tidalab"
	case "shadowsocks_tidalab", "shadowsockstidalab", "shadowsocks-tidalab", "ss_tidalab", "sstidalab":
		return "shadowsocks_tidalab"
	default:
		return strings.ToLower(strings.TrimSpace(apiStyle))
	}
}

func v2BoardNormalizeNodeTypeForAPIStyle(nodeType string, apiStyle string) string {
	switch v2BoardNormalizeAPIStyle(apiStyle) {
	case "deepbwork":
		return C.TypeVMess
	case "trojan_tidalab":
		return C.TypeTrojan
	case "shadowsocks_tidalab":
		return C.TypeShadowsocks
	default:
		return v2BoardNormalizeNodeType(nodeType)
	}
}

func v2BoardNormalizeNodeType(nodeType string) string {
	switch strings.ToLower(strings.TrimSpace(nodeType)) {
	case "v2ray":
		return C.TypeVMess
	case "ss":
		return C.TypeShadowsocks
	case "deepbwork":
		return C.TypeVMess
	case "trojan_tidalab":
		return C.TypeTrojan
	case "shadowsocks_tidalab":
		return C.TypeShadowsocks
	default:
		return strings.ToLower(strings.TrimSpace(nodeType))
	}
}
