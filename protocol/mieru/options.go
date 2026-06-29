package mieru

import (
	"strconv"
	"strings"

	E "github.com/sagernet/sing/common/exceptions"

	mierupb "github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
)

func mieruTransportProtocol(transport string) (*mierupb.TransportProtocol, error) {
	switch strings.ToUpper(transport) {
	case "TCP":
		return mierupb.TransportProtocol_TCP.Enum(), nil
	case "UDP":
		return mierupb.TransportProtocol_UDP.Enum(), nil
	default:
		if transport == "" {
			return nil, E.New("missing transport")
		}
		return nil, E.New("unknown transport: ", transport)
	}
}

func validateMieruUser(name string, password string) error {
	if name == "" {
		return E.New("missing username")
	}
	if password == "" {
		return E.New("missing password")
	}
	if len(name) > 64 {
		return E.New("username exceeds 64 bytes")
	}
	if len(password) > 64 {
		return E.New("password exceeds 64 bytes")
	}
	return nil
}

func parseMieruPortRange(portRange string) (int, int, error) {
	if portRange == "" {
		return 0, 0, E.New("empty port range")
	}
	if strings.TrimSpace(portRange) != portRange {
		return 0, 0, E.New("invalid port range: ", portRange)
	}
	beginString, endString, found := strings.Cut(portRange, "-")
	if !found || beginString == "" || endString == "" {
		return 0, 0, E.New("invalid port range: ", portRange)
	}
	begin, err := strconv.Atoi(beginString)
	if err != nil {
		return 0, 0, E.Cause(err, "invalid begin port")
	}
	end, err := strconv.Atoi(endString)
	if err != nil {
		return 0, 0, E.Cause(err, "invalid end port")
	}
	if begin < 1 || begin > 65535 {
		return 0, 0, E.New("begin port out of range: ", begin)
	}
	if end < 1 || end > 65535 {
		return 0, 0, E.New("end port out of range: ", end)
	}
	if begin > end {
		return 0, 0, E.New("begin port greater than end port")
	}
	return begin, end, nil
}
