package v2board

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultTimeout           = 30 * time.Second
	DefaultUserAgent         = "singlink v2board"
	DefaultErrorBodyLimit    = 4 << 10
	DefaultConfigBodyLimit   = 4 << 20
	DefaultAliveBodyLimit    = 16 << 20
	DefaultUserListBodyLimit = 16 << 20
	DefaultDrainBodyLimit    = 512 << 10
)

const (
	APIStyleUniProxy            = "uniproxy"
	APIStyleDeepbwork           = "deepbwork"
	APIStyleTrojanTidalab       = "trojan_tidalab"
	APIStyleShadowsocksTidalab  = "shadowsocks_tidalab"
	APIStyleShadowsocksTidalab2 = "shadowsockstidalab"
)

var (
	ErrNotModified   = errors.New("v2board: not modified")
	ErrEmptyUserList = errors.New("v2board: empty user list")
)

type Client struct {
	httpClient     *http.Client
	ownsHTTPClient bool
	baseURL        *url.URL
	nodeConfig     NodeConfig
	apiVersion     int
	apiStyle       string
	userAgent      string
	errorBodyLimit int64
	userBodyLimit  int64

	access     sync.Mutex
	configETag string
	userETag   string
}

func New(options Options) (*Client, error) {
	return NewClient(options)
}

func NewClient(optionsValue any, legacy ...any) (*Client, error) {
	options, err := clientOptions(optionsValue, legacy...)
	if err != nil {
		return nil, err
	}
	if options.Timeout < 0 {
		return nil, fmt.Errorf("v2board: timeout must not be negative")
	}
	if options.Timeout == 0 {
		options.Timeout = DefaultTimeout
	}
	if options.UserAgent == "" {
		options.UserAgent = DefaultUserAgent
	}
	if options.ErrorBodyLimit <= 0 {
		options.ErrorBodyLimit = DefaultErrorBodyLimit
	}
	if options.UserListBodyLimit <= 0 {
		options.UserListBodyLimit = DefaultUserListBodyLimit
	}
	if options.APIVersion == 0 {
		options.APIVersion = 1
	}
	if options.APIVersion != 1 && options.APIVersion != 2 {
		return nil, fmt.Errorf("v2board: unsupported api_version %d", options.APIVersion)
	}
	options.APIStyle = normalizeAPIStyleForNodeType(options.APIStyle, options.NodeConfig.NodeType)
	if !validAPIStyle(options.APIStyle) {
		return nil, fmt.Errorf("v2board: unsupported api_style %q", options.APIStyle)
	}
	ownsHTTPClient := options.HTTPClient == nil
	if options.HTTPClient == nil {
		options.HTTPClient, err = newHTTPClient(options.Timeout, options.APISendIP)
		if err != nil {
			return nil, err
		}
	}
	baseURL, nodeConfig, err := normalizeOptions(options)
	if err != nil {
		return nil, err
	}
	return &Client{
		httpClient:     options.HTTPClient,
		ownsHTTPClient: ownsHTTPClient,
		baseURL:        baseURL,
		nodeConfig:     nodeConfig,
		apiVersion:     options.APIVersion,
		apiStyle:       options.APIStyle,
		userAgent:      options.UserAgent,
		errorBodyLimit: options.ErrorBodyLimit,
		userBodyLimit:  options.UserListBodyLimit,
	}, nil
}

func newHTTPClient(timeout time.Duration, apiSendIP string) (*http.Client, error) {
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("v2board: default HTTP transport is not *http.Transport")
	}
	transport := defaultTransport.Clone()
	client := &http.Client{Timeout: timeout, Transport: transport}
	apiSendIP = strings.TrimSpace(apiSendIP)
	if apiSendIP != "" {
		addr, err := netip.ParseAddr(apiSendIP)
		if err != nil {
			return nil, fmt.Errorf("v2board: parse api_send_ip: %w", err)
		}
		dialer := &net.Dialer{
			Timeout: timeout,
			LocalAddr: &net.TCPAddr{
				IP: net.IP(addr.AsSlice()),
			},
		}
		transport.DialContext = dialer.DialContext
	}
	return client, nil
}

func (c *Client) Close() error {
	if c != nil && c.ownsHTTPClient && c.httpClient != nil {
		c.httpClient.CloseIdleConnections()
	}
	return nil
}

