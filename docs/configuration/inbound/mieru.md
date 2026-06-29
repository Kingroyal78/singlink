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

See [Listen Fields](/configuration/shared/listen/) for details.

The Mieru inbound requires a single `listen_port`. singlink does not expose Mieru inbound port ranges or multiple port bindings.

### Fields

#### transport

==Required==

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

#### user_hint_is_mandatory

Deprecated compatibility field. The Mieru inbound always requires user hints in production builds, even if this field is omitted or set to `false`.
