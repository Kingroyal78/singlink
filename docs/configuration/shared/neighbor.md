---
icon: material/lan
---

# Neighbor Resolution

Match LAN devices by MAC address and hostname using
[`source_mac_address`](/configuration/route/rule/#source_mac_address) and
[`source_hostname`](/configuration/route/rule/#source_hostname) rule items.

Neighbor resolution is automatically enabled when these rule items exist
or when a [local DNS server](/configuration/dns/server/local/) sets
[neighbor_domain](/configuration/dns/server/local/#neighbor_domain).
Use [`route.find_neighbor`](/configuration/route/#find_neighbor) to force enable it for logging without rules.

## Linux

Works natively. No special setup required.

Hostname resolution requires DHCP lease files,
automatically detected from common DHCP servers (dnsmasq, odhcpd, ISC dhcpd, Kea).
Custom paths can be set via [`route.dhcp_lease_files`](/configuration/route/#dhcp_lease_files).

## macOS

Works natively. No special setup required.

See [VPN Hotspot](/manual/misc/vpn-hotspot/#macos) for Internet Sharing setup.