func clientOptions(optionsValue any, legacy ...any) (Options, error) {
	if len(legacy) == 0 {
		options, ok := optionsValue.(Options)
		if !ok {
			return Options{}, fmt.Errorf("v2board: invalid client options type %T", optionsValue)
		}
		return options, nil
	}
	apiHost, ok := optionsValue.(string)
	if !ok {
		return Options{}, fmt.Errorf("v2board: invalid api host type %T", optionsValue)
	}
	if len(legacy) != 4 {
		return Options{}, fmt.Errorf("v2board: invalid legacy client options")
	}
	apiKey, ok := legacy[0].(string)
	if !ok {
		return Options{}, fmt.Errorf("v2board: invalid api key type %T", legacy[0])
	}
	nodeID, ok := legacy[1].(int)
	if !ok {
		return Options{}, fmt.Errorf("v2board: invalid node id type %T", legacy[1])
	}
	nodeType, ok := legacy[2].(string)
	if !ok {
		return Options{}, fmt.Errorf("v2board: invalid node type type %T", legacy[2])
	}
	timeout, ok := legacy[3].(time.Duration)
	if !ok {
		return Options{}, fmt.Errorf("v2board: invalid timeout type %T", legacy[3])
	}
	return Options{
		APIHost: apiHost,
		NodeConfig: NodeConfig{
			NodeID:   nodeID,
			NodeType: nodeType,
			Token:    apiKey,
		},
		Timeout: timeout,
	}, nil
}

func normalizeOptions(options Options) (*url.URL, NodeConfig, error) {
	apiHost := strings.TrimSpace(options.APIHost)
	if apiHost == "" {
		return nil, NodeConfig{}, errors.New("v2board: api host is empty")
	}
	baseURL, err := url.Parse(apiHost)
	if err != nil {
		return nil, NodeConfig{}, fmt.Errorf("v2board: parse api host: %w", err)
	}
	if baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, NodeConfig{}, errors.New("v2board: api host must include scheme and host")
	}
	baseURL.RawQuery = ""
	baseURL.Fragment = ""

	nodeConfig := options.NodeConfig
	nodeConfig.NodeType = normalizeNodeTypeForAPIStyle(nodeConfig.NodeType, options.APIStyle)
	if nodeConfig.NodeType == "" && options.APIVersion == 2 {
		nodeConfig.NodeType = "v2node"
	}
	nodeConfig.Token = strings.TrimSpace(nodeConfig.Token)
	if nodeConfig.NodeID <= 0 {
		return nil, NodeConfig{}, errors.New("v2board: node id must be positive")
	}
	if nodeConfig.NodeType == "" {
		return nil, NodeConfig{}, errors.New("v2board: node type is empty")
	}
	if nodeConfig.Token == "" {
		return nil, NodeConfig{}, errors.New("v2board: token is empty")
	}
	return baseURL, nodeConfig, nil
}

func (c *Client) ConfigETag() string {
	c.access.Lock()
	defer c.access.Unlock()
	return c.configETag
}

func (c *Client) UserETag() string {
	c.access.Lock()
	defer c.access.Unlock()
	return c.userETag
}

func (c *Client) SetConfigETag(etag string) {
	c.access.Lock()
	defer c.access.Unlock()
	c.configETag = normalizeETag(etag)
}

func (c *Client) SetUserETag(etag string) {
	c.access.Lock()
	defer c.access.Unlock()
	c.userETag = normalizeETag(etag)
}

func (c *Client) GetServerConfig(ctx context.Context) (*ServerConfig, error) {
	if c.apiStyle == APIStyleShadowsocksTidalab {
		return c.getShadowsocksTidalabServerConfig(ctx)
	}
	request, err := c.newRequest(ctx, http.MethodGet, "config", nil)
	if err != nil {
		return nil, err
	}
	if etag := c.ConfigETag(); etag != "" {
		request.Header.Set("If-None-Match", etag)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusNotModified:
		return nil, ErrNotModified
	default:
		return nil, c.responseError(response)
	}

	body, err := readLimitedResponseBody(response, DefaultConfigBodyLimit)
	if err != nil {
		return nil, fmt.Errorf("v2board: read server config: %w", err)
	}
	config, err := c.decodeServerConfig(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("v2board: decode server config: %w", err)
	}
	if etag := response.Header.Get("ETag"); etag != "" {
		c.SetConfigETag(etag)
	}
	return config, nil
}

