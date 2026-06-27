---
icon: material/new-box
---

# V2Board

The V2Board service turns panel nodes into managed singlink inbounds. It periodically pulls node and user data from a
V2Board-compatible API and reports traffic usage back to the panel.

### Structure

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

### Fields

#### api_host

Panel API base URL. It must include the scheme and host, for example `https://panel.example.com`.

#### api_key

Panel token used when requesting node configuration and user lists.

#### api_send_ip

Optional local source IP used by the HTTP client when connecting to the panel.

#### api_version

Panel API version. Supported values:

* `1`
* `2`

Defaults to `1`.

#### api_style

Panel API style. Supported values:

* `uniproxy`
* `deepbwork`
* `trojan_tidalab`
* `shadowsocks_tidalab`

Defaults to `uniproxy`.

#### timeout

HTTP timeout for panel requests. Defaults to `30s`.

#### error_body_limit

Maximum response body size retained for panel error responses. Use `0` for the default.

#### user_list_body_limit

Maximum response body size accepted for user-list responses. Use `0` for the default.

#### listen

Local listen address for managed inbounds. If omitted, the panel node `listen_ip` is used when available.

There is no `listen_port` field in the service configuration. The listen port comes from the panel node `server_port`.

#### tcp_fast_open

Enable TCP Fast Open on managed inbounds.

#### pull_interval

Interval for pulling node configuration and user lists. Values below `5s` are raised to `5s`. Defaults to `1m`.

#### push_interval

Interval for reporting traffic usage. Values below `5s` are raised to `5s`. Defaults to `1m`.

#### node_report_min_traffic

Minimum per-node traffic delta before reporting.

#### device_online_min_traffic

Minimum per-device traffic delta used to mark a device online.

#### tls

TLS override for managed inbounds.

Object format:

```json
{
  "mode": "none",
  "cert_file": "",
  "key_file": "",
  "server_name": "",
  "certificate_provider": ""
}
```

Set `mode` to `none` to force TLS off. Set both `cert_file` and `key_file`, or set `certificate_provider`, to provide
inbound certificates when the panel node requires TLS.

Panel-managed certificate modes such as `http` and `dns` are not automatically issued by this service.

#### multiplex

Inbound multiplex settings. See [Multiplex](/configuration/shared/multiplex/) for supported fields.

#### nodes

List of panel nodes managed by this service.

Object format:

```json
{
  "tag": "",
  "node_id": 1,
  "node_type": "shadowsocks"
}
```

Node objects may override the service-level fields above.

Supported `node_type` values:

* `vmess`
* `vless`
* `shadowsocks`
* `trojan`
* `tuic`
* `anytls`
* `hysteria`
* `hysteria2`

### Example

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
