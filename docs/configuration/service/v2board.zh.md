---
icon: material/new-box
---

# V2Board

V2Board 服务会把面板节点转换为由 singlink 管理的入站。它会定期从兼容 V2Board 的 API 拉取节点和用户数据，并向面板回报流量。

### 结构

```json
{
  "type": "v2board",
  "tag": "",
  "api_host": "",
  "api_key": "",
  "api_send_ip": "",
  "api_version": 1,
  "api_style": "uniproxy",
  "timeout": "30s",
  "error_body_limit": 0,
  "user_list_body_limit": 0,
  "listen": "0.0.0.0",
  "tcp_fast_open": false,
  "pull_interval": "1m",
  "push_interval": "1m",
  "node_report_min_traffic": 0,
  "device_online_min_traffic": 0,
  "tls": {},
  "multiplex": {},
  "nodes": []
}
```

### 字段

#### api_host

面板 API 基础地址，必须包含 scheme 和 host，例如 `https://panel.example.com`。

#### api_key

请求节点配置和用户列表时使用的面板 token。

#### api_send_ip

连接面板时 HTTP 客户端使用的可选本地源 IP。

#### api_version

面板 API 版本。支持值：

* `1`
* `2`

默认值为 `1`。

#### api_style

面板 API 风格。支持值：

* `uniproxy`
* `deepbwork`
* `trojan_tidalab`
* `shadowsocks_tidalab`

默认值为 `uniproxy`。

#### timeout

面板请求的 HTTP 超时时间。默认值为 `30s`。

#### error_body_limit

面板错误响应保留的最大响应体大小。使用 `0` 表示默认值。

#### user_list_body_limit

用户列表响应允许的最大响应体大小。使用 `0` 表示默认值。

#### listen

受管理入站的本地监听地址。省略时会优先使用面板节点的 `listen_ip`。

服务配置没有 `listen_port` 字段，监听端口来自面板节点的 `server_port`。

#### tcp_fast_open

为受管理入站启用 TCP Fast Open。

#### pull_interval

拉取节点配置和用户列表的间隔。小于 `5s` 的值会被提升到 `5s`。默认值为 `1m`。

#### push_interval

回报流量的间隔。小于 `5s` 的值会被提升到 `5s`。默认值为 `1m`。

#### node_report_min_traffic

触发节点流量回报的最小流量变化量。

#### device_online_min_traffic

判定设备在线的最小设备流量变化量。

#### tls

受管理入站的 TLS 覆盖配置。

对象格式：

```json
{
  "mode": "none",
  "cert_file": "",
  "key_file": "",
  "server_name": "",
  "certificate_provider": ""
}
```

将 `mode` 设为 `none` 可强制关闭 TLS。面板节点需要 TLS 时，可以同时设置 `cert_file` 和 `key_file`，或设置
`certificate_provider` 来提供入站证书。

该服务不会自动执行面板侧 `http`、`dns` 等证书签发模式。

#### multiplex

入站多路复用设置。支持字段参阅 [Multiplex](/zh/configuration/shared/multiplex/)。

#### nodes

由该服务管理的面板节点列表。

对象格式：

```json
{
  "tag": "",
  "node_id": 1,
  "node_type": "shadowsocks"
}
```

节点对象可以覆盖上面的服务级字段。

支持的 `node_type`：

* `vmess`
* `vless`
* `shadowsocks`
* `trojan`
* `tuic`
* `anytls`
* `hysteria`
* `hysteria2`

### 示例

```json
{
  "services": [
    {
      "type": "v2board",
      "tag": "v2board",
      "api_host": "https://panel.example.com",
      "api_key": "REPLACE_WITH_V2BOARD_NODE_TOKEN",
      "api_version": 1,
      "api_style": "uniproxy",
      "timeout": "30s",
      "listen": "0.0.0.0",
      "pull_interval": "1m",
      "push_interval": "1m",
      "tls": {
        "mode": "none"
      },
      "nodes": [
        {
          "tag": "v2board-shadowsocks-1",
          "node_id": 1,
          "node_type": "shadowsocks"
        }
      ]
    }
  ]
}
```
