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

参阅 [监听字段](/zh/configuration/shared/listen/)。

Mieru 入站要求使用单个 `listen_port`。singlink 不暴露 Mieru 入站端口范围或多个端口绑定。

### 字段

#### transport

==必填==

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

#### user_hint_is_mandatory

已废弃的兼容字段。生产构建中 Mieru 入站始终要求客户端发送用户提示，即使省略该字段或设置为 `false` 也是如此。
