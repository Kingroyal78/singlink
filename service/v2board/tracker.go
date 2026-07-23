package v2board

import (
	"context"
	"fmt"
	"maps"
	"net"
	"net/netip"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sagernet/sing/common/bufio"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/singlink/singlink/adapter"
	C "github.com/singlink/singlink/constant"
)

type TrafficTracker struct {
	access sync.RWMutex
	nodes  map[string]*nodeTraffic
}

type nodeTraffic struct {
	access   sync.Mutex
	nodeID   int
	users    map[string]int
	limits   map[string]userLimit
	counters map[string]*userCounter
	pending  map[int][]*userCounter
	online   map[string]map[string]struct{}
	previous map[string]map[string]struct{}
	alive    map[int]int
	aliveAt  time.Time
	aliveTTL time.Duration
	rules    nodeRules
}

type userLimit struct {
	speed   int
	device  int
	limiter *rateLimiter
}

type userCounter struct {
	upload   atomic.Int64
	download atomic.Int64
	active   atomic.Int32
}

type trafficTotal struct {
	upload   int64
	download int64
}

type nodeRules struct {
	domainRegex   []*regexp.Regexp
	domainSuffix  []string
	domainFull    []string
	domainKeyword []string
	ipPrefixes    []netip.Prefix
	ipPrivate     bool
	ports         []portRange
	protocol      []string
}

type portRange struct {
	start uint16
	end   uint16
}

const maxRestoredAliveIPsPerUser = 256

const defaultAliveListTTL = time.Minute

func normalizeAliveListTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return defaultAliveListTTL
	}
	return ttl
}

var trackerRegistry = struct {
	sync.Mutex
	trackers map[uintptr]*trafficTrackerRef
}{
	trackers: make(map[uintptr]*trafficTrackerRef),
}

type trafficTrackerRef struct {
	tracker *TrafficTracker
	refs    int
}

func retainTrafficTracker(router adapter.Router) *TrafficTracker {
	key := routerKey(router)
	trackerRegistry.Lock()
	defer trackerRegistry.Unlock()
	ref, loaded := trackerRegistry.trackers[key]
	if loaded {
		ref.refs++
		return ref.tracker
	}
	ref = &trafficTrackerRef{tracker: &TrafficTracker{
		nodes: make(map[string]*nodeTraffic),
	}, refs: 1}
	trackerRegistry.trackers[key] = ref
	router.AppendTracker(ref.tracker)
	return ref.tracker
}

func releaseTrafficTracker(router adapter.Router, tracker *TrafficTracker) {
	if tracker == nil {
		return
	}
	key := routerKey(router)
	trackerRegistry.Lock()
	defer trackerRegistry.Unlock()
	ref, loaded := trackerRegistry.trackers[key]
	if !loaded || ref.tracker != tracker {
		return
	}
	ref.refs--
	if ref.refs > 0 {
		return
	}
	delete(trackerRegistry.trackers, key)
	router.RemoveTracker(tracker)
}

func routerKey(router adapter.Router) uintptr {
	value := reflect.ValueOf(router)
	if value.Kind() == reflect.Ptr || value.Kind() == reflect.UnsafePointer {
		return value.Pointer()
	}
	return uintptr(reflect.ValueOf(&router).Pointer())
}

func (t *TrafficTracker) UpdateNode(tag string, nodeID int, users []UserInfo, alive map[int]int, rules nodeRules) {
	t.updateNode(tag, "", nodeID, users, alive, time.Now(), defaultAliveListTTL, rules)
}

func (t *TrafficTracker) updateNode(tag string, nodeType string, nodeID int, users []UserInfo, alive map[int]int, aliveAt time.Time, aliveTTL time.Duration, rules nodeRules) {
	t.access.Lock()
	defer t.access.Unlock()
	state, loaded := t.nodes[tag]
	if !loaded {
		state = &nodeTraffic{}
		t.nodes[tag] = state
	}
	state.updateUsersWithAliveStateForNodeType(nodeType, nodeID, users, alive, aliveAt, aliveTTL, rules)
}

func (t *TrafficTracker) UpdateAliveList(tag string, alive map[int]int) {
	t.updateAliveList(tag, alive, time.Now(), defaultAliveListTTL)
}

