package v2board

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/bufio"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/singlink/singlink/adapter"
	C "github.com/singlink/singlink/constant"
	"github.com/singlink/singlink/route"
)

func TestRestoreAliveRequeuesSnapshot(t *testing.T) {
	state := &nodeTraffic{}
	state.updateUsers(23, []UserInfo{{
		ID:   7,
		UUID: "00000000-0000-0000-0000-000000000007",
	}}, nil, nodeRules{})
	state.markOnline("00000000-0000-0000-0000-000000000007", "192.0.2.1")

	_, alive := state.snapshot(0, 0)
	if got := alive[7]; len(got) != 1 || got[0] != "192.0.2.1_23" {
		t.Fatalf("unexpected alive snapshot: %#v", alive)
	}
	_, drained := state.snapshot(0, 0)
	if len(drained) != 0 {
		t.Fatalf("alive snapshot was not drained: %#v", drained)
	}

	state.restoreAlive(alive)
	_, restored := state.snapshot(0, 0)
	if got := restored[7]; len(got) != 1 || got[0] != "192.0.2.1_23" {
		t.Fatalf("unexpected restored alive snapshot: %#v", restored)
	}
}

func TestRestoreAliveCapsRestoredAddresses(t *testing.T) {
	const user = "00000000-0000-0000-0000-000000000007"
	state := &nodeTraffic{}
	state.updateUsers(23, []UserInfo{{
		ID:   7,
		UUID: user,
	}}, nil, nodeRules{})
	alive := make(map[int][]string)
	for i := range maxRestoredAliveIPsPerUser + 50 {
		alive[7] = append(alive[7], M.ParseSocksaddrHostPort("192.0.2.1", uint16(i+1)).String()+"_23")
	}

	state.restoreAlive(alive)
	_, restored := state.snapshot(0, 0)
	if got := len(restored[7]); got != maxRestoredAliveIPsPerUser {
		t.Fatalf("expected capped restored alive list, got %d", got)
	}
}

func TestTrafficTrackerRegistryRefCount(t *testing.T) {
	router := &route.Router{}
	key := routerKey(router)
	trackerRegistry.Lock()
	delete(trackerRegistry.trackers, key)
	trackerRegistry.Unlock()

	first := retainTrafficTracker(router)
	second := retainTrafficTracker(router)
	if first != second {
		t.Fatal("expected tracker to be shared for the same router")
	}
	releaseTrafficTracker(router, first)
	trackerRegistry.Lock()
	ref, loaded := trackerRegistry.trackers[key]
	trackerRegistry.Unlock()
	if !loaded || ref.refs != 1 {
		t.Fatalf("expected one retained tracker, got loaded=%v ref=%#v", loaded, ref)
	}
	releaseTrafficTracker(router, second)
	trackerRegistry.Lock()
	_, loaded = trackerRegistry.trackers[key]
	trackerRegistry.Unlock()
	if loaded {
		t.Fatal("tracker registry entry was not released")
	}
}

func TestUserTrackerKeyFallsBackToPanelLabel(t *testing.T) {
	state := &nodeTraffic{}
	state.updateUsers(23, []UserInfo{{
		ID:    7,
		Label: "user-7",
	}}, nil, nodeRules{})

	if counter := state.counter("user-7"); counter == nil {
		t.Fatal("expected label based counter")
	}
	if counter := state.counter(""); counter != nil {
		t.Fatal("empty user should not match")
	}
}

func TestUserTrackerKeyKeepsUUIDForNonNaiveWhenUsernameIsPresent(t *testing.T) {
	const uuid = "00000000-0000-0000-0000-000000000007"
	state := &nodeTraffic{}
	state.updateUsersWithAliveStateForNodeType(C.TypeVMess, 23, []UserInfo{{
		ID:       7,
		UUID:     uuid,
		Username: "user-7",
	}}, nil, time.Now(), defaultAliveListTTL, nodeRules{})

	if counter := state.counter(uuid); counter == nil {
		t.Fatal("expected uuid based counter for non-naive user")
	}
	if counter := state.counter("user-7"); counter != nil {
		t.Fatal("non-naive tracker must not prefer username over uuid")
	}
}