func (c *Client) GetNodeInfo(ctx context.Context) (*NodeInfo, error) {
	config, err := c.GetServerConfig(ctx)
	if err != nil {
		return nil, err
	}
	nodeType := serverConfigNodeType(config, c.nodeConfig.NodeType)
	node := &NodeInfo{
		ID:       c.nodeConfig.NodeID,
		Type:     nodeType,
		Security: config.TLS,
		Tag:      fmt.Sprintf("[%s]-%s:%d", c.baseURL.String(), nodeType, c.nodeConfig.NodeID),
		Common:   config,
	}
	if config.BaseConfig != nil {
		node.PushInterval = config.BaseConfig.PushInterval.Duration()
		node.PullInterval = config.BaseConfig.PullInterval.Duration()
		node.NodeReportMinTraffic = int64(config.BaseConfig.NodeReportMinTraffic)
		node.DeviceOnlineMinTraffic = int64(config.BaseConfig.DeviceOnlineMinTraffic)
	}
	if node.Type == "shadowsocks" {
		node.Security = SecurityNone
	}
	return node, nil
}

func (c *Client) GetUserList(ctx context.Context) ([]UserInfo, error) {
	request, err := c.newRequest(ctx, http.MethodGet, "user", nil)
	if err != nil {
		return nil, err
	}
	if etag := c.UserETag(); etag != "" {
		request.Header.Set("If-None-Match", etag)
	}
	request.Header.Set("X-Response-Format", "msgpack")
	request.Header.Set("Accept", "application/x-msgpack, application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusNotModified:
		return nil, ErrNotModified
	default:
		return nil, c.responseError(response)
	}

	body, err := c.readUserListBody(response, "user list")
	if err != nil {
		return nil, err
	}
	userList, err := decodeUserList(body, response.Header.Get("Content-Type"))
	if err != nil {
		return nil, err
	}
	if etag := response.Header.Get("ETag"); etag != "" {
		c.SetUserETag(etag)
	}
	return userList.Users, nil
}

func (c *Client) GetAliveList(ctx context.Context) (map[int]int, error) {
	if !c.supportsAliveReport() {
		return nil, nil
	}
	request, err := c.newRequest(ctx, http.MethodGet, "alivelist", nil)
	if err != nil {
		return nil, err
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, c.responseError(response)
	}
	body, err := readLimitedResponseBody(response, DefaultAliveBodyLimit)
	if err != nil {
		return nil, fmt.Errorf("v2board: read alive list: %w", err)
	}
	var alive AliveMap
	if err = json.NewDecoder(bytes.NewReader(body)).Decode(&alive); err != nil {
		return nil, fmt.Errorf("v2board: decode alive list: %w", err)
	}
	if alive.Alive == nil {
		alive.Alive = make(map[int]int)
	}
	return alive.Alive, nil
}

func (c *Client) GetUserAlive(ctx context.Context) (map[int]int, error) {
	return c.GetAliveList(ctx)
}

func (c *Client) PushUserTraffic(ctx context.Context, userTraffic []UserTraffic) error {
	if c.apiStyle != APIStyleUniProxy {
		payload := make([]LegacyUserTraffic, 0, len(userTraffic))
		for _, traffic := range userTraffic {
			payload = append(payload, LegacyUserTraffic{
				UserID:   traffic.UID,
				Upload:   traffic.Upload,
				Download: traffic.Download,
			})
		}
		return c.postJSON(ctx, "push", payload)
	}
	payload := make(map[int][]int64, len(userTraffic))
	for _, traffic := range userTraffic {
		payload[traffic.UID] = []int64{traffic.Upload, traffic.Download}
	}
	return c.postJSON(ctx, "push", payload)
}

func (c *Client) ReportUserTraffic(ctx context.Context, userTraffic []UserTraffic) error {
	return c.PushUserTraffic(ctx, userTraffic)
}

func (c *Client) PushAlive(ctx context.Context, users map[int][]string) error {
	if !c.supportsAliveReport() {
		return nil
	}
	return c.postJSON(ctx, "alive", users)
}

func (c *Client) ReportNodeOnlineUsers(ctx context.Context, users *map[int][]string) error {
	if users == nil {
		return c.PushAlive(ctx, nil)
	}
	return c.PushAlive(ctx, *users)
}

func (c *Client) ReportOnlineUsers(ctx context.Context, users []OnlineUser) error {
	return c.PushAlive(ctx, onlineUsersMap(users, c.nodeConfig.NodeID))
}