func (t *TrafficTracker) updateAliveList(tag string, alive map[int]int, aliveAt time.Time, aliveTTL time.Duration) {
	t.access.RLock()
	state := t.nodes[tag]
	t.access.RUnlock()
	if state == nil {
		return
	}
	state.updateAliveListWithState(alive, aliveAt, aliveTTL)
}

func (t *TrafficTracker) DeleteNode(tag string) {
	t.access.Lock()
	delete(t.nodes, tag)
	t.access.Unlock()
}

func (t *TrafficTracker) Snapshot(tag string, trafficMin int64, onlineMin int64) ([]UserTraffic, map[int][]string) {
	t.access.RLock()
	state := t.nodes[tag]
	t.access.RUnlock()
	if state == nil {
		return nil, nil
	}
	return state.snapshot(trafficMin, onlineMin)
}

func (t *TrafficTracker) RestoreTraffic(tag string, traffic []UserTraffic) {
	if len(traffic) == 0 {
		return
	}
	t.access.RLock()
	state := t.nodes[tag]
	t.access.RUnlock()
	if state == nil {
		return
	}
	state.restoreTraffic(traffic)
}

func (t *TrafficTracker) RestoreAlive(tag string, alive map[int][]string) {
	if len(alive) == 0 {
		return
	}
	t.access.RLock()
	state := t.nodes[tag]
	t.access.RUnlock()
	if state == nil {
		return
	}
	state.restoreAlive(alive)
}

func (t *TrafficTracker) RoutedConnection(ctx context.Context, conn net.Conn, metadata adapter.InboundContext, matchedRule adapter.Rule, matchOutbound adapter.Outbound) net.Conn {
	state, counter := t.match(metadata)
	if counter == nil {
		return conn
	}
	if state.blocked(metadata.Destination, metadata.Protocol) {
		_ = conn.Close()
		return nil
	}
	if !state.markOnline(metadata.User, metadata.Source.Addr.String()) {
		_ = conn.Close()
		return nil
	}
	if limiter := state.speedLimiter(metadata.User); limiter != nil {
		conn = counter.trackConn(conn)
		conn = newRateLimitedConn(conn, limiter)
	} else {
		conn = counter.trackConn(conn)
	}
	return bufio.NewInt64CounterConn(conn, []*atomic.Int64{&counter.upload}, []*atomic.Int64{&counter.download})
}

func (t *TrafficTracker) RoutedPacketConnection(ctx context.Context, conn N.PacketConn, metadata adapter.InboundContext, matchedRule adapter.Rule, matchOutbound adapter.Outbound) N.PacketConn {
	state, counter := t.match(metadata)
	if counter == nil {
		return conn
	}
	protocol := metadata.Protocol
	if protocol == "" {
		protocol = metadata.Destination.Network()
	}
	if state.blocked(metadata.Destination, protocol) {
		_ = conn.Close()
		return nil
	}
	if !state.markOnline(metadata.User, metadata.Source.Addr.String()) {
		_ = conn.Close()
		return nil
	}
	if limiter := state.speedLimiter(metadata.User); limiter != nil {
		conn = counter.trackPacketConn(conn)
		conn = newRateLimitedPacketConn(conn, limiter)
	} else {
		conn = counter.trackPacketConn(conn)
	}
	return bufio.NewInt64CounterPacketConn(conn, []*atomic.Int64{&counter.upload}, nil, []*atomic.Int64{&counter.download}, nil)
}

func (t *TrafficTracker) match(metadata adapter.InboundContext) (*nodeTraffic, *userCounter) {
	if metadata.Inbound == "" || metadata.User == "" {
		return nil, nil
	}
	t.access.RLock()
	state := t.nodes[metadata.Inbound]
	t.access.RUnlock()
	if state == nil {
		return nil, nil
	}
	return state, state.counter(metadata.User)
}

func (n *nodeTraffic) updateUsers(nodeID int, users []UserInfo, alive map[int]int, rules nodeRules) {
	n.updateUsersWithAliveState(nodeID, users, alive, time.Now(), defaultAliveListTTL, rules)
}

func (n *nodeTraffic) updateUsersWithAliveState(nodeID int, users []UserInfo, alive map[int]int, aliveAt time.Time, aliveTTL time.Duration, rules nodeRules) {
	n.updateUsersWithAliveStateForNodeType("", nodeID, users, alive, aliveAt, aliveTTL, rules)
}