func TestUserTrackerKeyUsesUsernameForNaive(t *testing.T) {
	const uuid = "00000000-0000-0000-0000-000000000007"
	state := &nodeTraffic{}
	state.updateUsersWithAliveStateForNodeType(C.TypeNaive, 23, []UserInfo{{
		ID:       7,
		UUID:     uuid,
		Username: "user-7",
		Password: uuid,
	}}, nil, time.Now(), defaultAliveListTTL, nodeRules{})

	if counter := state.counter("user-7"); counter == nil {
		t.Fatal("expected username based counter for naive user")
	}
	if counter := state.counter(uuid); counter != nil {
		t.Fatal("naive tracker must prefer username over uuid")
	}
}

func TestDeviceLimitRejectsNewAddressWhenPanelLimitReached(t *testing.T) {
	state := &nodeTraffic{}
	state.updateUsers(23, []UserInfo{{
		ID:          7,
		UUID:        "00000000-0000-0000-0000-000000000007",
		DeviceLimit: 1,
	}}, map[int]int{7: 1}, nodeRules{})

	if state.markOnline("00000000-0000-0000-0000-000000000007", "192.0.2.2") {
		t.Fatal("expected new address to be rejected when panel alive count reaches device limit")
	}
}

func TestDeviceLimitIgnoresExpiredPanelAliveCount(t *testing.T) {
	const user = "00000000-0000-0000-0000-000000000007"
	state := &nodeTraffic{}
	state.updateUsersWithAliveState(23, []UserInfo{{
		ID:          7,
		UUID:        user,
		DeviceLimit: 1,
	}}, map[int]int{7: 1}, time.Now().Add(-2*time.Minute), time.Minute, nodeRules{})

	if !state.markOnline(user, "192.0.2.2") {
		t.Fatal("stale panel alive count should not reject a new address")
	}
}

func TestDeviceLimitCountsLocallyTrackedNewAddresses(t *testing.T) {
	const user = "00000000-0000-0000-0000-000000000007"
	state := &nodeTraffic{}
	state.updateUsers(23, []UserInfo{{
		ID:          7,
		UUID:        user,
		DeviceLimit: 1,
	}}, nil, nodeRules{})

	if !state.markOnline(user, "192.0.2.1") {
		t.Fatal("expected first address to be accepted")
	}
	if state.markOnline(user, "192.0.2.2") {
		t.Fatal("expected second address to be rejected before the next panel alive poll")
	}
}

func TestDeviceLimitAllowsAlreadyTrackedAddress(t *testing.T) {
	state := &nodeTraffic{}
	state.updateUsers(23, []UserInfo{{
		ID:          7,
		UUID:        "00000000-0000-0000-0000-000000000007",
		DeviceLimit: 1,
	}}, map[int]int{7: 1}, nodeRules{})
	state.previous = map[string]map[string]struct{}{
		"00000000-0000-0000-0000-000000000007": {"192.0.2.1": {}},
	}

	if !state.markOnline("00000000-0000-0000-0000-000000000007", "192.0.2.1") {
		t.Fatal("expected existing address to remain allowed")
	}
}

func TestUpdateUsersPreservesRemovedUserPendingTraffic(t *testing.T) {
	const user = "00000000-0000-0000-0000-000000000007"
	state := &nodeTraffic{}
	state.updateUsers(23, []UserInfo{{
		ID:   7,
		UUID: user,
	}}, nil, nodeRules{})
	counter := state.counter(user)
	counter.upload.Add(1024)
	counter.download.Add(2048)

	state.updateUsers(23, nil, nil, nodeRules{})

	traffic, _ := state.snapshot(0, 0)
	if got := trafficForUID(traffic, 7); got == nil || got.Upload != 1024 || got.Download != 2048 {
		t.Fatalf("removed user traffic was not preserved: %#v", traffic)
	}
	if len(state.pending) != 0 {
		t.Fatalf("drained pending counter was not pruned: %#v", state.pending)
	}
}

