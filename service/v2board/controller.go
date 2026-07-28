package v2board

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/singlink/singlink/adapter"
	C "github.com/singlink/singlink/constant"
	"github.com/singlink/singlink/log"
	"github.com/singlink/singlink/option"
)

const (
	maxControllerBackoff  = 5 * time.Minute
	statusShutdownTimeout = 5 * time.Second
)

var shadowsocksAppliedFeatures = []string{
	"shadowsocks-uot-v1",
	"shadowsocks-uot-v2",
	"shadowsocks-sing-mux-v1",
}

type Controller struct {
	ctx             context.Context
	logger          log.ContextLogger
	inbound         adapter.InboundManager
	router          adapter.Router
	tracker         *TrafficTracker
	client          *Client
	options         effectiveNodeOptions
	node            *NodeInfo
	users           []UserInfo
	activeUsers     []UserInfo
	aliveList       map[int]int
	aliveUpdated    time.Time
	nodeHash        [32]byte
	userHash        [32]byte
	current         option.Inbound
	inboundReady    bool
	pendingBuild    bool
	statusSupported bool
	statusReady     bool
	appliedRevision string
	appliedFeatures []string
	statusReset     chan struct{}
	statusWake      chan struct{}
	statusRunning   bool
	lifecycle       sync.Mutex
	closed          bool
}

func NewController(ctx context.Context, logger log.ContextLogger, inbound adapter.InboundManager, router adapter.Router, tracker *TrafficTracker, client *Client, options effectiveNodeOptions) *Controller {
	return &Controller{
		ctx:         ctx,
		logger:      logger,
		inbound:     inbound,
		router:      router,
		tracker:     tracker,
		client:      client,
		options:     options,
		statusReset: make(chan struct{}, 1),
		statusWake:  make(chan struct{}, 1),
	}
}

func (c *Controller) Run() {
	c.lifecycle.Lock()
	if c.statusReset == nil {
		c.statusReset = make(chan struct{}, 1)
	}
	if c.statusWake == nil {
		c.statusWake = make(chan struct{}, 1)
	}
	if c.statusRunning {
		c.lifecycle.Unlock()
		return
	}
	c.statusRunning = true
	c.lifecycle.Unlock()

	statusDone := make(chan struct{})
	go func() {
		defer close(statusDone)
		c.runStatusLoop()
	}()

	pullTimer := time.NewTimer(0)
	pushTimer := time.NewTimer(c.options.PushInterval)
	var pullBackoff controllerBackoff
	var pushBackoff controllerBackoff
	defer func() {
		stopTimer(pullTimer)
		stopTimer(pushTimer)
		<-statusDone
		c.lifecycle.Lock()
		c.statusRunning = false
		c.lifecycle.Unlock()
	}()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-pullTimer.C:
			err := c.sync()
			if err != nil && !errors.Is(err, context.Canceled) {
				c.logger.Error(E.Cause(err, "v2board sync ", c.options.Tag))
			}
			resetTimer(pullTimer, pullBackoff.Next(c.options.PullInterval, err, c.options.Tag+"/pull"))
		case <-pushTimer.C:
			err := c.push()
			if err != nil && !errors.Is(err, context.Canceled) {
				c.logger.Error(E.Cause(err, "v2board report ", c.options.Tag))
			}
			resetTimer(pushTimer, pushBackoff.Next(c.options.PushInterval, err, c.options.Tag+"/push"))
		}
	}
}

func (c *Controller) runStatusLoop() {
	statusTimer := time.NewTimer(c.statusInterval())
	var statusBackoff controllerBackoff
	defer stopTimer(statusTimer)

	report := func() {
		err := c.reportStatus()
		if err != nil && !errors.Is(err, context.Canceled) {
			c.warn(E.Cause(err, "v2board status ", c.options.Tag))
		}
		resetTimer(
			statusTimer,
			statusBackoff.Next(c.statusInterval(), err, c.options.Tag+"/status"),
		)
	}

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-statusTimer.C:
			report()
		case <-c.statusWake:
			report()
		case <-c.statusReset:
			statusBackoff = controllerBackoff{}
			resetTimer(statusTimer, c.statusInterval())
		}
	}
}

