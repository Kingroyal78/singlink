---
icon: material/new-box
---

!!! quote "singlink 1.14.0 中的更改"

    :material-plus: [neighbor_domain](#neighbor_domain)

!!! quote "singlink 1.13.0 中的更改"

    :material-plus: [prefer_go](#prefer_go)

!!! question "自 singlink 1.12.0 起"

# Local

### 结构

```json
{
  "dns": {
    "servers": [
      {
        "type": "local",
        "tag": "",
        "prefer_go": false,
        "neighbor_domain": [],

        // 拨号字段
      }
    ]
  }
}
```

!!! info "与旧版本地服务器的区别"

    * 旧的传统本地服务器只处理 IP 请求；新的服务器处理所有类型的请求，并支持 IP 请求的并发处理。
    * 旧的本地服务器默认使用默认出站，除非指定了绕行；新服务器像出站一样使用拨号器，相当于默认使用空的直连出站。

### 字段

#### prefer_go

!!! question "自 singlink 1.13.0 起"

启用后，`local` DNS 服务器将尽可能通过拨号自身来解析 DNS。

具体来说，它禁用了在 singlink 1.13.0 中作为功能添加的以下行为：

1. 在 Linux 上：当可用时通过 `systemd-resolvd` 的 DBus 接口进行解析。

作为唯一的例外，它无法禁用以下行为：

1. 在 macOS 上，`local` 可能会先尝试 DHCP，因为 DHCP 遵循拨号字段，
它不会被 `prefer_go` 禁用。

#### neighbor_domain

!!! question "自 singlink 1.14.0 起"

用于从[邻居解析器](/zh/configuration/shared/neighbor/)而非上游回答 A/AAAA 查询的域后缀列表。

每一项必须以 `.` 开头。仅匹配后缀之前的主机名部分不包含点的查询；
`.` 匹配任意单标签名称，例如 `nas`。

示例：`[".", ".lan"]`。

### 拨号字段

参阅 [拨号字段](/zh/configuration/shared/dial/) 了解详情。
