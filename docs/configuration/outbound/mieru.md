---
icon: material/new-box
---

### Structure

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

  ... // Dial Fields
}
```

!!! note

    singlink exposes the Mieru client fields documented below. Other Mieru client profile fields are not configurable through this outbound.

### Fields

#### server

==Required==

The server address.

#### server_port

The server port.

At least one of `server_port` and `server_ports` must be set.

#### server_ports

Server port range list.

Each item must be a string in `start-end` format, for example `9000-9010`.

Both ports are inclusive and must be between 1 and 65535. `start` must be less than or equal to `end`.

At least one of `server_port` and `server_ports` must be set.

#### transport

==Required==

Transport protocol.

Supported values are `TCP` and `UDP`. Values are parsed case-insensitively, but the canonical form is uppercase.

#### username

==Required==

Mieru user name.

Must be non-empty and no more than 64 bytes.

#### password

==Required==

Mieru password.

Must be non-empty and no more than 64 bytes.

#### multiplexing

Mieru multiplexing level.

Supported values are `MULTIPLEXING_DEFAULT`, `MULTIPLEXING_OFF`, `MULTIPLEXING_LOW`, `MULTIPLEXING_MIDDLE`, and `MULTIPLEXING_HIGH`.

`MULTIPLEXING_OFF` disables multiplexing.

#### traffic_pattern

Base64 encoding of a Mieru `TrafficPattern` protobuf message.

### Dial Fields

See [Dial Fields](/configuration/shared/dial/) for details.
