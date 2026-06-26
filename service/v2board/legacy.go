package v2board

import (
	"encoding/json"
	"strings"
)

type legacyV2RayConfig struct {
	Inbounds []legacyV2RayInbound `json:"inbounds"`
}

type legacyV2RayInbound struct {
	Port           int                       `json:"port"`
	StreamSettings legacyV2RayStreamSettings `json:"streamSettings"`
}

type legacyV2RayStreamSettings struct {
	Network      string            `json:"network"`
	Security     string            `json:"security"`
	TCPSettings  json.RawMessage   `json:"tcpSettings"`
	WSSettings   json.RawMessage   `json:"wsSettings"`
	HTTPSettings json.RawMessage   `json:"httpSettings"`
	QUICSettings json.RawMessage   `json:"quicSettings"`
	GRPCSettings json.RawMessage   `json:"grpcSettings"`
	TLSSettings  legacyTLSSettings `json:"tlsSettings"`
}

type legacyTLSSettings struct {
	ServerName string `json:"serverName"`
}

func (c legacyV2RayConfig) serverConfig() *ServerConfig {
	config := &ServerConfig{Protocol: "vmess"}
	if len(c.Inbounds) == 0 {
		return config
	}
	inbound := c.Inbounds[0]
	settings := inbound.StreamSettings
	network := strings.ToLower(strings.TrimSpace(settings.Network))
	if network == "" {
		network = "tcp"
	}
	config.ServerPort = inbound.Port
	config.Network = network
	config.NetworkSettings = settings.networkSettings(network)
	if strings.EqualFold(settings.Security, "tls") {
		config.TLS = SecurityTLS
		config.ServerName = settings.TLSSettings.ServerName
		config.TLSSettings.ServerName = settings.TLSSettings.ServerName
	}
	return config
}

func (s legacyV2RayStreamSettings) networkSettings(network string) json.RawMessage {
	switch network {
	case "tcp":
		return s.TCPSettings
	case "ws":
		return s.WSSettings
	case "http":
		return s.HTTPSettings
	case "quic":
		return s.QUICSettings
	case "grpc":
		return s.GRPCSettings
	default:
		return nil
	}
}

type legacyTrojanConfig struct {
	LocalPort int             `json:"local_port"`
	SSL       legacyTrojanSSL `json:"ssl"`
}

type legacyTrojanSSL struct {
	SNI string `json:"sni"`
}

func (c legacyTrojanConfig) serverConfig() *ServerConfig {
	return &ServerConfig{
		Protocol:   "trojan",
		ServerPort: c.LocalPort,
		ServerName: c.SSL.SNI,
		TLS:        SecurityTLS,
	}
}