func TestSnapshotKeepsActiveRemovedUserPendingCounter(t *testing.T) {
	const user = "00000000-0000-0000-0000-000000000007"
	state := &nodeTraffic{}
	state.updateUsers(23, []UserInfo{{
		ID:   7,
		UUID: user,
	}}, nil, nodeRules{})
	counter := state.counter(user)
	counter.active.Add(1)
	counter.upload.Add(1024)
	state.updateUsers(23, nil, nil, nodeRules{})

	traffic, _ := state.snapshot(0, 0)
	if got := trafficForUID(traffic, 7); got == nil || got.Upload != 1024 {
		t.Fatalf("removed user traffic was not preserved: %#v", traffic)
	}
	if pending := state.pending[7]; len(pending) != 1 || pending[0] != counter {
		t.Fatalf("active pending counter was pruned: %#v", state.pending)
	}

	counter.upload.Add(512)
	counter.active.Add(-1)
	traffic, _ = state.snapshot(0, 0)
	if got := trafficForUID(traffic, 7); got == nil || got.Upload != 512 {
		t.Fatalf("active pending counter did not report final traffic: %#v", traffic)
	}
	if len(state.pending) != 0 {
		t.Fatalf("inactive pending counter was not pruned: %#v", state.pending)
	}
}

func TestRestoreTrafficPreservesRemovedUserPendingTraffic(t *testing.T) {
	const user = "00000000-0000-0000-0000-000000000007"
	state := &nodeTraffic{}
	state.updateUsers(23, []UserInfo{{
		ID:   7,
		UUID: user,
	}}, nil, nodeRules{})
	counter := state.counter(user)
	counter.upload.Add(1024)
	counter.download.Add(2048)
	state.updateUsers(23, nil, nil, nodeRules{})

	traffic, _ := state.snapshot(0, 0)
	state.restoreTraffic(traffic)

	restored, _ := state.snapshot(0, 0)
	if got := trafficForUID(restored, 7); got == nil || got.Upload != 1024 || got.Download != 2048 {
		t.Fatalf("removed user traffic was not restored: %#v", restored)
	}
}

func TestSpeedLimiterIsSharedAndRefreshed(t *testing.T) {
	const user = "00000000-0000-0000-0000-000000000007"
	state := &nodeTraffic{}
	state.updateUsers(23, []UserInfo{{
		ID:         7,
		UUID:       user,
		SpeedLimit: 8,
	}}, nil, nodeRules{})
	first := state.speedLimiter(user)
	if first == nil {
		t.Fatal("expected speed limiter")
	}

	state.updateUsers(23, []UserInfo{{
		ID:         7,
		UUID:       user,
		SpeedLimit: 8,
	}}, nil, nodeRules{})
	if second := state.speedLimiter(user); second != first {
		t.Fatal("expected unchanged speed limit to reuse limiter")
	}

	state.updateUsers(23, []UserInfo{{
		ID:         7,
		UUID:       user,
		SpeedLimit: 16,
	}}, nil, nodeRules{})
	if third := state.speedLimiter(user); third == nil || third == first {
		t.Fatal("expected changed speed limit to rebuild limiter")
	}
}

func TestRoutedConnectionAppliesSpeedLimiter(t *testing.T) {
	const user = "00000000-0000-0000-0000-000000000007"
	tracker := &TrafficTracker{nodes: map[string]*nodeTraffic{"node": {}}}
	tracker.nodes["node"].updateUsers(23, []UserInfo{{
		ID:         7,
		UUID:       user,
		SpeedLimit: 8,
	}}, nil, nodeRules{})
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	conn := tracker.RoutedConnection(context.Background(), client, adapter.InboundContext{
		Inbound:     "node",
		User:        user,
		Source:      M.ParseSocksaddrHostPort("192.0.2.1", 12345),
		Destination: M.ParseSocksaddrHostPort("example.com", 443),
	}, nil, nil)
	counter, ok := conn.(*bufio.CounterConn)
	if !ok {
		t.Fatalf("expected counter conn, got %T", conn)
	}
	if _, ok = counter.Upstream().(*rateLimitedConn); !ok {
		t.Fatalf("expected rate limited upstream, got %T", counter.Upstream())
	}
}

