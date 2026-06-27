---
icon: material/new-box
---

!!! quote "Changes in singlink 1.14.0"

    :material-plus: [neighbor_domain](#neighbor_domain)

!!! quote "Changes in singlink 1.13.0"

    :material-plus: [prefer_go](#prefer_go)

!!! question "Since singlink 1.12.0"

# Local

### Structure

```json
{
  "dns": {
    "servers": [
      {
        "type": "local",
        "tag": "",
        "prefer_go": false,
        "neighbor_domain": []

        // Dial Fields
      }
    ]
  }
}
```

!!! info "Difference from legacy local server"

    * The old legacy local server only handles IP requests; the new one handles all types of requests and supports concurrent for IP requests.
    * The old local server uses default outbound by default unless detour is specified; the new one uses dialer just like outbound, which is equivalent to using an empty direct outbound by default.

### Fields

#### prefer_go

!!! question "Since singlink 1.13.0"

When enabled, `local` DNS server will resolve DNS by dialing itself whenever possible.

Specifically, it disables following behaviors which was added as features in singlink 1.13.0:

1. On Linux: Resolve through `systemd-resolvd`'s DBus interface when available.

As a sole exception, it cannot disable the following behavior:

1. On macOS, `local` may try DHCP first when available, since DHCP respects Dial Fields,
it is not disabled by `prefer_go`.

#### neighbor_domain

!!! question "Since singlink 1.14.0"

A list of domain suffixes for which A/AAAA queries are answered from the
[neighbor resolver](/configuration/shared/neighbor/) instead of the upstream.

Each entry must start with `.`. Only queries whose host part (the portion
before the suffix) contains no dots are matched; `.` matches any
single-label name such as `nas`.

Example: `[".", ".lan"]`.

### Dial Fields

See [Dial Fields](/configuration/shared/dial/) for details.