func OnlineUsersMap(users []OnlineUser) map[int][]string {
	return onlineUsersMap(users, 0)
}

func onlineUsersMap(users []OnlineUser, nodeID int) map[int][]string {
	result := make(map[int][]string)
	for _, user := range users {
		value := user.IP
		if nodeID > 0 {
			value += "_" + strconv.Itoa(nodeID)
		}
		result[user.UID] = append(result[user.UID], value)
	}
	return result
}

func (c *Client) postJSON(ctx context.Context, resource string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("v2board: encode %s request: %w", resource, err)
	}
	request, err := c.newRequest(ctx, http.MethodPost, resource, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return c.responseError(response)
	}
	drainResponseBody(response.Body)
	return nil
}

func (c *Client) newRequest(ctx context.Context, method string, resource string, body io.Reader) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, method, c.endpoint(resource), body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", c.userAgent)
	request.Header.Set("Accept", "application/json")
	return request, nil
}

func (c *Client) endpoint(resource string) string {
	endpoint := *c.baseURL
	basePath := strings.TrimRight(endpoint.Path, "/")
	resource = strings.TrimLeft(resource, "/")
	if c.apiStyle != APIStyleUniProxy {
		endpoint.Path = basePath + "/api/v1/server/" + c.legacyClass() + "/" + c.legacyAction(resource)
	} else if c.apiVersion == 2 && resource == "config" {
		endpoint.Path = basePath + "/api/v2/server/config"
	} else {
		endpoint.Path = basePath + "/api/v1/server/UniProxy/" + resource
	}
	query := endpoint.Query()
	if c.apiStyle == APIStyleUniProxy && (c.apiVersion != 2 || resource != "config") {
		query.Set("node_type", panelNodeType(c.nodeConfig.NodeType))
	}
	query.Set("node_id", strconv.Itoa(c.nodeConfig.NodeID))
	query.Set("token", c.nodeConfig.Token)
	if c.apiStyle != APIStyleUniProxy && resource == "config" && c.apiStyle != APIStyleShadowsocksTidalab {
		query.Set("local_port", "23333")
	}
	endpoint.RawQuery = query.Encode()
	return endpoint.String()
}

func normalizeAPIStyle(apiStyle string) string {
	switch strings.ToLower(strings.TrimSpace(apiStyle)) {
	case "", APIStyleUniProxy:
		return APIStyleUniProxy
	case APIStyleDeepbwork, "v2ray", "aurora":
		return APIStyleDeepbwork
	case APIStyleTrojanTidalab, "trojantidalab", "trojan-tidalab":
		return APIStyleTrojanTidalab
	case APIStyleShadowsocksTidalab, APIStyleShadowsocksTidalab2, "shadowsocks-tidalab", "ss_tidalab", "sstidalab":
		return APIStyleShadowsocksTidalab
	default:
		return strings.ToLower(strings.TrimSpace(apiStyle))
	}
}

func normalizeAPIStyleForNodeType(apiStyle string, nodeType string) string {
	apiStyle = normalizeAPIStyle(apiStyle)
	if apiStyle != APIStyleUniProxy {
		return apiStyle
	}
	switch strings.ToLower(strings.TrimSpace(nodeType)) {
	case APIStyleDeepbwork, "aurora":
		return APIStyleDeepbwork
	case APIStyleTrojanTidalab, "trojantidalab", "trojan-tidalab":
		return APIStyleTrojanTidalab
	case APIStyleShadowsocksTidalab, APIStyleShadowsocksTidalab2, "shadowsocks-tidalab", "ss_tidalab", "sstidalab":
		return APIStyleShadowsocksTidalab
	default:
		return apiStyle
	}
}

func validAPIStyle(apiStyle string) bool {
	switch apiStyle {
	case APIStyleUniProxy, APIStyleDeepbwork, APIStyleTrojanTidalab, APIStyleShadowsocksTidalab:
		return true
	default:
		return false
	}
}

func (c *Client) legacyClass() string {
	switch c.apiStyle {
	case APIStyleDeepbwork:
		return "Deepbwork"
	case APIStyleTrojanTidalab:
		return "TrojanTidalab"
	case APIStyleShadowsocksTidalab:
		return "ShadowsocksTidalab"
	default:
		return "UniProxy"
	}
}

func (c *Client) legacyAction(resource string) string {
	if resource == "push" {
		return "submit"
	}
	return resource
}