func (c *Controller) Close() error {
	c.lifecycle.Lock()
	if c.closed {
		c.lifecycle.Unlock()
		return nil
	}
	c.closed = true
	c.updateAppliedStatusLocked(false)
	_, reportStatus := shadowsocksStatusRevision(c.node)
	status := NodeStatus{
		Ready:           false,
		AppliedRevision: "",
		AppliedFeatures: []string{},
		Version:         C.Version,
	}
	var result error
	err := c.inbound.Remove(c.options.Tag)
	if err != nil && !errors.Is(err, os.ErrInvalid) {
		result = E.Append(result, err, func(err error) error {
			return err
		})
	}
	c.tracker.DeleteNode(c.options.Tag)
	client := c.client
	c.lifecycle.Unlock()

	if reportStatus && client != nil {
		statusContext, cancel := context.WithTimeout(context.Background(), statusShutdownTimeout)
		if err := client.ReportNodeStatus(statusContext, status); err != nil {
			c.warn(E.Cause(err, "v2board final status ", c.options.Tag))
		}
		cancel()
	}
	if client != nil {
		result = E.Append(result, client.Close(), func(err error) error {
			return err
		})
	}
	return result
}

func (c *Controller) sync() error {
	var users []UserInfo
	now := time.Now()
	node, err := c.client.GetNodeInfo(c.ctx)
	if err != nil {
		if !errors.Is(err, ErrNotModified) {
			if !errors.Is(err, ErrEmptyUserList) {
				return err
			}
			users = []UserInfo{}
		} else {
			node = nil
		}
	}
	nodeChanged := false
	if node != nil {
		hash := digestNode(node)
		c.lifecycle.Lock()
		c.node = node
		_, c.statusSupported = shadowsocksStatusRevision(node)
		c.lifecycle.Unlock()
		c.applyPanelIntervals(node)
		if hash != c.nodeHash {
			c.nodeHash = hash
			nodeChanged = true
		}
	}
	if users == nil {
		users, err = c.client.GetUserList(c.ctx)
		if err != nil {
			if !errors.Is(err, ErrNotModified) {
				c.reportUnavailableStatusBestEffort()
				return err
			}
			users = nil
		}
	}
	usersChanged := false
	if users != nil {
		c.users = users
	}
	activeUsers := activePanelUsers(c.users, now)
	hash := digestUsers(activeUsers)
	c.activeUsers = activeUsers
	if hash != c.userHash {
		c.userHash = hash
		usersChanged = true
	}
	aliveList, err := c.client.GetAliveList(c.ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		c.warn(E.Cause(err, "v2board alive list ", c.options.Tag))
	} else {
		aliveUpdated := time.Now()
		c.aliveList = aliveList
		c.aliveUpdated = aliveUpdated
		if c.inboundReady {
			c.tracker.updateAliveList(c.options.Tag, aliveList, aliveUpdated, c.options.PullInterval)
		}
	}
	if c.node == nil {
		if users != nil && len(activeUsers) == 0 {
			return c.clearInbound(nodeRules{})
		}
		return fmt.Errorf("node config not loaded")
	}
	if !nodeChanged && !usersChanged && c.inboundReady && !c.pendingBuild {
		return nil
	}
	c.pendingBuild = true
	if err := c.rebuildInbound(); err != nil {
		return err
	}
	c.pendingBuild = false
	return nil
}