func TestRoutedConnectionRejectsBlockedDestination(t *testing.T) {
	const user = "00000000-0000-0000-0000-000000000007"
	tracker := &TrafficTracker{nodes: map[string]*nodeTraffic{"node": {}}}
	tracker.nodes["node"].updateUsers(23, []UserInfo{{
		ID:   7,
		UUID: user,
	}}, nil, nodeRules{
		domainFull: []string{"blocked.example"},
	})
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	conn := tracker.RoutedConnection(context.Background(), client, adapter.InboundContext{
		Inbound:     "node",
		User:        user,
		Source:      M.ParseSocksaddrHostPort("192.0.2.1", 12345),
		Destination: M.ParseSocksaddrHostPort("blocked.example", 443),
	}, nil, nil)
	if conn != nil {
		t.Fatalf("expected blocked connection to abort, got %T", conn)
	}
}

func TestRoutedConnectionRejectsDeviceLimit(t *testing.T) {
	const user = "00000000-0000-0000-0000-000000000007"
	tracker := &TrafficTracker{nodes: map[string]*nodeTraffic{"node": {}}}
	tracker.nodes["node"].updateUsers(23, []UserInfo{{
		ID:          7,
		UUID:        user,
		DeviceLimit: 1,
	}}, map[int]int{7: 1}, nodeRules{})
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	conn := tracker.RoutedConnection(context.Background(), client, adapter.InboundContext{
		Inbound:     "node",
		User:        user,
		Source:      M.ParseSocksaddrHostPort("192.0.2.2", 12345),
		Destination: M.ParseSocksaddrHostPort("example.com", 443),
	}, nil, nil)
	if conn != nil {
		t.Fatalf("expected device-limited connection to abort, got %T", conn)
	}
}

func TestRoutedPacketConnectionAppliesSpeedLimiter(t *testing.T) {
	const user = "00000000-0000-0000-0000-000000000007"
	tracker := &TrafficTracker{nodes: map[string]*nodeTraffic{"node": {}}}
	tracker.nodes["node"].updateUsers(23, []UserInfo{{
		ID:         7,
		UUID:       user,
		SpeedLimit: 8,
	}}, nil, nodeRules{})

	conn := tracker.RoutedPacketConnection(context.Background(), &fakePacketConn{}, adapter.InboundContext{
		Inbound:     "node",
		User:        user,
		Source:      M.ParseSocksaddrHostPort("192.0.2.1", 12345),
		Destination: M.ParseSocksaddrHostPort("example.com", 443),
	}, nil, nil)
	counter, ok := conn.(*bufio.CounterPacketConn)
	if !ok {
		t.Fatalf("expected counter packet conn, got %T", conn)
	}
	if _, ok = counter.Upstream().(*rateLimitedPacketConn); !ok {
		t.Fatalf("expected rate limited packet upstream, got %T", counter.Upstream())
	}
}

func TestSpeedLimitBytesPerSecondUsesMbps(t *testing.T) {
	if got := speedLimitBytesPerSecond(8); got != 1000000 {
		t.Fatalf("unexpected speed limit conversion: %d", got)
	}
}

