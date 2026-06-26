---
icon: material/new-box
---

!!! question "Since singlink 1.12.0"

# Service

### Structure

```json
{
  "services": [
    {
      "type": "",
      "tag": ""
    }
  ]
}
```

### Fields

| Type              | Format                                |
|-------------------|---------------------------------------|
| `api`             | [singlink API](./api)                 |
| `ccm`             | [CCM](./ccm)                          |
| `derp`            | [DERP](./derp)                        |
| `hysteria-realm`  | [Hysteria Realm](./hysteria-realm)    |
| `ocm`             | [OCM](./ocm)                          |
| `resolved`        | [Resolved](./resolved)                |
| `ssm-api`         | [SSM API](./ssm-api)                  |
| `usbip-server`    | [USB/IP Server](./usbip-server)       |
| `usbip-client`    | [USB/IP Client](./usbip-client)       |

#### tag

The tag of the endpoint.
