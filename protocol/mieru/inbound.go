package mieru

import (
	"context"
	"encoding/hex"
	"io"
	"net"
	"net/netip"
	"sync"
	"time"

	mierucommon "github.com/enfein/mieru/v3/apis/common"
	mieruconstant "github.com/enfein/mieru/v3/apis/constant"
	mierumodel "github.com/enfein/mieru/v3/apis/model"
	mieruserver "github.com/enfein/mieru/v3/apis/server"
	mierutp "github.com/enfein/mieru/v3/apis/trafficpattern"
	mierupb "github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	mierucipher "github.com/enfein/mieru/v3/pkg/cipher"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/buf"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/singlink/singlink/adapter"
	"github.com/singlink/singlink/adapter/inbound"
	"github.com/singlink/singlink/common/listener"
	"github.com/singlink/singlink/common/uot"
	C "github.com/singlink/singlink/constant"
	"github.com/singlink/singlink/log"
	"github.com/singlink/singlink/option"
	"google.golang.org/protobuf/proto"
)

func RegisterInbound(registry *inbound.Registry) {
	inbound.Register[option.MieruInboundOptions](registry, C.TypeMieru, NewInbound)
}

type Inbound struct {
	inbound.Adapter
	ctx           context.Context
	router        adapter.ConnectionRouterEx
	logger        log.ContextLogger
	server        mieruserver.Server
	listenOptions option.ListenOptions
	mu            sync.Mutex
}

func NewInbound(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.MieruInboundOptions) (adapter.Inbound, error) {
	options.ListenOptions.UDPFragmentDefault = true
	config, err := buildMieruServerConfig(logger, options)
	if err != nil {
		return nil, E.Cause(err, "build mieru server config")
	}

	server := mieruserver.NewServer()
	if err = server.Store(config); err != nil {
		return nil, E.Cause(err, "store mieru server config")
	}

	return &Inbound{
		Adapter:       inbound.NewAdapter(C.TypeMieru, tag),
		ctx:           ctx,
		router:        uot.NewRouter(router, logger),
		logger:        logger,
		server:        server,
		listenOptions: options.ListenOptions,
	}, nil
}

func (h *Inbound) Start(stage adapter.StartStage) error {
	if stage != adapter.StartStateStart {
		return nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if err := h.server.Start(); err != nil {
		return E.Cause(err, "start mieru server")
	}
	h.logger.Info("mieru server started")
	go h.acceptLoop()
	return nil
}

func (h *Inbound) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.server.Stop()
}

