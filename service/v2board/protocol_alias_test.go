package v2board

import "testing"

func TestMapInboundAcceptsSSProtocolAlias(t *testing.T) {
	inbound, err := MapServerConfigInbound(&ServerConfig{
		Protocol:   "ss",
		ListenIP:   "127.0.0.1",
		ServerPort: 8388,
		Network:    "tcp",
		Cipher:     benchmarkSSCipher,
	}, []UserInfo{{
		ID:   1,
		UUID: "00000000-0000-0000-0000-000000000001",
	}}, MapperOptions{Tag: "ss-alias"})
	if err != nil {
		t.Fatal(err)
	}
	if inbound.Type != "shadowsocks" {
		t.Fatalf("unexpected inbound type: %s", inbound.Type)
	}
}
