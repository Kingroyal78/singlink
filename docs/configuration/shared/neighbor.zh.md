---
icon: material/lan
---

# 邻居解析

通过
[`source_mac_address`](/configuration/route/rule/#source_mac_address) 和
[`source_hostname`](/configuration/route/rule/#source_hostname) 规则项匹配局域网设备的 MAC 地址和主机名。

当这些规则项存在，或 [local DNS 服务器](/zh/configuration/dns/server/local/) 设置了 [neighbor_domain](/zh/configuration/dns/server/local/#neighbor_domain) 时，邻居解析自动启用。
使用 [`route.find_neighbor`](/configuration/route/#find_neighbor) 可在没有规则时强制启用以输出日志。

## Linux

原生支持，无需特殊设置。

主机名解析需要 DHCP 租约文件，
自动从常见 DHCP 服务器（dnsmasq、odhcpd、ISC dhcpd、Kea）检测。
可通过 [`route.dhcp_lease_files`](/configuration/route/#dhcp_lease_files) 设置自定义路径。

## macOS

原生支持，无需特殊设置。

参阅 [VPN 热点](/manual/misc/vpn-hotspot/#macos) 了解互联网共享设置。
