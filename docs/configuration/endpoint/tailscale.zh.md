---
icon: material/new-box
---

!!! quote "singlink 1.14.0 中的更改"

    :material-plus: [ssh_server](#ssh_server)

!!! quote "singlink 1.13.0 中的更改"

    :material-plus: [relay_server_port](#relay_server_port)  
    :material-plus: [relay_server_static_endpoints](#relay_server_static_endpoints)  
    :material-plus: [system_interface](#system_interface)  
    :material-plus: [system_interface_name](#system_interface_name)  
    :material-plus: [system_interface_mtu](#system_interface_mtu)  
    :material-plus: [advertise_tags](#advertise_tags)

!!! question "自 singlink 1.12.0 起"

### 结构

```json
{
  "type": "tailscale",
  "tag": "ts-ep",
  "state_directory": "",
  "auth_key": "",
  "control_url": "",
  "ephemeral": false,
  "hostname": "",
  "accept_routes": false,
  "exit_node": "",
  "exit_node_allow_lan_access": false,
  "advertise_routes": [],
  "advertise_exit_node": false,
  "advertise_tags": [],
  "relay_server_port": 0,
  "relay_server_static_endpoints": [],
  "system_interface": false,
  "system_interface_name": "",
  "system_interface_mtu": 0,
  "udp_timeout": "5m",
  "ssh_server": false,

  ... // 拨号字段
}
```

### 字段

#### state_directory

存储 Tailscale 状态的目录。

默认使用 `tailscale`。

示例：`$HOME/.tailscale`

#### auth_key

!!! note

    认证密钥不是必需的。默认情况下，singlink 将记录登录 URL。

用于创建节点的认证密钥。如果节点已经创建（从之前存储的状态），则不使用此字段。

#### control_url

协调服务器 URL。

默认使用 `https://controlplane.tailscale.com`。

#### ephemeral

指示实例是否应注册为临时节点 (https://tailscale.com/s/ephemeral-nodes)。

#### hostname

节点的主机名。

默认使用系统主机名。

示例：`localhost`

#### accept_routes

指示节点是否应接受其他节点通告的路由。

#### exit_node

要使用的出口节点名称或 IP 地址。

#### exit_node_allow_lan_access

!!! note

    当出口节点没有相应的通告路由时，即使设置了 `exit_node_allow_lan_access`，私有流量也无法路由到出口节点。

指示本地可访问的子网应该直接路由还是通过出口节点路由。

#### advertise_routes

通告到 Tailscale 网络的 CIDR 前缀，作为可通过当前节点访问的路由。

示例：`["192.168.1.1/24"]`

#### advertise_exit_node

指示节点是否应将自己通告为出口节点。

#### advertise_tags

!!! question "自 singlink 1.13.0 起"

为此节点通告的标签，用于 ACL 执行。

示例：`["tag:server"]`

#### relay_server_port

!!! question "自 singlink 1.13.0 起"

监听来自其他 Tailscale 节点的中继连接的端口。

#### relay_server_static_endpoints

!!! question "自 singlink 1.13.0 起"

为中继服务器通告的静态端点。

#### system_interface

!!! question "自 singlink 1.13.0 起"

为 Tailscale 创建系统 TUN 接口。

#### system_interface_name

!!! question "自 singlink 1.13.0 起"

自定义 TUN 接口名。默认使用 `tailscale`（macOS 上为 `utun`）。

#### system_interface_mtu

!!! question "自 singlink 1.13.0 起"

覆盖 TUN 的 MTU。默认使用 Tailscale 自己的 MTU。

#### udp_timeout

UDP NAT 过期时间。

默认使用 `5m`。

#### ssh_server

!!! question "自 singlink 1.14.0 起"

在 tailnet 的 TCP 22 端口上运行 Tailscale SSH 服务器。

访问控制由 Tailscale 管理控制台中的 SSH ACL 决定，它将每个连接映射到一个本地用户。该用户如何解析、以及允许哪些用户，取决于平台：

- **Linux** 和 **macOS**：从系统用户数据库解析用户。要切换到 singlink 运行身份以外的用户需要以 root 运行；非 root 时，会话仅限于当前用户。
- **Windows**：会话以 singlink 进程的身份运行；映射的用户不会被模拟，因此映射到其他本地账户的会话将被拒绝。

对象格式：

```json
{
  "enabled": true,
  "disable_pty": false,
  "disable_sftp": false,
  "disable_forwarding": false
}
```

将 `ssh_server` 值设置为 `true` 等同于 `{ "enabled": true }`。

#### ssh_server.enabled

启用 SSH 服务器。

#### ssh_server.disable_pty

拒绝 PTY 分配请求。

#### ssh_server.disable_sftp

拒绝 SFTP 子系统。

#### ssh_server.disable_forwarding

拒绝本地和远程的 TCP 与 Unix 套接字转发，包括 SSH agent 转发。

### 拨号字段

!!! note

    Tailscale 端点中的拨号字段仅控制它如何连接到控制平面，与实际连接无关。

参阅 [拨号字段](/zh/configuration/shared/dial/) 了解详情。