func (n *nodeTraffic) updateUsersWithAliveStateForNodeType(nodeType string, nodeID int, users []UserInfo, alive map[int]int, aliveAt time.Time, aliveTTL time.Duration, rules nodeRules) {
	n.access.Lock()
	defer n.access.Unlock()
	n.nodeID = nodeID
	n.rules = rules
	n.alive = cloneAliveList(alive)
	n.aliveAt = aliveAt
	n.aliveTTL = normalizeAliveListTTL(aliveTTL)
	nextUsers := make(map[string]int, len(users))
	nextLimits := make(map[string]userLimit, len(users))
	nextCounters := make(map[string]*userCounter, len(users))
	nextOnline := make(map[string]map[string]struct{}, len(users))
	nextPrevious := make(map[string]map[string]struct{}, len(users))
	nextPending := clonePendingCounters(n.pending)
	for _, user := range users {
		key := userTrackerKey(nodeType, user)
		if key == "" {
			continue
		}
		nextUsers[key] = user.ID
		sameUser := n.users[key] == user.ID
		if existing := n.counters[key]; existing != nil && sameUser {
			nextCounters[key] = existing
		} else {
			if existing != nil {
				nextPending = appendPendingCounter(nextPending, n.users[key], existing)
			}
			nextCounters[key] = new(userCounter)
		}
		if existing := n.online[key]; existing != nil && sameUser {
			nextOnline[key] = existing
		}
		if existing := n.previous[key]; existing != nil && sameUser {
			nextPrevious[key] = existing
		}
		nextLimits[key] = n.updatedUserLimit(key, user)
	}
	for key, counter := range n.counters {
		if _, carried := nextCounters[key]; carried {
			continue
		}
		nextPending = appendPendingCounter(nextPending, n.users[key], counter)
	}
	n.users = nextUsers
	n.limits = nextLimits
	n.counters = nextCounters
	n.pending = nextPending
	n.online = nextOnline
	n.previous = nextPrevious
}

func (n *nodeTraffic) updatedUserLimit(key string, user UserInfo) userLimit {
	nextLimit := userLimit{
		speed:  user.SpeedLimit,
		device: user.DeviceLimit,
	}
	if user.SpeedLimit <= 0 {
		return nextLimit
	}
	if existing := n.limits[key]; existing.speed == user.SpeedLimit && existing.limiter != nil {
		nextLimit.limiter = existing.limiter
	} else {
		nextLimit.limiter = newRateLimiter(user.SpeedLimit)
	}
	return nextLimit
}

func (n *nodeTraffic) updateAliveListWithState(alive map[int]int, aliveAt time.Time, aliveTTL time.Duration) {
	n.access.Lock()
	defer n.access.Unlock()
	n.alive = cloneAliveList(alive)
	n.aliveAt = aliveAt
	n.aliveTTL = normalizeAliveListTTL(aliveTTL)
}

func (n *nodeTraffic) counter(user string) *userCounter {
	n.access.Lock()
	defer n.access.Unlock()
	if _, loaded := n.users[user]; !loaded {
		return nil
	}
	return n.counters[user]
}

func (n *nodeTraffic) speedLimiter(user string) *rateLimiter {
	n.access.Lock()
	defer n.access.Unlock()
	return n.limits[user].limiter
}

func userTrackerKey(nodeType string, user UserInfo) string {
	switch normalizeNodeType(nodeType) {
	case C.TypeMieru, C.TypeNaive:
		if user.Username != "" {
			return user.Username
		}
	}
	if user.UUID != "" {
		return user.UUID
	}
	if user.Label != "" {
		return user.Label
	}
	if user.ID > 0 {
		return fmt.Sprintf("user-%d", user.ID)
	}
	return ""
}

func (n *nodeTraffic) blocked(destination M.Socksaddr, protocol string) bool {
	n.access.Lock()
	defer n.access.Unlock()
	return n.rules.blocks(destination, protocol)
}

func buildNodeRules(routes []ServerRoute) (nodeRules, error) {
	var rules nodeRules
	for _, route := range routes {
		action := strings.ToLower(strings.TrimSpace(route.Action))
		switch action {
		case "block":
			for _, item := range route.Match {
				if err := rules.addDomainBlockRule(route.ID, item); err != nil {
					return nodeRules{}, err
				}
			}
		case "block_ip":
			for _, item := range route.Match {
				if err := rules.addIPBlockRule(route.ID, item); err != nil {
					return nodeRules{}, err
				}
			}
		case "block_port":
			for _, item := range route.Match {
				if err := rules.addPortBlockRule(route.ID, item); err != nil {
					return nodeRules{}, err
				}
			}
		case "protocol":
			for _, item := range route.Match {
				rules.addProtocolRule(item)
			}
		}
	}
	return rules, nil
}