func (c *Controller) rebuildInbound() error {
	rules, err := buildNodeRules(c.node.Common.Routes)
	if err != nil {
		c.reportUnavailableStatusBestEffort()
		return err
	}
	if len(c.activeUsers) == 0 {
		return c.clearInbound(rules)
	}
	inbound, err := BuildInbound(c.options.Tag, c.node, c.activeUsers, MapperOptions{
		Listen:      c.options.Listen,
		TCPFastOpen: c.options.TCPFastOpen,
		InboundTLS:  c.options.TLS,
		Multiplex:   c.options.Multiplex,
	})
	if err != nil {
		c.reportUnavailableStatusBestEffort()
		return err
	}
	if err := c.applyInbound(inbound); err != nil {
		return err
	}
	c.lifecycle.Lock()
	if c.closed {
		c.lifecycle.Unlock()
		return context.Canceled
	}
	c.tracker.updateNode(c.options.Tag, inbound.Type, c.options.NodeID, c.activeUsers, c.aliveList, c.aliveUpdated, c.options.PullInterval, rules)
	c.inboundReady = true
	c.updateAppliedStatusLocked(true)
	c.lifecycle.Unlock()
	c.reportStatusBestEffort()
	c.logger.Info("v2board node ", c.options.Tag, " loaded with ", len(c.activeUsers), " users")
	return nil
}

func (c *Controller) applyInbound(inbound option.Inbound) error {
	c.lifecycle.Lock()
	reportNotReady := false
	defer func() {
		c.lifecycle.Unlock()
		if reportNotReady {
			c.reportStatusBestEffort()
		}
	}()
	if c.closed {
		return context.Canceled
	}
	err := c.inbound.Create(c.ctx, c.router, c.logger, c.options.Tag, inbound.Type, inbound.Options)
	if err == nil {
		c.current = inbound
		return nil
	}
	if !c.inboundReady || c.current.Type == "" || !isLikelyListenConflict(err) {
		if !c.inboundReady || c.current.Type == "" {
			c.inboundReady = false
			c.updateAppliedStatusLocked(false)
			reportNotReady = true
		}
		return err
	}

	if removeErr := c.inbound.Remove(c.options.Tag); removeErr != nil && !errors.Is(removeErr, os.ErrInvalid) {
		if _, loaded := c.inbound.Get(c.options.Tag); loaded {
			return E.Errors(err, E.Cause(removeErr, "remove previous inbound"))
		}
		if restoreErr := c.restoreCurrentInbound(); restoreErr != nil {
			c.current = option.Inbound{}
			c.inboundReady = false
			c.updateAppliedStatusLocked(false)
			reportNotReady = true
			return E.Errors(
				err,
				E.Cause(removeErr, "remove previous inbound"),
				E.Cause(restoreErr, "restore previous inbound after failed removal"),
			)
		}
		return E.Errors(err, E.Cause(removeErr, "remove previous inbound"))
	}
	replaceErr := c.inbound.Create(c.ctx, c.router, c.logger, c.options.Tag, inbound.Type, inbound.Options)
	if replaceErr == nil {
		c.current = inbound
		return nil
	}
	if restoreErr := c.restoreCurrentInbound(); restoreErr != nil {
		c.current = option.Inbound{}
		c.inboundReady = false
		c.updateAppliedStatusLocked(false)
		reportNotReady = true
		return E.Errors(replaceErr, E.Cause(restoreErr, "restore previous inbound after failed replacement"))
	}
	return replaceErr
}

func (c *Controller) restoreCurrentInbound() error {
	if c.current.Type == "" {
		return nil
	}
	return c.inbound.Create(c.ctx, c.router, c.logger, c.options.Tag, c.current.Type, c.current.Options)
}