func (c *Client) supportsAliveReport() bool {
	return c.apiStyle == APIStyleUniProxy
}

func (c *Client) decodeServerConfig(reader io.Reader) (*ServerConfig, error) {
	switch c.apiStyle {
	case APIStyleDeepbwork:
		var config legacyV2RayConfig
		if err := json.NewDecoder(reader).Decode(&config); err != nil {
			return nil, err
		}
		return config.serverConfig(), nil
	case APIStyleTrojanTidalab:
		var config legacyTrojanConfig
		if err := json.NewDecoder(reader).Decode(&config); err != nil {
			return nil, err
		}
		return config.serverConfig(), nil
	default:
		var config ServerConfig
		if err := json.NewDecoder(reader).Decode(&config); err != nil {
			return nil, err
		}
		return &config, nil
	}
}

func (c *Client) getShadowsocksTidalabServerConfig(ctx context.Context) (*ServerConfig, error) {
	request, err := c.newRequest(ctx, http.MethodGet, "user", nil)
	if err != nil {
		return nil, err
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, c.responseError(response)
	}
	body, err := c.readUserListBody(response, "shadowsocks tidalab user list")
	if err != nil {
		return nil, err
	}
	userList, err := decodeUserList(body, response.Header.Get("Content-Type"))
	if err != nil {
		return nil, err
	}
	if len(userList.Users) == 0 {
		return nil, ErrEmptyUserList
	}
	first := userList.Users[0]
	return &ServerConfig{
		Protocol:   "shadowsocks",
		ServerPort: first.Port,
		Cipher:     first.Cipher,
	}, nil
}

func normalizeETag(etag string) string {
	etag = strings.TrimSpace(etag)
	etag = strings.TrimPrefix(etag, "W/")
	return strings.Trim(etag, `"`)
}

func decodeUserList(body []byte, contentType string) (*UserListBody, error) {
	var msgpackError error
	if shouldTryMsgpack(body, contentType) {
		var decoded any
		decoded, msgpackError = decodeMsgpack(body)
		if msgpackError == nil {
			userList, err := userListFromAny(decoded)
			if err == nil {
				return userList, nil
			}
			msgpackError = err
		}
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		if msgpackError != nil {
			return nil, fmt.Errorf("v2board: decode user list msgpack: %v; decode json: %w", msgpackError, err)
		}
		return nil, fmt.Errorf("v2board: decode user list json: %w", err)
	}
	userList, err := userListFromAny(decoded)
	if err != nil {
		if msgpackError != nil {
			return nil, fmt.Errorf("v2board: decode user list msgpack: %v; decode json shape: %w", msgpackError, err)
		}
		return nil, err
	}
	return userList, nil
}

func shouldTryMsgpack(body []byte, contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err == nil {
		switch mediaType {
		case "application/x-msgpack", "application/msgpack", "application/vnd.msgpack", "binary/message-pack":
			return true
		}
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return false
	}
	return body[0] != '{' && body[0] != '['
}

func (c *Client) responseError(response *http.Response) error {
	body, readErr := io.ReadAll(io.LimitReader(response.Body, c.errorBodyLimit))
	if readErr != nil {
		return fmt.Errorf("v2board: unexpected status %s; read error body: %w", response.Status, readErr)
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return fmt.Errorf("v2board: unexpected status %s", response.Status)
	}
	return fmt.Errorf("v2board: unexpected status %s: %s", response.Status, string(body))
}

func (c *Client) readUserListBody(response *http.Response, name string) ([]byte, error) {
	body, err := readLimitedResponseBody(response, c.userBodyLimit)
	if err != nil {
		return nil, fmt.Errorf("v2board: read %s: %w", name, err)
	}
	return body, nil
}

func readLimitedResponseBody(response *http.Response, limit int64) ([]byte, error) {
	if limit <= 0 {
		return io.ReadAll(response.Body)
	}
	if response.ContentLength > limit {
		return nil, fmt.Errorf("response body too large: %d > %d bytes", response.ContentLength, limit)
	}
	readLimit := limit + 1
	if limit == math.MaxInt64 {
		readLimit = limit
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, readLimit))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("response body too large: exceeds %d bytes", limit)
	}
	return body, nil
}

func drainResponseBody(reader io.Reader) {
	_, _ = io.Copy(io.Discard, io.LimitReader(reader, DefaultDrainBodyLimit))
}