func (r *nodeRules) addDomainBlockRule(routeID int, item string) error {
	item = strings.TrimSpace(item)
	if item == "" {
		return nil
	}
	if protocol, loaded := strings.CutPrefix(item, "protocol:"); loaded {
		r.addProtocolRule(protocol)
		return nil
	}
	if pattern, loaded := strings.CutPrefix(item, "regexp:"); loaded {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			return nil
		}
		matcher, err := regexp.Compile(pattern)
		if err != nil {
			return fmt.Errorf("v2board route %d: compile regexp %q: %w", routeID, pattern, err)
		}
		r.domainRegex = append(r.domainRegex, matcher)
		return nil
	}
	if domain, loaded := strings.CutPrefix(item, "domain:"); loaded {
		domain = normalizeDomainRuleValue(domain)
		if domain != "" {
			r.domainSuffix = append(r.domainSuffix, domain)
		}
		return nil
	}
	if domain, loaded := strings.CutPrefix(item, "full:"); loaded {
		domain = normalizeDomainRuleValue(domain)
		if domain != "" {
			r.domainFull = append(r.domainFull, domain)
		}
		return nil
	}
	if keyword, loaded := strings.CutPrefix(item, "keyword:"); loaded {
		keyword = normalizeDomainRuleValue(keyword)
		if keyword != "" {
			r.domainKeyword = append(r.domainKeyword, keyword)
		}
		return nil
	}
	if _, loaded := strings.CutPrefix(item, "geosite:"); loaded {
		return nil
	}
	keyword := normalizeDomainRuleValue(item)
	if keyword != "" {
		r.domainKeyword = append(r.domainKeyword, keyword)
	}
	return nil
}

func (r *nodeRules) addIPBlockRule(routeID int, item string) error {
	item = strings.TrimSpace(item)
	if item == "" {
		return nil
	}
	if code, loaded := strings.CutPrefix(strings.ToLower(item), "geoip:"); loaded {
		if code == "private" {
			r.ipPrivate = true
		}
		return nil
	}
	if prefix, err := netip.ParsePrefix(item); err == nil {
		r.ipPrefixes = append(r.ipPrefixes, prefix)
		return nil
	}
	addr, err := netip.ParseAddr(item)
	if err != nil {
		return fmt.Errorf("v2board route %d: parse ip/cidr %q: %w", routeID, item, err)
	}
	r.ipPrefixes = append(r.ipPrefixes, netip.PrefixFrom(addr, addr.BitLen()))
	return nil
}

func (r *nodeRules) addPortBlockRule(routeID int, item string) error {
	item = strings.TrimSpace(item)
	if item == "" {
		return nil
	}
	var startText, endText string
	if index := strings.IndexAny(item, "-:"); index >= 0 {
		startText = strings.TrimSpace(item[:index])
		endText = strings.TrimSpace(item[index+1:])
	} else {
		startText = item
		endText = item
	}
	start, err := parseRulePort(startText)
	if err != nil {
		return fmt.Errorf("v2board route %d: parse port %q: %w", routeID, item, err)
	}
	end, err := parseRulePort(endText)
	if err != nil {
		return fmt.Errorf("v2board route %d: parse port %q: %w", routeID, item, err)
	}
	if start > end {
		return fmt.Errorf("v2board route %d: invalid port range %q", routeID, item)
	}
	r.ports = append(r.ports, portRange{start: start, end: end})
	return nil
}

func (r *nodeRules) addProtocolRule(item string) {
	item = strings.TrimSpace(item)
	if item != "" {
		r.protocol = append(r.protocol, item)
	}
}

func parseRulePort(value string) (uint16, error) {
	if value == "" {
		return 0, fmt.Errorf("empty port")
	}
	port, err := strconv.ParseUint(value, 10, 16)
	if err != nil {
		return 0, err
	}
	if port == 0 {
		return 0, fmt.Errorf("port must be greater than zero")
	}
	return uint16(port), nil
}

func normalizeDomainRuleValue(value string) string {
	return strings.Trim(strings.ToLower(strings.TrimSpace(value)), ".")
}

