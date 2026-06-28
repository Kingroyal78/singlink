# singlink

The universal proxy platform.

[![Packaging status](https://repology.org/badge/vertical-allrepos/singlink.svg)](https://repology.org/project/singlink/versions)

singlink is a Go-based proxy runtime for running proxy inbounds, outbounds, DNS, routing, rule sets, and service integrations from a single JSON configuration. This repository also contains panel/service integrations and internal load-test tooling used to validate high-concurrency Shadowsocks deployments.

## Highlights

- Multi-protocol proxy runtime with JSON configuration and hot reload support.
- Inbound and outbound support for Shadowsocks, VMess, VLESS, Trojan, TUIC, Hysteria/Hysteria2, AnyTLS, SOCKS, HTTP, WireGuard, Tor, TUN, and related transports.
- DNS, route rule, GeoIP, Geosite, and rule-set tooling.
- Managed service integrations:
  - V2Board-compatible panel node and user synchronization.
  - Shadowsocks Server Management API.
  - CCM, a Claude Code multiplexer.
  - OCM, an OpenAI Codex multiplexer.
- Internal high-concurrency SS lifecycle load test for capacity and leak checks.

## Documentation

Main documentation:

```text
https://singlink.sagernet.org
```

Important local docs:

- [Configuration index](docs/configuration/index.md)
- [V2Board service](docs/configuration/service/v2board.md)
- [SSM API service](docs/configuration/service/ssm-api.md)
- [CCM service](docs/configuration/service/ccm.md)
- [OCM service](docs/configuration/service/ocm.md)
- [Shadowsocks inbound](docs/configuration/inbound/shadowsocks.md)
- [Shadowsocks outbound](docs/configuration/outbound/shadowsocks.md)

## Build

Requirements:

- Go toolchain compatible with `go.mod` (currently `go 1.25.0`, toolchain `go1.26.4`).
- CGO/toolchain dependencies required by the selected build tags and target platform.

Build the main binary:

```bash
make build
```

Install into `$(go env GOPATH)/bin`:

```bash
make install
```

Run the test suite:

```bash
go test ./...
```

Build directly without Make:

```bash
go build -tags "$(cat release/DEFAULT_BUILD_TAGS_OTHERS)" ./cmd/singlink
```

## Basic Usage

Check a configuration:

```bash
singlink check -c config.json
```

Run a configuration:

```bash
singlink run -c config.json
```

Run with multiple config files or a config directory:

```bash
singlink run -c base.json -c node.json
singlink run -C ./config.d
singlink run -D /var/lib/singlink -c config.json
```

Useful maintenance commands:

```bash
singlink format -c config.json
singlink merge -c base.json -c node.json merged.json
singlink version
```

## Services

singlink can run long-lived services in addition to normal proxy routing.

### V2Board

The V2Board service pulls node and user state from a V2Board-compatible panel, creates managed inbounds, and reports traffic usage back to the panel.

See [docs/configuration/service/v2board.md](docs/configuration/service/v2board.md).

### SSM API

The SSM API service exposes a REST API for managing Shadowsocks users on managed Shadowsocks inbounds.

See [docs/configuration/service/ssm-api.md](docs/configuration/service/ssm-api.md).

### CCM and OCM

CCM and OCM expose local Claude Code or OpenAI Codex OAuth credentials through authenticated proxy services. Treat these as sensitive services: use explicit users, strong bearer tokens, TLS, and a trusted network boundary.

See:

- [docs/configuration/service/ccm.md](docs/configuration/service/ccm.md)
- [docs/configuration/service/ocm.md](docs/configuration/service/ocm.md)

## Shadowsocks High-Concurrency Testing

This repository includes an internal lifecycle load test:

```bash
go run ./cmd/internal/ss_loadtest
```

The test starts a local singlink Shadowsocks inbound and local loopback HTTP/idle targets. It is designed to keep the traffic on `127.0.0.1`, so it can validate CPU, memory, FD usage, goroutine cleanup, connection lifecycle, and Shadowsocks cipher behavior without sending benchmark traffic to the public Internet.

Example 100k configured users / 10k online users:

```bash
go run ./cmd/internal/ss_loadtest \
  -total-users=100000 \
  -idle=7000 \
  -light=2000 \
  -medium=800 \
  -heavy=200 \
  -lifecycle \
  -soak-day=15m \
  -ramp=20s \
  -drain=60s \
  -metrics-interval=5s
```

The lifecycle mode writes JSONL metrics containing RSS, heap, goroutines, FD count, active connections, request counters, and TCP state samples. It fails the run when measured errors occur or when post-drain connection, FD, goroutine, or heap behavior suggests a leak.

### Reference Results

These numbers are loopback benchmark results on an 8 vCPU / 15 GiB RAM Linux test host. They are capacity references, not a public-network SLA.

| Scenario | Result | Requests | Errors | Peak RSS | Peak heap | Peak FD | Peak goroutines | Notes |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 100k configured / 10k online, 15m lifecycle | PASS | 316,456 | 0 | 3.57 GiB | 2.80 GiB | 28,895 | 39,151 | Stable post-drain cleanup. |
| 100k configured / 30k online, 2m lifecycle | PASS with host tuning | 399,828 | 0 | 11.29 GiB | 8.64 GiB | 95,895 | 127,978 | Requires slow ramp and wider local ephemeral port range for single-host loopback testing. |

Observed post-drain cleanup for the passing runs returned to:

```text
active=0 idle_active=0 fd=14 goroutines=11
```

Capacity guidance for the tested 8 vCPU / 15 GiB profile:

- 10k online is validated with zero measured errors in the lifecycle test.
- 15k-20k online is the conservative production planning range for the tested traffic mix.
- 30k online is possible in the loopback test only with tuning and slow ramp, but CPU and memory are near the practical limit.
- Real-world capacity must also account for public bandwidth, kernel limits, TLS settings, route complexity, DNS behavior, logging, and observability overhead.

For high-concurrency SS deployments, start with:

```bash
ulimit -n 262144
```

For single-host loopback stress tests above 10k online, Linux ephemeral port range and TIME_WAIT behavior may become the bottleneck before the proxy runtime itself. Tune host kernel settings only when you understand the tradeoffs.

## Development Notes

Common checks:

```bash
go test ./...
git diff --check
```

Format documentation tables and generated config docs:

```bash
make fmt_docs
```

Serve the documentation locally:

```bash
make docs_install
make docs
```

Build tags are read from:

```text
release/DEFAULT_BUILD_TAGS_OTHERS
```

## Release Artifacts

Official installable artifacts are produced by the `Release Artifacts` workflow.

Recommended release flow:

```bash
git tag vX.Y.Z
git push origin vX.Y.Z
```

The tag workflow builds and publishes:

- GitHub Release binary archives named `singlink_<version>_<os>_<arch>.tar.gz`.
- Per-asset `.sha256` files.
- `checksums.txt`.
- `release-manifest.json` for one-click installers.
- GHCR Docker image tags:
  - `ghcr.io/<owner>/singlink:<version>`
  - `ghcr.io/<owner>/singlink:v<version>`
  - `ghcr.io/<owner>/singlink:latest` for non-prerelease versions.

Manual runs are supported through GitHub Actions `workflow_dispatch`, but production installers should consume tagged
GitHub Releases instead of transient workflow artifacts.

Binary installer example:

```bash
curl -fsSL https://github.com/Kingroyal78/singlink/releases/latest/download/install.sh | sh
```

## License

```text
Copyright (C) 2022 by nekohasekai <contact-sagernet@sekai.icu>

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program. If not, see <http://www.gnu.org/licenses/>.

In addition, no derivative work may use the name or imply association
with this application without prior consent.
```
