package v2board

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	SecurityNone    = 0
	SecurityTLS     = 1
	SecurityREALITY = 2
)

const (
	None    = SecurityNone
	Tls     = SecurityTLS
	Reality = SecurityREALITY
)

// NodeConfig is the panel node identity used on every UniProxy request.
type NodeConfig struct {
	NodeID   int    `json:"node_id,omitempty"`
	NodeType string `json:"node_type,omitempty"`
	Token    string `json:"token,omitempty"`
}

// Options configures a V2Board UniProxy client.
type Options struct {
	APIHost           string
	APISendIP         string
	APIVersion        int
	APIStyle          string
	NodeConfig        NodeConfig
	Timeout           time.Duration
	UserAgent         string
	ErrorBodyLimit    int64
	UserListBodyLimit int64
	HTTPClient        *http.Client
}

type ServerConfig struct {
	Protocol              string             `json:"protocol"`
	ListenIP              string             `json:"listen_ip"`
	ServerPort            int                `json:"server_port"`
	PortBindings          []MieruPortBinding `json:"port_bindings"`
	Routes                []ServerRoute      `json:"routes"`
	BaseConfig            *ServerBaseConfig  `json:"base_config"`
	TLS                   int                `json:"tls"`
	TLSSettings           ServerTLSSettings  `json:"tls_settings"`
	CertInfo              *CertInfo          `json:"-"`
	Network               string             `json:"network"`
	NetworkSettings       json.RawMessage    `json:"network_settings"`
	Encryption            string             `json:"encryption"`
	EncryptionSettings    ServerEncSettings  `json:"encryption_settings"`
	ServerName            string             `json:"server_name"`
	Flow                  string             `json:"flow"`
	RealityConfig         RealityConfig      `json:"reality_config"`
	Cipher                string             `json:"cipher"`
	ServerKey             string             `json:"server_key"`
	CongestionControl     string             `json:"congestion_control"`
	QUICCongestionControl string             `json:"quic_congestion_control"`
	ZeroRTTHandshake      bool               `json:"zero_rtt_handshake"`
	PaddingScheme         []string           `json:"padding_scheme,omitempty"`
	Version               int                `json:"version"`
	UpMbps                int                `json:"up_mbps"`
	DownMbps              int                `json:"down_mbps"`
	Obfs                  string             `json:"obfs"`
	ObfsPassword          string             `json:"obfs_password"`
	ObfsSettings          json.RawMessage    `json:"obfs_settings"`
	IgnoreClientBandwidth bool               `json:"ignore_client_bandwidth"`
	Transport             string             `json:"transport"`
	Multiplexing          string             `json:"multiplexing"`
	HandshakeMode         string             `json:"handshake_mode"`
	TrafficPattern        string             `json:"traffic_pattern"`
	MTU                   int                `json:"mtu"`
	Host                  string             `json:"host"`
	Port                  int                `json:"port"`
	SecretMode            string             `json:"secret_mode"`
	TLSDomain             string             `json:"tls_domain"`
	AllowAdTag            bool               `json:"allow_ad_tag"`
	Settings              map[string]any     `json:"settings"`
}