func (r nodeRules) blocks(destination M.Socksaddr, protocol string) bool {
	destination = destination.Unwrap()
	domain := normalizeDomainRuleValue(destination.AddrString())
	for _, matcher := range r.domainRegex {
		if matcher.MatchString(domain) {
			return true
		}
	}
	if slices.Contains(r.domainFull, domain) {
		return true
	}
	for _, suffix := range r.domainSuffix {
		if domain == suffix || strings.HasSuffix(domain, "."+suffix) {
			return true
		}
	}
	for _, keyword := range r.domainKeyword {
		if strings.Contains(domain, keyword) {
			return true
		}
	}
	if destination.IsIP() {
		for _, prefix := range r.ipPrefixes {
			if prefix.Contains(destination.Addr) {
				return true
			}
		}
		if r.ipPrivate && destination.Addr.IsPrivate() {
			return true
		}
	}
	for _, port := range r.ports {
		if destination.Port >= port.start && destination.Port <= port.end {
			return true
		}
	}
	if protocol == "" {
		return false
	}
	protocol = strings.TrimSpace(protocol)
	return slices.Contains(r.protocol, protocol)
}

func (n *nodeTraffic) markOnline(user string, ip string) bool {
	if ip == "" || ip == "<invalid IP>" {
		return true
	}
	n.access.Lock()
	defer n.access.Unlock()
	uid, loaded := n.users[user]
	if !loaded {
		return false
	}
	if n.exceedsDeviceLimitLocked(user, uid, ip) {
		return false
	}
	online := n.online[user]
	if online == nil {
		online = make(map[string]struct{})
		n.online[user] = online
	}
	addOnlineIPWithLimit(online, ip, maxRestoredAliveIPsPerUser)
	return true
}

func (n *nodeTraffic) snapshot(trafficMin int64, onlineMin int64) ([]UserTraffic, map[int][]string) {
	n.access.Lock()
	defer n.access.Unlock()
	totals := make(map[int]trafficTotal, len(n.counters)+len(n.pending))
	alive := make(map[int][]string)
	previous := make(map[string]map[string]struct{}, len(n.online))
	for uid, counters := range n.pending {
		remaining := counters[:0]
		for _, counter := range counters {
			addCounterSnapshot(totals, uid, counter)
			if counter != nil && counter.active.Load() > 0 {
				remaining = append(remaining, counter)
			}
		}
		if len(remaining) > 0 {
			n.pending[uid] = remaining
		} else {
			delete(n.pending, uid)
		}
	}
	for user, counter := range n.counters {
		uid, loaded := n.users[user]
		if !loaded {
			continue
		}
		upload, download := addCounterSnapshot(totals, uid, counter)
		total := upload + download
		if total >= onlineMin {
			for ip := range n.online[user] {
				alive[uid] = append(alive[uid], fmt.Sprintf("%s_%d", ip, n.nodeID))
			}
		}
		if len(n.online[user]) > 0 {
			previous[user] = cloneStringSet(n.online[user])
		}
		delete(n.online, user)
	}
	n.previous = previous
	if len(alive) == 0 {
		alive = nil
	}
	traffic := make([]UserTraffic, 0, len(totals))
	for uid, total := range totals {
		amount := total.upload + total.download
		if amount >= trafficMin && amount > 0 {
			traffic = append(traffic, UserTraffic{
				UID:      uid,
				Upload:   total.upload,
				Download: total.download,
			})
		}
	}
	return traffic, alive
}

func addCounterSnapshot(totals map[int]trafficTotal, uid int, counter *userCounter) (int64, int64) {
	if uid <= 0 || counter == nil {
		return 0, 0
	}
	upload := counter.upload.Swap(0)
	download := counter.download.Swap(0)
	total := totals[uid]
	total.upload += upload
	total.download += download
	totals[uid] = total
	return upload, download
}

func (n *nodeTraffic) exceedsDeviceLimitLocked(user string, uid int, ip string) bool {
	limit := n.limits[user].device
	if limit <= 0 {
		return false
	}
	if _, loaded := n.online[user][ip]; loaded {
		return false
	}
	if _, loaded := n.previous[user][ip]; loaded {
		return false
	}
	localNew := 0
	for trackedIP := range n.online[user] {
		if _, loaded := n.previous[user][trackedIP]; !loaded {
			localNew++
		}
	}
	remoteAlive := 0
	if n.aliveListFresh(time.Now()) {
		remoteAlive = n.alive[uid]
	}
	return remoteAlive+localNew >= limit
}

