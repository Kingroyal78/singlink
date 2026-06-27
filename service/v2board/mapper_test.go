package v2board

import (
	"strings"
	"testing"

	"github.com/singlink/singlink/option"
)

func TestMapUniProxyInboundVLESSWebsocket(t *testing.T) {
	inbound, err := MapUniProxyInbound("vless", []byte(`{
		"protocol": "vless",
		"listen_ip": "127.0.0.1",
		"server_port": 443,
		"tls": 1,
		"network": "ws",
		"network_settings": {
			"path": "/ws?ed=2048",
			"headers": {"Host": "example.com"}
		},
		"flow": "xtls-rprx-vision"
	}`), []UserInfo{{
		ID:   1,
		UUID: "00000000-0000-0000-0000-000000000001",
	}}, MapperOptions{
		Tag: "panel-vless",
		TLS: &option.V2BoardTLSOptions{
			CertFile: "cert.pem",
			KeyFile:  "key.pem",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if inbound.Type != "vless" || inbound.Tag != "panel-vless" {
		t.Fatalf("unexpected inbound identity: %#v", inbound)
	}
	options := inbound.Options.(*option.VLESSInboundOptions)
	if len(options.Users) != 1 || options.Users[0].Flow != "xtls-rprx-vision" {
		t.Fatalf("unexpected users: %#v", options.Users)
	}
	if options.Transport == nil || options.Transport.Type != "ws" {
		t.Fatalf("unexpected transport: %#v", options.Transport)
	}
	if options.Transport.WebsocketOptions.Path != "/ws" || options.Transport.WebsocketOptions.MaxEarlyData != 2048 {
		t.Fatalf("unexpected websocket options: %#v", options.Transport.WebsocketOptions)
	}
	if options.TLS == nil || !options.TLS.Enabled || options.TLS.CertificatePath != "cert.pem" {
		t.Fatalf("unexpected tls options: %#v", options.TLS)
	}
}

func TestMapInboundRejectsUnsupportedTransport(t *testing.T) {
	_, err := MapUniProxyInbound("vmess", []byte(`{
		"protocol": "vmess",
		"server_port": 10000,
		"network": "kcp"
	}`), []UserInfo{{
		ID:   1,
		UUID: "00000000-0000-0000-0000-000000000001",
	}}, MapperOptions{})
	if err == nil || !strings.Contains(err.Error(), "unsupported network") {
		t.Fatalf("expected unsupported network error, got %v", err)
	}
}

func TestMapInboundRejectsUnsupportedXHTTPTransport(t *testing.T) {
	_, err := MapUniProxyInbound("vless", []byte(`{
		"protocol": "vless",
		"server_port": 10000,
		"network": "xhttp"
	}`), []UserInfo{{
		ID:   1,
		UUID: "00000000-0000-0000-0000-000000000001",
	}}, MapperOptions{})
	if err == nil || !strings.Contains(err.Error(), `unsupported network "xhttp"`) {
		t.Fatalf("expected unsupported xhttp error, got %v", err)
	}
}

func TestMapInboundRejectsAcceptProxyProtocol(t *testing.T) {
	_, err := MapUniProxyInbound("vless", []byte(`{
		"protocol": "vless",
		"server_port": 10000,
		"network": "ws",
		"network_settings": {
			"acceptProxyProtocol": true,
			"path": "/ws"
		}
	}`), []UserInfo{{
		ID:   1,
		UUID: "00000000-0000-0000-0000-000000000001",
	}}, MapperOptions{})
	if err == nil || !strings.Contains(err.Error(), "unsupported acceptProxyProtocol") {
		t.Fatalf("expected unsupported acceptProxyProtocol error, got %v", err)
	}
}

func TestMapInboundRejectsUnsupportedNodeTypeBeforeUserValidation(t *testing.T) {
	_, err := MapUniProxyInbound("mtproxy", []byte(`{
		"server_port": 10000,
		"secret_mode": "tls"
	}`), []UserInfo{{
		ID:    1,
		Label: "user-1",
	}}, MapperOptions{})
	if err == nil || !strings.Contains(err.Error(), `unsupported node type "mtproxy" by singlink`) {
		t.Fatalf("expected unsupported node type error, got %v", err)
	}
}

func TestMapInboundRejectsTLSRequiredProtocolWhenTLSDisabled(t *testing.T) {
	_, err := MapUniProxyInbound("tuic", []byte(`{
		"protocol": "tuic",
		"server_port": 10000,
		"server_name": "example.com"
	}`), []UserInfo{{
		ID:   1,
		UUID: "00000000-0000-0000-0000-000000000001",
	}}, MapperOptions{
		TLS: &option.V2BoardTLSOptions{Mode: "none"},
	})
	if err == nil || !strings.Contains(err.Error(), "tuic tls") || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected disabled TLS error, got %v", err)
	}
}

func TestMapInboundOptionalTLSHonorsPanelCertModeNone(t *testing.T) {
	inbound, err := MapUniProxyInbound("vless", []byte(`{
		"protocol": "vless",
		"server_port": 10000,
		"tls": 1,
		"tls_settings": {
			"cert_mode": "none",
			"cert_file": "stale-cert.pem",
			"key_file": "stale-key.pem"
		}
	}`), []UserInfo{{
		ID:   1,
		UUID: "00000000-0000-0000-0000-000000000001",
	}}, MapperOptions{})
	if err != nil {
		t.Fatal(err)
	}
	options := inbound.Options.(*option.VLESSInboundOptions)
	if options.TLS != nil {
		t.Fatalf("expected panel cert_mode=none to disable optional TLS, got %#v", options.TLS)
	}
}

func TestMapInboundRejectsPanelCertModeNoneForTLSRequiredProtocol(t *testing.T) {
	_, err := MapUniProxyInbound("trojan", []byte(`{
		"protocol": "trojan",
		"server_port": 10000,
		"server_name": "example.com",
		"tls_settings": {
			"cert_mode": "none"
		}
	}`), []UserInfo{{
		ID:   1,
		UUID: "00000000-0000-0000-0000-000000000001",
	}}, MapperOptions{})
	if err == nil || !strings.Contains(err.Error(), "cert_mode=none") {
		t.Fatalf("expected cert_mode=none error, got %v", err)
	}
}

func TestMapInboundRejectsPanelCertificateProviderAutomation(t *testing.T) {
	_, err := MapUniProxyInbound("trojan", []byte(`{
		"protocol": "trojan",
		"server_port": 10000,
		"server_name": "example.com",
		"tls_settings": {
			"cert_mode": "dns",
			"provider": "cloudflare",
			"dns_env": "CF_TOKEN=secret"
		}
	}`), []UserInfo{{
		ID:   1,
		UUID: "00000000-0000-0000-0000-000000000001",
	}}, MapperOptions{})
	if err == nil || !strings.Contains(err.Error(), `unsupported v2board cert_mode "dns"`) {
		t.Fatalf("expected unsupported cert mode error, got %v", err)
	}
}

func TestMapInboundRejectsUnsupportedVLESSEncryption(t *testing.T) {
	_, err := MapUniProxyInbound("vless", []byte(`{
		"protocol": "vless",
		"server_port": 10000,
		"encryption": "mlkem768x25519plus",
		"encryption_settings": {
			"mode": "native",
			"rtt": "0rtt",
			"ticket": "600s",
			"server_padding": "100-200",
			"client_padding": "100-200",
			"private_key": "server-secret",
			"password": "client-public"
		}
	}`), []UserInfo{{
		ID:   1,
		UUID: "00000000-0000-0000-0000-000000000001",
	}}, MapperOptions{})
	if err == nil || !strings.Contains(err.Error(), `unsupported encryption "mlkem768x25519plus"`) {
		t.Fatalf("expected unsupported vless encryption error, got %v", err)
	}
}

func TestMapInboundRejectsVLESSEncryptionSettingsWithoutEncryption(t *testing.T) {
	_, err := MapUniProxyInbound("vless", []byte(`{
		"protocol": "vless",
		"server_port": 10000,
		"encryption_settings": {
			"mode": "native"
		}
	}`), []UserInfo{{
		ID:   1,
		UUID: "00000000-0000-0000-0000-000000000001",
	}}, MapperOptions{})
	if err == nil || !strings.Contains(err.Error(), "unsupported encryption_settings") {
		t.Fatalf("expected unsupported vless encryption_settings error, got %v", err)
	}
}

func TestMapUniProxyInboundVLESSCustomECH(t *testing.T) {
	const echKey = "ech-key"
	inbound, err := MapUniProxyInbound("vless", []byte(`{
		"protocol": "vless",
		"server_port": 443,
		"tls": 1,
		"tls_settings": {
			"server_name": "example.com",
			"cert_file": "cert.pem",
			"key_file": "key.pem",
			"ech": "custom",
			"ech_server_name": "public.example.com",
			"ech_key": "`+echKey+`",
			"ech_config": "client-config"
		}
	}`), []UserInfo{{
		ID:   1,
		UUID: "00000000-0000-0000-0000-000000000001",
	}}, MapperOptions{})
	if err != nil {
		t.Fatal(err)
	}
	options := inbound.Options.(*option.VLESSInboundOptions)
	if options.TLS == nil || options.TLS.ECH == nil || !options.TLS.ECH.Enabled {
		t.Fatalf("expected ECH tls options, got %#v", options.TLS)
	}
	if got := []string(options.TLS.ECH.Key); len(got) != 1 || got[0] != echKey {
		t.Fatalf("unexpected ECH key: %#v", options.TLS.ECH.Key)
	}
}

func TestMapInboundRejectsRealityXver(t *testing.T) {
	_, err := MapUniProxyInbound("vless", []byte(`{
		"protocol": "vless",
		"server_port": 443,
		"tls": 2,
		"tls_settings": {
			"server_name": "www.example.com",
			"dest": "www.example.com",
			"server_port": "443",
			"private_key": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
			"short_id": "01234567",
			"xver": 1
		}
	}`), []UserInfo{{
		ID:   1,
		UUID: "00000000-0000-0000-0000-000000000001",
	}}, MapperOptions{})
	if err == nil || !strings.Contains(err.Error(), "unsupported xver 1") {
		t.Fatalf("expected unsupported reality xver error, got %v", err)
	}
}

func TestMapUniProxyInboundVLESSHTTPTransport(t *testing.T) {
	inbound, err := MapUniProxyInbound("vless", []byte(`{
		"protocol": "vless",
		"server_port": 443,
		"tls": 1,
		"network": "http",
		"network_settings": {
			"host": ["edge.example.com"],
			"path": "/h2",
			"method": "POST"
		}
	}`), []UserInfo{{
		ID:   1,
		UUID: "00000000-0000-0000-0000-000000000001",
	}}, MapperOptions{
		Tag: "panel-vless-http",
		TLS: &option.V2BoardTLSOptions{
			CertFile: "cert.pem",
			KeyFile:  "key.pem",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	options := inbound.Options.(*option.VLESSInboundOptions)
	if options.Transport == nil || options.Transport.Type != "http" {
		t.Fatalf("unexpected transport: %#v", options.Transport)
	}
	if got := []string(options.Transport.HTTPOptions.Host); len(got) != 1 || got[0] != "edge.example.com" {
		t.Fatalf("unexpected http host: %#v", options.Transport.HTTPOptions.Host)
	}
	if options.Transport.HTTPOptions.Path != "/h2" || options.Transport.HTTPOptions.Method != "POST" {
		t.Fatalf("unexpected http options: %#v", options.Transport.HTTPOptions)
	}
}

func TestMapUniProxyInboundHysteriaVersion2UsesHysteria2(t *testing.T) {
	inbound, err := MapUniProxyInbound("hysteria", []byte(`{
		"version": 2,
		"server_port": 443,
		"server_name": "hy.example.com",
		"up_mbps": 100,
		"down_mbps": 100,
		"ignore_client_bandwidth": true,
		"obfs": "salamander",
		"obfs-password": "secret"
	}`), []UserInfo{{
		ID:   1,
		UUID: "00000000-0000-0000-0000-000000000001",
	}}, MapperOptions{
		Tag: "panel-hy2",
		TLS: &option.V2BoardTLSOptions{
			CertFile: "cert.pem",
			KeyFile:  "key.pem",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if inbound.Type != "hysteria2" {
		t.Fatalf("expected hysteria2 inbound, got %s", inbound.Type)
	}
	options := inbound.Options.(*option.Hysteria2InboundOptions)
	if !options.IgnoreClientBandwidth || options.Obfs == nil || options.Obfs.Password != "secret" {
		t.Fatalf("unexpected hysteria2 options: %#v", options)
	}
}

func TestMapUniProxyInboundAnyTLSReality(t *testing.T) {
	inbound, err := MapUniProxyInbound("anytls", []byte(`{
		"protocol": "anytls",
		"server_port": 8443,
		"tls": 2,
		"tls_settings": {
			"server_name": "www.example.com",
			"dest": "www.example.com",
			"server_port": "443",
			"private_key": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
			"short_id": "01234567"
		},
		"padding_scheme": ["stop=8", "0=30-30"]
	}`), []UserInfo{{
		ID:   1,
		UUID: "00000000-0000-0000-0000-000000000001",
	}}, MapperOptions{Tag: "panel-anytls-reality"})
	if err != nil {
		t.Fatal(err)
	}
	if inbound.Type != "anytls" || inbound.Tag != "panel-anytls-reality" {
		t.Fatalf("unexpected inbound identity: %#v", inbound)
	}
	options := inbound.Options.(*option.AnyTLSInboundOptions)
	if options.TLS == nil || options.TLS.Reality == nil || !options.TLS.Reality.Enabled {
		t.Fatalf("expected reality tls options, got %#v", options.TLS)
	}
	if options.TLS.ServerName != "www.example.com" {
		t.Fatalf("unexpected reality server name: %q", options.TLS.ServerName)
	}
	if options.TLS.Reality.PrivateKey != "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" {
		t.Fatalf("unexpected reality private key: %#v", options.TLS.Reality)
	}
	if got := []string(options.TLS.Reality.ShortID); len(got) != 1 || got[0] != "01234567" {
		t.Fatalf("unexpected reality short ids: %#v", options.TLS.Reality.ShortID)
	}
	if options.TLS.Reality.Handshake.Server != "www.example.com" || options.TLS.Reality.Handshake.ServerPort != 443 {
		t.Fatalf("unexpected reality handshake: %#v", options.TLS.Reality.Handshake)
	}
	if got := []string(options.PaddingScheme); len(got) != 2 || got[0] != "stop=8" {
		t.Fatalf("unexpected padding scheme: %#v", options.PaddingScheme)
	}
}

func TestMapInboundRejectsShortShadowsocks2022UserKey(t *testing.T) {
	_, err := MapUniProxyInbound("shadowsocks", []byte(`{
		"protocol": "shadowsocks",
		"server_port": 10000,
		"cipher": "2022-blake3-aes-256-gcm",
		"server_key": "server-key"
	}`), []UserInfo{{
		ID:   1,
		UUID: "short",
	}}, MapperOptions{})
	if err == nil || !strings.Contains(err.Error(), "too short") {
		t.Fatalf("expected short key error, got %v", err)
	}
}

func TestMapInboundRejectsUnsupportedShadowsocksObfs(t *testing.T) {
	_, err := MapUniProxyInbound("shadowsocks", []byte(`{
		"protocol": "shadowsocks",
		"server_port": 10000,
		"cipher": "aes-128-gcm",
		"obfs": "http",
		"obfs_settings": {"host": "example.com", "path": "/"}
	}`), []UserInfo{{
		ID:   1,
		UUID: "00000000-0000-0000-0000-000000000001",
	}}, MapperOptions{})
	if err == nil || !strings.Contains(err.Error(), `unsupported obfs "http"`) {
		t.Fatalf("expected unsupported shadowsocks obfs error, got %v", err)
	}
}

func TestMapInboundRejectsUnsupportedShadowsocksNetwork(t *testing.T) {
	_, err := MapUniProxyInbound("shadowsocks", []byte(`{
		"protocol": "shadowsocks",
		"server_port": 10000,
		"cipher": "aes-128-gcm",
		"network": "http"
	}`), []UserInfo{{
		ID:   1,
		UUID: "00000000-0000-0000-0000-000000000001",
	}}, MapperOptions{})
	if err == nil || !strings.Contains(err.Error(), `unsupported network "http"`) {
		t.Fatalf("expected unsupported shadowsocks network error, got %v", err)
	}
}