func (c *ServerConfig) UnmarshalJSON(data []byte) error {
	type serverConfig ServerConfig
	aux := struct {
		*serverConfig
		ServerPortRaw        json.RawMessage    `json:"server_port"`
		NetworkSettingsCamel json.RawMessage    `json:"networkSettings"`
		TLSSettingsCamel     *ServerTLSSettings `json:"tlsSettings"`
		ObfsPasswordHyphen   string             `json:"obfs-password"`
		SettingsRaw          json.RawMessage    `json:"settings"`
	}{
		serverConfig: (*serverConfig)(c),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if rawJSONHasValue(aux.ServerPortRaw) {
		serverPort, err := parseFlexiblePort(aux.ServerPortRaw, "server_port")
		if err != nil {
			return err
		}
		c.ServerPort = serverPort
	}
	if len(c.NetworkSettings) == 0 || bytes.Equal(c.NetworkSettings, []byte("null")) {
		if len(aux.NetworkSettingsCamel) > 0 {
			c.NetworkSettings = aux.NetworkSettingsCamel
		}
	}
	if aux.TLSSettingsCamel != nil {
		c.TLSSettings = *aux.TLSSettingsCamel
	}
	if c.ObfsPassword == "" && aux.ObfsPasswordHyphen != "" {
		c.ObfsPassword = aux.ObfsPasswordHyphen
	}
	if rawJSONHasValue(aux.SettingsRaw) {
		if len(bytes.TrimSpace(aux.SettingsRaw)) > 0 && bytes.TrimSpace(aux.SettingsRaw)[0] == '{' {
			if err := json.Unmarshal(aux.SettingsRaw, &c.Settings); err != nil {
				return fmt.Errorf("decode settings: %w", err)
			}
		}
	}
	return nil
}

type MieruPortBinding struct {
	Port      int    `json:"port,omitempty"`
	PortRange string `json:"port_range,omitempty"`
	Protocol  string `json:"protocol,omitempty"`
}

func (b *MieruPortBinding) UnmarshalJSON(data []byte) error {
	aux := struct {
		PortRaw      json.RawMessage `json:"port"`
		PortRange    string          `json:"port_range"`
		PortRangeAlt string          `json:"portRange"`
		Protocol     string          `json:"protocol"`
		Transport    string          `json:"transport"`
	}{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	var portRangeFromPort string
	if rawJSONHasValue(aux.PortRaw) {
		var portText string
		if err := json.Unmarshal(aux.PortRaw, &portText); err == nil {
			portText = strings.TrimSpace(portText)
			if strings.Contains(portText, "-") {
				portRangeFromPort = portText
			}
		}
		if portRangeFromPort != "" {
			b.Port = 0
		} else {
			port, err := parseFlexiblePort(aux.PortRaw, "port")
			if err != nil {
				return err
			}
			b.Port = port
		}
	}
	b.PortRange = firstNonEmpty(aux.PortRange, aux.PortRangeAlt, portRangeFromPort)
	b.Protocol = firstNonEmpty(aux.Protocol, aux.Transport)
	return nil
}

func parseFlexiblePort(data json.RawMessage, field string) (int, error) {
	var port int
	if err := json.Unmarshal(data, &port); err == nil {
		return port, nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return 0, fmt.Errorf("%s must be a port number: %w", field, err)
	}
	text = strings.TrimSpace(text)
	if text == "" || strings.Contains(text, "-") {
		return 0, nil
	}
	parsed, err := strconv.Atoi(text)
	if err != nil {
		return 0, fmt.Errorf("%s must be a port number: %w", field, err)
	}
	return parsed, nil
}

type ServerNodeInfo struct {
	ID                     int
	Type                   string
	Security               int
	PushInterval           time.Duration
	PullInterval           time.Duration
	NodeReportMinTraffic   int64
	DeviceOnlineMinTraffic int64
	Tag                    string
	Common                 *ServerConfig
}

type NodeInfo = ServerNodeInfo

type ServerRoute struct {
	ID          int        `json:"id"`
	Match       StringList `json:"match"`
	Action      string     `json:"action"`
	ActionValue *string    `json:"action_value"`
}

type Route = ServerRoute

type ServerBaseConfig struct {
	PushInterval           Interval `json:"push_interval"`
	PullInterval           Interval `json:"pull_interval"`
	DeviceOnlineMinTraffic int      `json:"device_online_min_traffic"`
	NodeReportMinTraffic   int      `json:"node_report_min_traffic"`
}

type BaseConfig = ServerBaseConfig

type StringList []string

func (l *StringList) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "null" {
		*l = nil
		return nil
	}
	if raw[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		if value == "" {
			*l = nil
			return nil
		}
		parts := strings.Split(value, ",")
		result := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				result = append(result, part)
			}
		}
		*l = result
		return nil
	}
	var values []string
	if err := json.Unmarshal(data, &values); err == nil {
		*l = values
		return nil
	}
	var anyValues []any
	if err := json.Unmarshal(data, &anyValues); err != nil {
		return err
	}
	result := make([]string, 0, len(anyValues))
	for _, value := range anyValues {
		result = append(result, fmt.Sprint(value))
	}
	*l = result
	return nil
}

type Interval time.Duration

func (i Interval) Duration() time.Duration {
	return time.Duration(i)
}

func (i Interval) Seconds() int64 {
	return int64(time.Duration(i) / time.Second)
}

func (i *Interval) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "null" {
		*i = 0
		return nil
	}
	if raw[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		duration, err := parseIntervalString(value)
		if err != nil {
			return err
		}
		*i = Interval(duration)
		return nil
	}
	seconds, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fmt.Errorf("parse interval: %w", err)
	}
	*i = Interval(time.Duration(seconds * float64(time.Second)))
	return nil
}

func (i Interval) MarshalJSON() ([]byte, error) {
	return []byte(strconv.FormatInt(i.Seconds(), 10)), nil
}

func parseIntervalString(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	if seconds, err := strconv.ParseFloat(value, 64); err == nil {
		return time.Duration(seconds * float64(time.Second)), nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse interval %q: %w", value, err)
	}
	return duration, nil
}

type ServerTLSSettings struct {
	ServerName       string   `json:"server_name"`
	ServerNames      []string `json:"server_names"`
	Dest             string   `json:"dest"`
	ServerPort       string   `json:"server_port"`
	ShortID          string   `json:"short_id"`
	ShortIDs         []string `json:"short_ids"`
	PublicKey        string   `json:"public_key"`
	PrivateKey       string   `json:"private_key"`
	MLDSA65Seed      string   `json:"mldsa65Seed"`
	Xver             Uint64   `json:"xver"`
	CertMode         string   `json:"cert_mode"`
	CertFile         string   `json:"cert_file"`
	KeyFile          string   `json:"key_file"`
	Provider         string   `json:"provider"`
	DNSEnv           string   `json:"dns_env"`
	RejectUnknownSNI string   `json:"reject_unknown_sni"`
	ECH              string   `json:"ech"`
	ECHServerName    string   `json:"ech_server_name"`
	ECHKey           string   `json:"ech_key"`
	ECHConfig        string   `json:"ech_config"`
}