func (c *Controller) clearInbound(rules nodeRules) error {
	c.lifecycle.Lock()
	if c.closed {
		c.lifecycle.Unlock()
		return context.Canceled
	}
	err := c.inbound.Remove(c.options.Tag)
	if err != nil && !errors.Is(err, os.ErrInvalid) {
		if _, loaded := c.inbound.Get(c.options.Tag); loaded {
			c.lifecycle.Unlock()
			return err
		}
		nodeType := c.current.Type
		c.current = option.Inbound{}
		c.tracker.updateNode(c.options.Tag, nodeType, c.options.NodeID, nil, c.aliveList, c.aliveUpdated, c.options.PullInterval, rules)
		c.inboundReady = false
		c.updateAppliedStatusLocked(false)
		c.lifecycle.Unlock()
		c.reportStatusBestEffort()
		return err
	}
	nodeType := c.current.Type
	c.current = option.Inbound{}
	c.tracker.updateNode(c.options.Tag, nodeType, c.options.NodeID, nil, c.aliveList, c.aliveUpdated, c.options.PullInterval, rules)
	c.inboundReady = true
	c.updateAppliedStatusLocked(false)
	c.lifecycle.Unlock()
	c.reportStatusBestEffort()
	return nil
}

func isLikelyListenConflict(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "address already in use") ||
		strings.Contains(message, "only one usage of each socket address")
}

func (c *Controller) push() error {
	if !c.inboundReady {
		return nil
	}
	traffic, alive := c.tracker.Snapshot(c.options.Tag, c.options.NodeReportMinTraffic, c.options.DeviceOnlineMinTraffic)
	if err := c.client.ReportUserTraffic(c.ctx, traffic); err != nil {
		if len(traffic) > 0 {
			c.tracker.RestoreTraffic(c.options.Tag, traffic)
		}
		if len(alive) > 0 {
			c.tracker.RestoreAlive(c.options.Tag, alive)
		}
		return err
	}
	alivePayload := alive
	if alivePayload == nil {
		alivePayload = make(map[int][]string)
	}
	if err := c.client.ReportNodeOnlineUsers(c.ctx, &alivePayload); err != nil {
		if len(alive) > 0 {
			c.tracker.RestoreAlive(c.options.Tag, alive)
		}
		return err
	}
	return nil
}

func (c *Controller) updateAppliedStatusLocked(ready bool) {
	revision, supported := shadowsocksStatusRevision(c.node)
	c.statusSupported = supported
	c.statusReady = supported && ready
	c.appliedRevision = ""
	c.appliedFeatures = nil
	if c.statusReady {
		c.appliedRevision = revision
		c.appliedFeatures = append([]string(nil), shadowsocksAppliedFeatures...)
	}
}

func shadowsocksStatusRevision(node *NodeInfo) (string, bool) {
	if node == nil || node.Common == nil || normalizeNodeType(firstNonEmpty(node.Type, node.Common.Protocol)) != C.TypeShadowsocks {
		return "", false
	}
	revision := strings.TrimSpace(node.Common.ConfigRevision)
	return revision, revision != ""
}

func (c *Controller) reportStatusBestEffort() {
	c.lifecycle.Lock()
	statusRunning := c.statusRunning && !c.closed
	c.lifecycle.Unlock()
	if statusRunning {
		c.requestStatusReport()
		return
	}

	if err := c.reportStatus(); err != nil {
		c.warn(E.Cause(err, "v2board status ", c.options.Tag))
		return
	}
	c.resetStatusSchedule()
}

func (c *Controller) reportUnavailableStatusBestEffort() {
	c.lifecycle.Lock()
	if c.closed || (c.inboundReady && c.current.Type != "") {
		c.lifecycle.Unlock()
		return
	}
	c.inboundReady = false
	c.updateAppliedStatusLocked(false)
	c.lifecycle.Unlock()
	c.reportStatusBestEffort()
}

func (c *Controller) requestStatusReport() {
	select {
	case c.statusWake <- struct{}{}:
	default:
	}
}

func (c *Controller) resetStatusSchedule() {
	select {
	case c.statusReset <- struct{}{}:
	default:
	}
}