func (h *Inbound) acceptLoop() {
	backoff := 100 * time.Millisecond
	for {
		conn, request, err := h.server.Accept()
		if err != nil {
			if !h.server.IsRunning() {
				return
			}
			select {
			case <-h.ctx.Done():
				return
			default:
			}
			h.logger.Debug("failed to accept mieru connection: ", err)
			time.Sleep(backoff)
			if backoff < time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = 100 * time.Millisecond
		go h.handleConnection(conn, request)
	}
}

func (h *Inbound) handleConnection(conn net.Conn, request *mierumodel.Request) {
	ctx := log.ContextWithNewID(h.ctx)

	switch request.Command {
	case mieruconstant.Socks5ConnectCmd, mieruconstant.Socks5UDPAssociateCmd:
	default:
		if err := writeMieruResponse(conn, mieruconstant.Socks5ReplyCommandNotSupported); err != nil {
			h.logger.DebugContext(ctx, "write mieru response: ", err)
		}
		conn.Close()
		h.logger.WarnContext(ctx, "unsupported mieru command: ", request.Command)
		return
	}

	if err := writeMieruResponse(conn, mieruconstant.Socks5ReplySuccess); err != nil {
		conn.Close()
		h.logger.DebugContext(ctx, "write mieru response: ", err)
		return
	}

	var metadata adapter.InboundContext
	metadata.Inbound = h.Tag()
	metadata.InboundType = h.Type()
	//nolint:staticcheck
	metadata.InboundDetour = h.listenOptions.Detour
	metadata.UDPDisableDomainUnmapping = h.listenOptions.UDPDisableDomainUnmapping

	if remoteAddr := conn.RemoteAddr(); remoteAddr != nil {
		metadata.Source = M.SocksaddrFromNet(remoteAddr).Unwrap()
	}
	metadata.Destination = addrSpecToSocksaddr(request.DstAddr)

	if userCtx, ok := conn.(mierucommon.UserContext); ok {
		metadata.User = userCtx.UserName()
	}

	switch request.Command {
	case mieruconstant.Socks5ConnectCmd:
		if metadata.User != "" {
			h.logger.InfoContext(ctx, "[", metadata.User, "] inbound mieru connection to ", metadata.Destination)
		} else {
			h.logger.InfoContext(ctx, "inbound mieru connection to ", metadata.Destination)
		}
		h.router.RouteConnectionEx(ctx, conn, metadata, nil)
	case mieruconstant.Socks5UDPAssociateCmd:
		if metadata.User != "" {
			h.logger.InfoContext(ctx, "[", metadata.User, "] inbound mieru packet connection")
		} else {
			h.logger.InfoContext(ctx, "inbound mieru packet connection")
		}
		h.router.RoutePacketConnectionEx(ctx, &mieruPacketConn{
			PacketConn: mierucommon.NewPacketOverStreamTunnel(conn),
		}, metadata, nil)
	default:
		conn.Close()
		h.logger.WarnContext(ctx, "unsupported mieru command: ", request.Command)
	}
}

func writeMieruResponse(conn net.Conn, reply byte) error {
	return (&mierumodel.Response{
		Reply: reply,
		BindAddr: mierumodel.AddrSpec{
			IP:   net.IPv4zero,
			Port: 0,
		},
	}).WriteToSocks5(conn)
}

type mieruListenerFactory struct {
	logger  log.ContextLogger
	options option.ListenOptions
}

func (f *mieruListenerFactory) Listen(ctx context.Context, network string, address string) (net.Listener, error) {
	listenOptions := f.options
	if port, ok := portFromAddress(address); ok {
		listenOptions.ListenPort = port
	}
	l := listener.New(listener.Options{
		Context: ctx,
		Logger:  f.logger,
		Network: []string{network},
		Listen:  listenOptions,
	})
	return l.ListenTCP()
}

func (f *mieruListenerFactory) ListenPacket(ctx context.Context, network string, address string) (net.PacketConn, error) {
	listenOptions := f.options
	if port, ok := portFromAddress(address); ok {
		listenOptions.ListenPort = port
	}
	l := listener.New(listener.Options{
		Context: ctx,
		Logger:  f.logger,
		Network: []string{network},
		Listen:  listenOptions,
	})
	return l.ListenUDP()
}

func portFromAddress(address string) (uint16, bool) {
	socksaddr := M.ParseSocksaddr(address)
	if socksaddr.Port == 0 {
		return 0, false
	}
	return socksaddr.Port, true
}

type mieruPacketConn struct {
	net.PacketConn
}

var _ N.PacketConn = (*mieruPacketConn)(nil)

func (c *mieruPacketConn) ReadPacket(buffer *buf.Buffer) (destination M.Socksaddr, err error) {
	n, _, err := c.PacketConn.ReadFrom(buffer.FreeBytes())
	if err != nil {
		return M.Socksaddr{}, err
	}
	buffer.Truncate(n)
	if buffer.Len() < 3 {
		return M.Socksaddr{}, io.ErrShortBuffer
	}
	header := buffer.Bytes()[:3]
	if header[0] != 0 || header[1] != 0 {
		return M.Socksaddr{}, E.New("invalid mieru UDP reserved header")
	}
	if header[2] != 0 {
		return M.Socksaddr{}, E.New("mieru UDP fragment is not supported")
	}
	buffer.Advance(3)

	var addr mierumodel.AddrSpec
	if err = addr.ReadFromSocks5(buffer); err != nil {
		return M.Socksaddr{}, err
	}
	return addrSpecToSocksaddr(addr), nil
}

func (c *mieruPacketConn) WritePacket(buffer *buf.Buffer, destination M.Socksaddr) error {
	defer buffer.Release()

	header := buf.NewSize(3 + M.MaxSocksaddrLength)
	defer header.Release()
	common.Must(header.WriteZeroN(3))

	if err := socksaddrToAddrSpec(destination).WriteToSocks5(header); err != nil {
		return err
	}

	packet := buf.NewSize(header.Len() + buffer.Len())
	defer packet.Release()
	common.Must1(packet.Write(header.Bytes()))
	common.Must1(packet.Write(buffer.Bytes()))
	_, err := c.PacketConn.WriteTo(packet.Bytes(), nil)
	return err
}

func buildMieruServerConfig(logger log.ContextLogger, options option.MieruInboundOptions) (*mieruserver.ServerConfig, error) {
	if err := validateMieruInboundOptions(options); err != nil {
		return nil, err
	}

	transportProtocol, err := mieruTransportProtocol(options.Transport)
	if err != nil {
		return nil, err
	}

	var trafficPattern *mierupb.TrafficPattern
	if options.TrafficPattern != "" {
		trafficPattern, err = mierutp.Decode(options.TrafficPattern)
		if err != nil {
			return nil, E.Cause(err, "decode traffic_pattern")
		}
		if err = mierutp.Validate(trafficPattern); err != nil {
			return nil, E.Cause(err, "validate traffic_pattern")
		}
	}

	advancedSettings := &mierupb.ServerAdvancedSettings{
		UserHintIsMandatory: proto.Bool(true),
	}

	return &mieruserver.ServerConfig{
		Config: &mierupb.ServerConfig{
			PortBindings: []*mierupb.PortBinding{
				{
					Port:     proto.Int32(int32(options.ListenOptions.ListenPort)),
					Protocol: transportProtocol,
				},
			},
			Users: common.Map(options.Users, func(user option.MieruUser) *mierupb.User {
				return &mierupb.User{
					Name:           proto.String(user.Name),
					HashedPassword: proto.String(hex.EncodeToString(mierucipher.HashPassword([]byte(user.Password), []byte(user.Name)))),
				}
			}),
			TrafficPattern:   trafficPattern,
			AdvancedSettings: advancedSettings,
		},
		StreamListenerFactory: &mieruListenerFactory{
			logger:  logger,
			options: options.ListenOptions,
		},
		PacketListenerFactory: &mieruListenerFactory{
			logger:  logger,
			options: options.ListenOptions,
		},
	}, nil
}

func validateMieruInboundOptions(options option.MieruInboundOptions) error {
	if options.ListenOptions.ListenPort == 0 {
		return E.New("missing listen_port")
	}
	if _, err := mieruTransportProtocol(options.Transport); err != nil {
		return err
	}
	if len(options.Users) == 0 {
		return E.New("missing users")
	}
	for index, user := range options.Users {
		if err := validateMieruUser(user.Name, user.Password); err != nil {
			return E.Cause(err, "users[", index, "]")
		}
	}
	return nil
}

func addrSpecToSocksaddr(addr mierumodel.AddrSpec) M.Socksaddr {
	if addr.FQDN != "" {
		return M.Socksaddr{
			Fqdn: addr.FQDN,
			Port: uint16(addr.Port),
		}
	}
	if addr.IP != nil {
		netAddr, _ := netip.AddrFromSlice(addr.IP)
		return M.Socksaddr{
			Addr: netAddr.Unmap(),
			Port: uint16(addr.Port),
		}
	}
	return M.Socksaddr{}
}

func socksaddrToAddrSpec(destination M.Socksaddr) mierumodel.AddrSpec {
	var addr mierumodel.AddrSpec
	if destination.IsFqdn() {
		addr.FQDN = destination.Fqdn
	} else if destination.Addr.IsValid() {
		addr.IP = destination.Addr.AsSlice()
	}
	addr.Port = int(destination.Port)
	return addr
}
