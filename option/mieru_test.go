package option

import (
	"context"
	"testing"

	"github.com/sagernet/sing/common/json"
	"github.com/sagernet/sing/service"
	C "github.com/singlink/singlink/constant"
	"github.com/stretchr/testify/require"
)

type stubMieruInboundOptionsRegistry struct{}

func (stubMieruInboundOptionsRegistry) CreateOptions(inboundType string) (any, bool) {
	switch inboundType {
	case C.TypeMieru:
		return new(MieruInboundOptions), true
	default:
		return nil, false
	}
}

type stubMieruOutboundOptionsRegistry struct{}

func (stubMieruOutboundOptionsRegistry) CreateOptions(outboundType string) (any, bool) {
	switch outboundType {
	case C.TypeMieru:
		return new(MieruOutboundOptions), true
	default:
		return nil, false
	}
}

func TestMieruInboundOptionsUnmarshal(t *testing.T) {
	t.Parallel()

	ctx := service.ContextWith[InboundOptionsRegistry](context.Background(), stubMieruInboundOptionsRegistry{})
	var inbound Inbound
	err := json.UnmarshalContext(ctx, []byte(`{
		"type": "mieru",
		"tag": "mieru-in",
		"listen": "127.0.0.1",
		"listen_port": 8964,
		"transport": "TCP",
		"port_bindings": [
			{
				"port": 8964,
				"protocol": "TCP"
			},
			{
				"port_range": "9000-9001",
				"protocol": "UDP"
			}
		],
		"users": [
			{
				"name": "user",
				"password": "password"
			}
		],
		"user_hint_is_mandatory": true
	}`), &inbound)
	require.NoError(t, err)
	require.Equal(t, C.TypeMieru, inbound.Type)
	require.Equal(t, "mieru-in", inbound.Tag)
	options := inbound.Options.(*MieruInboundOptions)
	require.Equal(t, uint16(8964), options.ListenOptions.ListenPort)
	require.Equal(t, "TCP", options.Transport)
	require.Equal(t, []MieruPortBinding{
		{Port: 8964, Protocol: "TCP"},
		{PortRange: "9000-9001", Protocol: "UDP"},
	}, options.PortBindings)
	require.Len(t, options.Users, 1)
	require.Equal(t, "user", options.Users[0].Name)
	require.Equal(t, "password", options.Users[0].Password)
	require.True(t, options.UserHintIsMandatory)
}

func TestMieruOutboundOptionsUnmarshal(t *testing.T) {
	t.Parallel()

	ctx := service.ContextWith[OutboundOptionsRegistry](context.Background(), stubMieruOutboundOptionsRegistry{})
	var outbound Outbound
	err := json.UnmarshalContext(ctx, []byte(`{
		"type": "mieru",
		"tag": "mieru-out",
		"server": "127.0.0.1",
		"server_port": 8964,
		"server_ports": [
			"9000-9010"
		],
		"transport": "TCP",
		"username": "user",
		"password": "password",
		"multiplexing": "MULTIPLEXING_LOW"
	}`), &outbound)
	require.NoError(t, err)
	require.Equal(t, C.TypeMieru, outbound.Type)
	require.Equal(t, "mieru-out", outbound.Tag)
	options := outbound.Options.(*MieruOutboundOptions)
	require.Equal(t, "127.0.0.1", options.Server)
	require.Equal(t, uint16(8964), options.ServerPort)
	require.Equal(t, []string{"9000-9010"}, []string(options.ServerPortRanges))
	require.Equal(t, "TCP", options.Transport)
	require.Equal(t, "user", options.UserName)
	require.Equal(t, "password", options.Password)
	require.Equal(t, "MULTIPLEXING_LOW", options.Multiplexing)
}

func TestMieruOptionsRejectUnknownFields(t *testing.T) {
	t.Parallel()

	inboundCtx := service.ContextWith[InboundOptionsRegistry](context.Background(), stubMieruInboundOptionsRegistry{})
	var inbound Inbound
	err := json.UnmarshalContext(inboundCtx, []byte(`{
		"type": "mieru",
		"tag": "mieru-in",
		"listen_port": 8964,
		"transport": "TCP",
		"unknown": true
	}`), &inbound)
	require.Error(t, err)

	outboundCtx := service.ContextWith[OutboundOptionsRegistry](context.Background(), stubMieruOutboundOptionsRegistry{})
	var outbound Outbound
	err = json.UnmarshalContext(outboundCtx, []byte(`{
		"type": "mieru",
		"tag": "mieru-out",
		"server": "127.0.0.1",
		"server_port": 8964,
		"transport": "TCP",
		"unknown": true
	}`), &outbound)
	require.Error(t, err)
}
