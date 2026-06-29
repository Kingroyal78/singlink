---
icon: material/new-box
---

### 结构

```json
{
  "type": "mieru",
  "tag": "mieru-out",

  "server": "127.0.0.1",
  "server_port": 8964,
  "server_ports": [
    "9000-9010",
    "9020-9030"
  ],
  "transport": "TCP",
  "username": "user",
  "password": "password",
  "multiplexing": "MULTIPLEXING_LOW",
  "traffic_pattern": "GgQIARAK",

  ... // 拨号字段
}
```

!!! note

    singlink 仅暴露下方列出的 Mieru 客户端字段，其他 Mieru 客户端配置项不能通过此出站配置。

### 字段

#### server

==必填==

服务器地址。

#### server_port

服务器端口。

必须填写 `server_port` 和 `server_ports` 中至少一项。

#### server_ports

服务器端口范围列表。

每一项必须是 `start-end` 格式的字符串，例如 `9000-9010`。

两端端口均包含在范围内，且必须在 1 到 65535 之间。`start` 必须小于或等于 `end`。

必须填写 `server_port` 和 `server_ports` 中至少一项。

#### transport

==必填==

传输协议。

支持值为 `TCP` 和 `UDP`。解析时不区分大小写，但规范写法为大写。

#### username

==必填==

Mieru 用户名。

不能为空，且不超过 64 bytes。

#### password

==必填==

Mieru 密码。

不能为空，且不超过 64 bytes。

#### multiplexing

Mieru 多路复用等级。

支持值为 `MULTIPLEXING_DEFAULT`、`MULTIPLEXING_OFF`、`MULTIPLEXING_LOW`、`MULTIPLEXING_MIDDLE` 和 `MULTIPLEXING_HIGH`。

`MULTIPLEXING_OFF` 会关闭多路复用。

#### traffic_pattern

Mieru `TrafficPattern` protobuf 消息的 base64 编码。

### 拨号字段

参阅 [拨号字段](/zh/configuration/shared/dial/)。