func (c *Controller) reportStatus() error {
	if c.client == nil {
		return nil
	}
	c.lifecycle.Lock()
	supported := c.statusSupported
	if !supported {
		_, supported = shadowsocksStatusRevision(c.node)
	}
	if !supported || c.closed {
		c.lifecycle.Unlock()
		return nil
	}
	status := NodeStatus{
		Ready:           c.statusReady,
		AppliedRevision: c.appliedRevision,
		AppliedFeatures: append([]string{}, c.appliedFeatures...),
		Version:         C.Version,
	}
	c.lifecycle.Unlock()
	return c.client.ReportNodeStatus(c.ctx, status)
}

func (c *Controller) applyPanelIntervals(node *NodeInfo) {
	c.lifecycle.Lock()
	previousPushInterval := c.options.PushInterval
	if node.PullInterval > 0 {
		c.options.PullInterval = normalizeInterval(node.PullInterval)
	}
	if node.PushInterval > 0 {
		c.options.PushInterval = normalizeInterval(node.PushInterval)
	}
	if c.options.PushInterval != previousPushInterval {
		c.resetStatusSchedule()
	}
	if node.Common != nil && node.Common.BaseConfig != nil {
		c.options.NodeReportMinTraffic = node.NodeReportMinTraffic
		c.options.DeviceOnlineMinTraffic = node.DeviceOnlineMinTraffic
	}
	c.lifecycle.Unlock()
}

func (c *Controller) statusInterval() time.Duration {
	c.lifecycle.Lock()
	interval := c.options.PushInterval
	c.lifecycle.Unlock()
	if interval <= 0 {
		return time.Minute
	}
	return interval
}

func (c *Controller) warn(args ...any) {
	if c.logger != nil {
		c.logger.Warn(args...)
	}
}

func activePanelUsers(users []UserInfo, now time.Time) []UserInfo {
	if len(users) == 0 {
		return nil
	}
	var filtered []UserInfo
	for _, user := range users {
		if !panelUserActive(user, now) {
			continue
		}
		filtered = append(filtered, user)
	}
	return filtered
}

func panelUserActive(user UserInfo, now time.Time) bool {
	if user.EnabledSet && !user.Enabled {
		return false
	}
	if user.ExpiresAt > 0 && user.ExpiresAt <= now.Unix() {
		return false
	}
	if expiresOnExpired(user.ExpiresOn, now) {
		return false
	}
	return true
}

func expiresOnExpired(value string, now time.Time) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
		expiresAt, err := time.Parse(layout, value)
		if err == nil {
			return !now.Before(expiresAt)
		}
	}
	expiresOn, err := time.Parse("2006-01-02", value)
	if err != nil {
		return false
	}
	return !now.Before(expiresOn.AddDate(0, 0, 1))
}

func digestNode(node *NodeInfo) [32]byte {
	if node == nil {
		return [32]byte{}
	}
	var commonConfig *ServerConfig
	if node.Common != nil {
		configCopy := *node.Common
		configCopy.BaseConfig = nil
		commonConfig = &configCopy
	}
	body, _ := json.Marshal(struct {
		Type     string        `json:"type,omitempty"`
		Security int           `json:"security,omitempty"`
		Common   *ServerConfig `json:"common,omitempty"`
	}{
		Type:     node.Type,
		Security: node.Security,
		Common:   commonConfig,
	})
	return sha256.Sum256(body)
}

