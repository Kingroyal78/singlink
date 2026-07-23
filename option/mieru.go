package option

import "github.com/sagernet/sing/common/json/badoption"

type MieruOutboundOptions struct {
	DialerOptions
	ServerOptions
	ServerPortRanges badoption.Listable[string] `json:"server_ports,omitempty"`
	Transport        string                     `json:"transport,omitempty"`
	UserName         string                     `json:"username,omitempty"`
	Password         string                     `json:"password,omitempty"`
	Multiplexing     string                     `json:"multiplexing,omitempty"`
	TrafficPattern   string                     `json:"traffic_pattern,omitempty"`
}

type MieruInboundOptions struct {
	ListenOptions
	Users               []MieruUser        `json:"users,omitempty"`
	PortBindings        []MieruPortBinding `json:"port_bindings,omitempty"`
	Transport           string             `json:"transport,omitempty"`
	TrafficPattern      string             `json:"traffic_pattern,omitempty"`
	MTU                 int                `json:"mtu,omitempty"`
	UserHintIsMandatory bool               `json:"user_hint_is_mandatory,omitempty"`
}

type MieruPortBinding struct {
	Port      int    `json:"port,omitempty"`
	PortRange string `json:"port_range,omitempty"`
	Protocol  string `json:"protocol,omitempty"`
}

type MieruUser struct {
	Name     string `json:"name,omitempty"`
	Password string `json:"password,omitempty"`
}
