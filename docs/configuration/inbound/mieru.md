---
icon: material/new-box
---

### Structure

```json
{
  "type": "mieru",
  "tag": "mieru-in",

  "listen": "::",
  "listen_port": 8964,

  ... // Other Listen Fields

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

    singlink exposes the Mieru server fields documented below. Other Mieru server profile fields are not configurable through this inbound.

### Listen Fields

See [Listen Fields](/configuration/shared/listen/) for details. `listen_port` is only used when `port_bindings` is omitted.

When `port_bindings` is set, Mieru listens on every configured binding. The shared listen address and socket options still come from Listen Fields.

### Fields

#### port_bindings

A list of Mieru port bindings.

If omitted, singlink uses the compatibility path of `listen_port` plus `transport`.

#### port_bindings.port

A single listening port.

Exactly one of `port` and `port_range` must be set.

#### port_bindings.port_range

A listening port range in `begin-end` form, for example `9000-9010`.

Exactly one of `port` and `port_range` must be set.

#### port_bindings.protocol

==Required==

Transport protocol for this port binding.

Supported values are `TCP` and `UDP`. Values are parsed case-insensitively, but the canonical form is uppercase.

#### transport

Required when `port_bindings` is omitted.

Transport protocol.

Supported values are `TCP` and `UDP`. Values are parsed case-insensitively, but the canonical form is uppercase.

#### users

==Required==

A list of Mieru users.

#### users.name

==Required==

Mieru user name.

Must be non-empty and no more than 64 bytes.

#### users.password

==Required==

Mieru password.

Must be non-empty and no more than 64 bytes.

#### traffic_pattern

Base64 encoding of a Mieru `TrafficPattern` protobuf message.

#### mtu

Mieru underlay MTU. When set, the value must be between `1280` and `1400`.

#### user_hint_is_mandatory

Deprecated compatibility field. The Mieru inbound always requires user hints in production builds, even if this field is omitted or set to `false`.
