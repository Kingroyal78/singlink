---
icon: material/new-box
---

### 结构

```json
{
  "type": "mieru",
  "tag": "mieru-in",

  "listen": "::",
  "listen_port": 8964,

  ... // 其他监听字段

  "transport": "TCP",
  "port_bindings": [
    {
      "port": 8964,
      "protocol": "TCP"
    },
    {
      "port_range": "9000-9010",
      "protocol": "UDP"
    }
  ],
  "users": [
    {
      "name": "user",
      "password": "password"
    }
  ],
  "traffic_pattern": "GgQIARAK",
  "user_hint_is_mandatory": true
}
```

!!! note

    singlink 仅暴露下方列出的 Mieru 服务端字段，其他 Mieru 服务端配置项不能通过此入站配置。

### 监听字段

参阅 [监听字段](/zh/configuration/shared/listen/)。只有省略 `port_bindings` 时才会使用 `listen_port`。

设置 `port_bindings` 后，Mieru 会监听每一个配置的端口绑定。共享的监听地址和 socket 选项仍来自监听字段。

### 字段

#### port_bindings

Mieru 端口绑定列表。

如果省略该字段，singlink 会使用兼容路径：`listen_port` 加 `transport`。

#### port_bindings.port

单个监听端口。

`port` 和 `port_range` 必须且只能设置其中一个。

#### port_bindings.port_range

监听端口范围，格式为 `begin-end`，例如 `9000-9010`。

`port` 和 `port_range` 必须且只能设置其中一个。

#### port_bindings.protocol

==必填==

该端口绑定的传输协议。

支持值为 `TCP` 和 `UDP`。解析时不区分大小写，但规范写法为大写。

#### transport

省略 `port_bindings` 时必填。

传输协议。

支持值为 `TCP` 和 `UDP`。解析时不区分大小写，但规范写法为大写。

#### users

==必填==

Mieru 用户列表。

#### users.name

==必填==

Mieru 用户名。

不能为空，且不超过 64 bytes。

#### users.password

==必填==

Mieru 密码。

不能为空，且不超过 64 bytes。

#### traffic_pattern

Mieru `TrafficPattern` protobuf 消息的 base64 编码。

#### mtu

Mieru underlay MTU。设置时取值必须在 `1280` 到 `1400` 之间。

#### user_hint_is_mandatory

已废弃的兼容字段。生产构建中 Mieru 入站始终要求客户端发送用户提示，即使省略该字段或设置为 `false` 也是如此。