type TLSSettings = ServerTLSSettings

type Uint64 uint64

func (u Uint64) Uint64() uint64 {
	return uint64(u)
}

func (u *Uint64) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "null" {
		*u = 0
		return nil
	}
	if raw[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		value = strings.TrimSpace(value)
		if value == "" {
			*u = 0
			return nil
		}
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return fmt.Errorf("parse uint64 %q: %w", value, err)
		}
		*u = Uint64(parsed)
		return nil
	}
	parsed, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return fmt.Errorf("parse uint64: %w", err)
	}
	*u = Uint64(parsed)
	return nil
}

func (u Uint64) MarshalJSON() ([]byte, error) {
	return []byte(strconv.FormatUint(uint64(u), 10)), nil
}

func (t ServerTLSSettings) EffectiveServerNames() []string {
	if len(t.ServerNames) > 0 {
		return t.ServerNames
	}
	if t.ServerName == "" {
		return nil
	}
	return []string{t.ServerName}
}

func (t ServerTLSSettings) EffectiveShortIDs() []string {
	if len(t.ShortIDs) > 0 {
		return t.ShortIDs
	}
	if t.ShortID == "" {
		return nil
	}
	return []string{t.ShortID}
}

func (t ServerTLSSettings) EffectiveShortIds() []string {
	return t.EffectiveShortIDs()
}

func (t ServerTLSSettings) PrimaryServerName() string {
	serverNames := t.EffectiveServerNames()
	if len(serverNames) == 0 {
		return ""
	}
	return serverNames[0]
}

type CertInfo struct {
	CertMode         string
	CertFile         string
	KeyFile          string
	Email            string
	CertDomain       string
	DNSEnv           map[string]string
	Provider         string
	RejectUnknownSNI bool
}

type ServerEncSettings struct {
	Mode          string `json:"mode"`
	RTT           string `json:"rtt"`
	Ticket        string `json:"ticket"`
	ServerPadding string `json:"server_padding"`
	ClientPadding string `json:"client_padding"`
	PrivateKey    string `json:"private_key"`
	Password      string `json:"password"`
}

type EncSettings = ServerEncSettings

func (s ServerEncSettings) HasValue() bool {
	return s.Mode != "" ||
		s.RTT != "" ||
		s.Ticket != "" ||
		s.ServerPadding != "" ||
		s.ClientPadding != "" ||
		s.PrivateKey != "" ||
		s.Password != ""
}

type PanelUserInfo struct {
	ID             int    `json:"id" codec:"id" msgpack:"id"`
	UUID           string `json:"uuid" codec:"uuid" msgpack:"uuid"`
	Username       string `json:"username,omitempty" codec:"username" msgpack:"username"`
	Password       string `json:"password,omitempty" codec:"password" msgpack:"password"`
	SpeedLimit     int    `json:"speed_limit" codec:"speed_limit" msgpack:"speed_limit"`
	DeviceLimit    int    `json:"device_limit" codec:"device_limit" msgpack:"device_limit"`
	Label          string `json:"label,omitempty" codec:"label" msgpack:"label"`
	Secret         string `json:"secret,omitempty" codec:"secret" msgpack:"secret"`
	LinkSecret     string `json:"link_secret,omitempty" codec:"link_secret" msgpack:"link_secret"`
	Port           int    `json:"port,omitempty" codec:"port" msgpack:"port"`
	Cipher         string `json:"cipher,omitempty" codec:"cipher" msgpack:"cipher"`
	Enabled        bool   `json:"enabled,omitempty" codec:"enabled" msgpack:"enabled"`
	EnabledSet     bool   `json:"-" codec:"-" msgpack:"-"`
	MaxConnections int    `json:"max_connections,omitempty" codec:"max_connections" msgpack:"max_connections"`
	MaxIPs         int    `json:"max_ips,omitempty" codec:"max_ips" msgpack:"max_ips"`
	QuotaBytes     int64  `json:"quota_bytes,omitempty" codec:"quota_bytes" msgpack:"quota_bytes"`
	ExpiresAt      int64  `json:"expires_at,omitempty" codec:"expires_at" msgpack:"expires_at"`
	ExpiresOn      string `json:"expires_on,omitempty" codec:"expires_on" msgpack:"expires_on"`
}

type UserInfo = PanelUserInfo

type UserListBody struct {
	Users []PanelUserInfo `json:"users" codec:"users" msgpack:"users"`
}

type AliveMap struct {
	Alive map[int]int `json:"alive"`
}

type UserTraffic struct {
	UID      int   `json:"uid"`
	Upload   int64 `json:"upload"`
	Download int64 `json:"download"`
}

type LegacyUserTraffic struct {
	UserID   int   `json:"user_id"`
	Upload   int64 `json:"u"`
	Download int64 `json:"d"`
}

type OnlineUser struct {
	UID int    `json:"uid"`
	IP  string `json:"ip"`
}