func TestBuildNodeRulesSupportsPanelBlockRoutes(t *testing.T) {
	rules, err := buildNodeRules([]ServerRoute{
		{
			ID:     1,
			Match:  StringList{"regexp:.*\\.example\\.com$", "domain:example.net", "full:exact.example.org", "keyword:video", "protocol:bittorrent"},
			Action: "block",
		},
		{
			ID:     2,
			Match:  StringList{"regexp:ignored\\.example"},
			Action: "dns",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !rules.blocks(M.ParseSocksaddrHostPort("www.example.com", 443), "") {
		t.Fatal("expected regexp domain rule to block")
	}
	if !rules.blocks(M.ParseSocksaddrHostPort("cdn.example.net", 443), "") {
		t.Fatal("expected domain suffix rule to block")
	}
	if !rules.blocks(M.ParseSocksaddrHostPort("exact.example.org", 443), "") {
		t.Fatal("expected full domain rule to block")
	}
	if rules.blocks(M.ParseSocksaddrHostPort("www.exact.example.org", 443), "") {
		t.Fatal("full domain rule should not block subdomains")
	}
	if !rules.blocks(M.ParseSocksaddrHostPort("video.example.org", 443), "") {
		t.Fatal("expected keyword domain rule to block")
	}
	if !rules.blocks(M.ParseSocksaddrHostPort("example.net", 443), "bittorrent") {
		t.Fatal("expected protocol rule to block")
	}
	if rules.blocks(M.ParseSocksaddrHostPort("ignored.example", 443), "") {
		t.Fatal("non-block route should not block")
	}
}

func TestBuildNodeRulesSupportsPanelProtocolRoute(t *testing.T) {
	rules, err := buildNodeRules([]ServerRoute{{
		ID:     1,
		Match:  StringList{"bittorrent", "quic"},
		Action: "protocol",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !rules.blocks(M.ParseSocksaddrHostPort("example.com", 443), "quic") {
		t.Fatal("expected protocol action to block matching protocol")
	}
	if rules.blocks(M.ParseSocksaddrHostPort("example.com", 443), "tls") {
		t.Fatal("unexpected block for non-matching protocol")
	}
}

func TestBuildNodeRulesSupportsPanelBlockIPRoutes(t *testing.T) {
	rules, err := buildNodeRules([]ServerRoute{{
		ID:     1,
		Match:  StringList{"192.0.2.8", "198.51.100.0/24", "geoip:private", "geoip:cn"},
		Action: "block_ip",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !rules.blocks(M.ParseSocksaddrHostPort("192.0.2.8", 443), "") {
		t.Fatal("expected exact ip rule to block")
	}
	if !rules.blocks(M.ParseSocksaddrHostPort("198.51.100.9", 443), "") {
		t.Fatal("expected cidr rule to block")
	}
	if !rules.blocks(M.ParseSocksaddrHostPort("10.0.0.1", 443), "") {
		t.Fatal("expected geoip:private rule to block private address")
	}
	if rules.blocks(M.ParseSocksaddrHostPort("203.0.113.9", 443), "") {
		t.Fatal("unexpected block for non-matching ip")
	}
}

func TestBuildNodeRulesSupportsPanelBlockPortRoutes(t *testing.T) {
	rules, err := buildNodeRules([]ServerRoute{{
		ID:     1,
		Match:  StringList{"53", "1000-2000", "3000:4000"},
		Action: "block_port",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !rules.blocks(M.ParseSocksaddrHostPort("example.com", 53), "") {
		t.Fatal("expected exact port rule to block")
	}
	if !rules.blocks(M.ParseSocksaddrHostPort("example.com", 1500), "") {
		t.Fatal("expected dash port range to block")
	}
	if !rules.blocks(M.ParseSocksaddrHostPort("example.com", 3500), "") {
		t.Fatal("expected colon port range to block")
	}
	if rules.blocks(M.ParseSocksaddrHostPort("example.com", 443), "") {
		t.Fatal("unexpected block for non-matching port")
	}
}

func TestBuildNodeRulesRejectsInvalidRegexp(t *testing.T) {
	_, err := buildNodeRules([]ServerRoute{{
		ID:     1,
		Match:  StringList{"regexp:["},
		Action: "block",
	}})
	if err == nil {
		t.Fatal("expected invalid regexp error")
	}
}

type fakePacketConn struct{}

func (c *fakePacketConn) ReadPacket(buffer *buf.Buffer) (M.Socksaddr, error) {
	return M.Socksaddr{}, nil
}

func (c *fakePacketConn) WritePacket(buffer *buf.Buffer, destination M.Socksaddr) error {
	buffer.Release()
	return nil
}

func (c *fakePacketConn) Close() error {
	return nil
}

func (c *fakePacketConn) LocalAddr() net.Addr {
	return fakeAddr("local")
}

func (c *fakePacketConn) SetDeadline(time.Time) error {
	return nil
}

func (c *fakePacketConn) SetReadDeadline(time.Time) error {
	return nil
}

func (c *fakePacketConn) SetWriteDeadline(time.Time) error {
	return nil
}

type fakeAddr string

func (a fakeAddr) Network() string {
	return string(a)
}

func (a fakeAddr) String() string {
	return string(a)
}

func trafficForUID(traffic []UserTraffic, uid int) *UserTraffic {
	for index := range traffic {
		if traffic[index].UID == uid {
			return &traffic[index]
		}
	}
	return nil
}