func (n *nodeTraffic) aliveListFresh(now time.Time) bool {
	if n.aliveAt.IsZero() {
		return false
	}
	return now.Sub(n.aliveAt) <= normalizeAliveListTTL(n.aliveTTL)
}

func (c *userCounter) trackConn(conn net.Conn) net.Conn {
	if c == nil {
		return conn
	}
	c.active.Add(1)
	return &trackedConn{
		Conn:    conn,
		counter: c,
	}
}

func (c *userCounter) trackPacketConn(conn N.PacketConn) N.PacketConn {
	if c == nil {
		return conn
	}
	c.active.Add(1)
	return &trackedPacketConn{
		PacketConn: conn,
		counter:    c,
	}
}

type trackedConn struct {
	net.Conn
	counter *userCounter
	once    sync.Once
}

func (c *trackedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() {
		c.counter.active.Add(-1)
	})
	return err
}

func (c *trackedConn) Upstream() any {
	return c.Conn
}

type trackedPacketConn struct {
	N.PacketConn
	counter *userCounter
	once    sync.Once
}

func (c *trackedPacketConn) Close() error {
	err := c.PacketConn.Close()
	c.once.Do(func() {
		c.counter.active.Add(-1)
	})
	return err
}

func (c *trackedPacketConn) Upstream() any {
	return c.PacketConn
}

func cloneAliveList(alive map[int]int) map[int]int {
	if len(alive) == 0 {
		return nil
	}
	cloned := make(map[int]int, len(alive))
	maps.Copy(cloned, alive)
	return cloned
}

func cloneStringSet(values map[string]struct{}) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]struct{}, len(values))
	for value := range values {
		cloned[value] = struct{}{}
	}
	return cloned
}

func clonePendingCounters(values map[int][]*userCounter) map[int][]*userCounter {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[int][]*userCounter, len(values))
	for uid, counters := range values {
		if len(counters) > 0 {
			cloned[uid] = append([]*userCounter(nil), counters...)
		}
	}
	return cloned
}

func appendPendingCounter(values map[int][]*userCounter, uid int, counter *userCounter) map[int][]*userCounter {
	if uid <= 0 || counter == nil {
		return values
	}
	if values == nil {
		values = make(map[int][]*userCounter)
	}
	values[uid] = append(values[uid], counter)
	return values
}

func (n *nodeTraffic) restoreTraffic(traffic []UserTraffic) {
	n.access.Lock()
	defer n.access.Unlock()
	for _, entry := range traffic {
		counter := n.counterForUIDLocked(entry.UID)
		if counter != nil {
			counter.upload.Add(entry.Upload)
			counter.download.Add(entry.Download)
		}
	}
}

func (n *nodeTraffic) counterForUIDLocked(uid int) *userCounter {
	if uid <= 0 {
		return nil
	}
	for user, userID := range n.users {
		if userID != uid {
			continue
		}
		counter := n.counters[user]
		if counter != nil {
			return counter
		}
	}
	if pending := n.pending[uid]; len(pending) > 0 {
		return pending[0]
	}
	counter := new(userCounter)
	n.pending = appendPendingCounter(n.pending, uid, counter)
	return counter
}

func (n *nodeTraffic) restoreAlive(alive map[int][]string) {
	n.access.Lock()
	defer n.access.Unlock()
	for uid, values := range alive {
		for user, userID := range n.users {
			if userID != uid {
				continue
			}
			online := n.online[user]
			if online == nil {
				online = make(map[string]struct{})
				n.online[user] = online
			}
			for _, value := range values {
				ip := value
				if index := strings.LastIndex(value, "_"); index > 0 {
					ip = value[:index]
				}
				if ip != "" {
					addOnlineIPWithLimit(online, ip, maxRestoredAliveIPsPerUser)
				}
			}
			break
		}
	}
}

func addOnlineIPWithLimit(online map[string]struct{}, ip string, limit int) {
	if limit <= 0 {
		online[ip] = struct{}{}
		return
	}
	if _, exists := online[ip]; !exists && len(online) >= limit {
		for existing := range online {
			delete(online, existing)
			break
		}
	}
	online[ip] = struct{}{}
}