func digestUsers(users []UserInfo) [32]byte {
	indexes := make([]int, len(users))
	for index := range indexes {
		indexes[index] = index
	}
	sort.Slice(indexes, func(i, j int) bool {
		left := users[indexes[i]]
		right := users[indexes[j]]
		if left.ID != right.ID {
			return left.ID < right.ID
		}
		if left.UUID != right.UUID {
			return left.UUID < right.UUID
		}
		if left.Username != right.Username {
			return left.Username < right.Username
		}
		if left.Password != right.Password {
			return left.Password < right.Password
		}
		if left.Label != right.Label {
			return left.Label < right.Label
		}
		if left.Secret != right.Secret {
			return left.Secret < right.Secret
		}
		if left.LinkSecret != right.LinkSecret {
			return left.LinkSecret < right.LinkSecret
		}
		if left.SpeedLimit != right.SpeedLimit {
			return left.SpeedLimit < right.SpeedLimit
		}
		if left.DeviceLimit != right.DeviceLimit {
			return left.DeviceLimit < right.DeviceLimit
		}
		if left.Port != right.Port {
			return left.Port < right.Port
		}
		if left.Cipher != right.Cipher {
			return left.Cipher < right.Cipher
		}
		if left.Enabled != right.Enabled {
			return !left.Enabled && right.Enabled
		}
		if left.MaxConnections != right.MaxConnections {
			return left.MaxConnections < right.MaxConnections
		}
		if left.MaxIPs != right.MaxIPs {
			return left.MaxIPs < right.MaxIPs
		}
		if left.QuotaBytes != right.QuotaBytes {
			return left.QuotaBytes < right.QuotaBytes
		}
		if left.ExpiresAt != right.ExpiresAt {
			return left.ExpiresAt < right.ExpiresAt
		}
		return left.ExpiresOn < right.ExpiresOn
	})
	hasher := sha256.New()
	writeString := func(value string) {
		var length [8]byte
		binary.LittleEndian.PutUint64(length[:], uint64(len(value)))
		_, _ = hasher.Write(length[:])
		_, _ = hasher.Write([]byte(value))
	}
	writeInt64 := func(value int64) {
		var buffer [8]byte
		binary.LittleEndian.PutUint64(buffer[:], uint64(value))
		_, _ = hasher.Write(buffer[:])
	}
	writeBool := func(value bool) {
		if value {
			_, _ = hasher.Write([]byte{1})
		} else {
			_, _ = hasher.Write([]byte{0})
		}
	}
	for _, index := range indexes {
		user := users[index]
		writeInt64(int64(user.ID))
		writeString(user.UUID)
		writeString(user.Username)
		writeString(user.Password)
		writeInt64(int64(user.SpeedLimit))
		writeInt64(int64(user.DeviceLimit))
		writeString(user.Label)
		writeString(user.Secret)
		writeString(user.LinkSecret)
		writeInt64(int64(user.Port))
		writeString(user.Cipher)
		writeBool(user.Enabled)
		writeInt64(int64(user.MaxConnections))
		writeInt64(int64(user.MaxIPs))
		writeInt64(user.QuotaBytes)
		writeInt64(user.ExpiresAt)
		writeString(user.ExpiresOn)
	}
	var digest [32]byte
	copy(digest[:], hasher.Sum(nil))
	return digest
}

type controllerBackoff struct {
	attempt int
	delay   time.Duration
}

func (b *controllerBackoff) Next(base time.Duration, err error, jitterKey string) time.Duration {
	base = runtimeInterval(base)
	if err == nil {
		b.attempt = 0
		b.delay = 0
		return base
	}
	if b.delay < base {
		b.delay = base
	} else {
		b.delay *= 2
		if b.delay > maxControllerBackoff {
			b.delay = maxControllerBackoff
		}
	}
	b.attempt++
	return jitterInterval(b.delay, jitterKey, b.attempt)
}

func runtimeInterval(duration time.Duration) time.Duration {
	if duration <= 0 {
		return time.Minute
	}
	return duration
}

func jitterInterval(duration time.Duration, key string, attempt int) time.Duration {
	if duration <= 0 {
		return duration
	}
	seed := sha256.Sum256(fmt.Appendf(nil, "%s/%d/%d", key, attempt, duration))
	offset := int(binary.BigEndian.Uint16(seed[:2])%41) - 20
	jittered := duration + duration*time.Duration(offset)/100
	if jittered < minimumInterval {
		return minimumInterval
	}
	if jittered > maxControllerBackoff {
		return maxControllerBackoff
	}
	return jittered
}

func resetTimer(timer *time.Timer, duration time.Duration) {
	if duration <= 0 {
		duration = time.Minute
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(duration)
}

func stopTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}
