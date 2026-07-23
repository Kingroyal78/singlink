package mieru

import (
	"bytes"
	"encoding/hex"
	"net"
	"testing"
	"time"

	mieruconstant "github.com/enfein/mieru/v3/apis/constant"
	mierupb "github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	mierucipher "github.com/enfein/mieru/v3/pkg/cipher"
	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/singlink/singlink/log"
	"github.com/singlink/singlink/option"
)

func TestParseMieruPortRange(t *testing.T) {
	begin, end, err := parseMieruPortRange("9000-9010")
	if err != nil {
		t.Fatal(err)
	}
	if begin != 9000 || end != 9010 {
		t.Fatalf("unexpected range: %d-%d", begin, end)
	}

	for _, portRange := range []string{
		"",
		"0-1",
		"1-0",
		"65536-65537",
		"2-1",
		"1-2x",
		"1 - 2",
		"1",
		"-2",
		"1-",
	} {
		t.Run(portRange, func(t *testing.T) {
			_, _, err := parseMieruPortRange(portRange)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestMieruTransportProtocol(t *testing.T) {
	for _, transport := range []string{"TCP", "tcp", "UDP", "udp"} {
		t.Run(transport, func(t *testing.T) {
			if _, err := mieruTransportProtocol(transport); err != nil {
				t.Fatal(err)
			}
		})
	}
	if _, err := mieruTransportProtocol(""); err == nil {
		t.Fatal("expected missing transport error")
	}
	if _, err := mieruTransportProtocol("quic"); err == nil {
		t.Fatal("expected unknown transport error")
	}
}

func TestValidateMieruUser(t *testing.T) {
	if err := validateMieruUser("user", "password"); err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct {
		name     string
		password string
	}{
		{name: "", password: "password"},
		{name: "user", password: ""},
		{name: "01234567890123456789012345678901234567890123456789012345678901234", password: "password"},
		{name: "user", password: "01234567890123456789012345678901234567890123456789012345678901234"},
	} {
		if err := validateMieruUser(testCase.name, testCase.password); err == nil {
			t.Fatalf("expected error for name=%q password=%q", testCase.name, testCase.password)
		}
	}
}

func TestBuildMieruServerConfigForcesMandatoryHintAndHashesUsers(t *testing.T) {
	config, err := buildMieruServerConfig(log.NewNOPFactory().Logger(), option.MieruInboundOptions{
		ListenOptions: option.ListenOptions{
			ListenPort: 8964,
		},
		Transport: "TCP",
		Users: []option.MieruUser{
			{Name: "user", Password: "password"},
		},
		UserHintIsMandatory: false,
	})
	if err != nil {
		t.Fatal(err)
	}

	if !config.Config.GetAdvancedSettings().GetUserHintIsMandatory() {
		t.Fatal("user hint mandatory is disabled")
	}
	users := config.Config.GetUsers()
	if len(users) != 1 {
		t.Fatalf("len(users) = %d, want 1", len(users))
	}
	if users[0].GetPassword() != "" {
		t.Fatalf("plaintext password = %q, want empty", users[0].GetPassword())
	}
	wantHash := hex.EncodeToString(mierucipher.HashPassword([]byte("password"), []byte("user")))
	if users[0].GetHashedPassword() != wantHash {
		t.Fatalf("hashed password = %q, want %q", users[0].GetHashedPassword(), wantHash)
	}
}

func TestBuildMieruServerConfigUsesPortBindings(t *testing.T) {
	config, err := buildMieruServerConfig(log.NewNOPFactory().Logger(), option.MieruInboundOptions{
		PortBindings: []option.MieruPortBinding{
			{Port: 8964, Protocol: "udp"},
			{PortRange: "9000-9001", Protocol: "TCP"},
		},
		Users: []option.MieruUser{
			{Name: "user", Password: "password"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	bindings := config.Config.GetPortBindings()
	if len(bindings) != 2 {
		t.Fatalf("len(port_bindings) = %d, want 2", len(bindings))
	}
	if bindings[0].GetPort() != 8964 || bindings[0].GetProtocol() != mierupb.TransportProtocol_UDP {
		t.Fatalf("unexpected first port binding: %#v", bindings[0])
	}
	if bindings[1].GetPortRange() != "9000-9001" || bindings[1].GetProtocol() != mierupb.TransportProtocol_TCP {
		t.Fatalf("unexpected second port binding: %#v", bindings[1])
	}
}

func TestValidateMieruInboundOptionsRejectsInvalidPortBindings(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		bindings []option.MieruPortBinding
	}{
		{
			name:     "missing port and range",
			bindings: []option.MieruPortBinding{{Protocol: "TCP"}},
		},
		{
			name:     "both port and range",
			bindings: []option.MieruPortBinding{{Port: 8964, PortRange: "9000-9001", Protocol: "TCP"}},
		},
		{
			name:     "missing protocol",
			bindings: []option.MieruPortBinding{{Port: 8964}},
		},
		{
			name:     "invalid range",
			bindings: []option.MieruPortBinding{{PortRange: "9001-9000", Protocol: "TCP"}},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			err := validateMieruInboundOptions(option.MieruInboundOptions{
				PortBindings: testCase.bindings,
				Users: []option.MieruUser{
					{Name: "user", Password: "password"},
				},
			})
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestMieruPacketConnReadPacket(t *testing.T) {
	packet := append([]byte{
		0, 0, 0,
		mieruconstant.Socks5IPv4Address,
		1, 2, 3, 4,
		0x01, 0xbb,
	}, []byte("payload")...)
	conn := &memoryPacketConn{read: packet}
	mieruConn := &mieruPacketConn{PacketConn: conn}

	buffer := buf.NewPacket()
	defer buffer.Release()
	destination, err := mieruConn.ReadPacket(buffer)
	if err != nil {
		t.Fatal(err)
	}
	if destination.String() != "1.2.3.4:443" {
		t.Fatalf("unexpected destination: %s", destination)
	}
	if !bytes.Equal(buffer.Bytes(), []byte("payload")) {
		t.Fatalf("unexpected payload: %q", buffer.Bytes())
	}
}

func TestMieruPacketConnRejectsUDPFragment(t *testing.T) {
	conn := &memoryPacketConn{read: []byte{0, 0, 1}}
	mieruConn := &mieruPacketConn{PacketConn: conn}

	buffer := buf.NewPacket()
	defer buffer.Release()
	if _, err := mieruConn.ReadPacket(buffer); err == nil {
		t.Fatal("expected fragment error")
	}
}

func TestMieruPacketConnWritePacket(t *testing.T) {
	conn := &memoryPacketConn{}
	mieruConn := &mieruPacketConn{PacketConn: conn}

	err := mieruConn.WritePacket(buf.As([]byte("payload")), M.ParseSocksaddr("1.2.3.4:443"))
	if err != nil {
		t.Fatal(err)
	}
	expected := append([]byte{
		0, 0, 0,
		mieruconstant.Socks5IPv4Address,
		1, 2, 3, 4,
		0x01, 0xbb,
	}, []byte("payload")...)
	if !bytes.Equal(conn.written, expected) {
		t.Fatalf("unexpected written packet: %v", conn.written)
	}
}

type memoryPacketConn struct {
	read    []byte
	written []byte
}

func (c *memoryPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	copy(p, c.read)
	return len(c.read), M.ParseSocksaddr("127.0.0.1:1"), nil
}

func (c *memoryPacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	c.written = append(c.written[:0], p...)
	return len(p), nil
}

func (c *memoryPacketConn) Close() error {
	return nil
}

func (c *memoryPacketConn) LocalAddr() net.Addr {
	return M.ParseSocksaddr("127.0.0.1:1")
}

func (c *memoryPacketConn) SetDeadline(t time.Time) error {
	return nil
}

func (c *memoryPacketConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (c *memoryPacketConn) SetWriteDeadline(t time.Time) error {
	return nil
}
