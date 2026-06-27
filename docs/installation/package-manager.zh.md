---
icon: material/package
---

# 包管理器

## :material-tram: 仓库安装

=== ":material-debian: Debian / APT"

    ```bash
    sudo mkdir -p /etc/apt/keyrings &&
       sudo curl -fsSL https://singlink.app/gpg.key -o /etc/apt/keyrings/sagernet.asc &&
       sudo chmod a+r /etc/apt/keyrings/sagernet.asc &&
       echo '
    Types: deb
    URIs: https://deb.sagernet.org/
    Suites: *
    Components: *
    Enabled: yes
    Signed-By: /etc/apt/keyrings/sagernet.asc
    ' | sudo tee /etc/apt/sources.list.d/sagernet.sources &&
       sudo apt-get update &&
       sudo apt-get install singlink # or singlink-beta
    ```

=== ":material-redhat: Redhat / DNF 5"

    ```bash
    sudo dnf config-manager addrepo --from-repofile=https://singlink.app/singlink.repo &&
    sudo dnf install singlink # or singlink-beta
    ```

=== ":material-redhat: Redhat / DNF 4"

    ```bash
    sudo dnf config-manager --add-repo https://singlink.app/singlink.repo &&
    sudo dnf -y install dnf-plugins-core &&
    sudo dnf install singlink # or singlink-beta
    ```

## :material-download-box: 手动安装

该脚本从 GitHub 发布中下载并安装最新的软件包，适用于基于 deb 或 rpm 的 Linux 发行版、ArchLinux 和 OpenWrt。

```shell
curl -fsSL https://singlink.app/install.sh | sh
```

或最新测试版：

```shell
curl -fsSL https://singlink.app/install.sh | sh -s -- --beta
```

或指定版本：

```shell
curl -fsSL https://singlink.app/install.sh | sh -s -- --version <version>
```

## :material-book-lock-open: 托管安装

=== ":material-linux: Linux"

    | 类型       | 平台            | 命令                           | 链接                                                                                                            |
    |----------|---------------|------------------------------|---------------------------------------------------------------------------------------------------------------|
    | AUR      | Arch Linux    | `? -S singlink`              | [![AUR package](https://repology.org/badge/version-for-repo/aur/singlink.svg)][aur]                           |
    | nixpkgs  | NixOS         | `nix-env -iA nixos.singlink` | [![nixpkgs unstable package](https://repology.org/badge/version-for-repo/nix_unstable/singlink.svg)][nixpkgs] |
    | Homebrew | macOS / Linux | `brew install singlink`      | [![Homebrew package](https://repology.org/badge/version-for-repo/homebrew/singlink.svg)][brew]                |
    | APK      | Alpine        | `apk add singlink`           | [![Alpine Linux Edge package](https://repology.org/badge/version-for-repo/alpine_edge/singlink.svg)][alpine]  |
    | DEB      | AOSC          | `apt install singlink`       | [![AOSC package](https://repology.org/badge/version-for-repo/aosc/singlink.svg)][aosc]                        |

=== ":material-apple: macOS"

    | 类型       | 平台    | 命令                      | 链接                                                                                             |
    |----------|-------|-------------------------|------------------------------------------------------------------------------------------------|
    | Homebrew | macOS | `brew install singlink` | [![Homebrew package](https://repology.org/badge/version-for-repo/homebrew/singlink.svg)][brew] |

=== ":material-microsoft-windows: Windows"

    | 类型         | 平台      | 命令                        | 链接                                                                                                  |
    |------------|---------|---------------------------|-----------------------------------------------------------------------------------------------------|
    | Scoop      | Windows | `scoop install singlink`  | [![Scoop package](https://repology.org/badge/version-for-repo/scoop/singlink.svg)][scoop]           |
    | Chocolatey | Windows | `choco install singlink`  | [![Chocolatey package](https://repology.org/badge/version-for-repo/chocolatey/singlink.svg)][choco] |
    | winget     | Windows | `winget install singlink` | [![winget package](https://repology.org/badge/version-for-repo/winget/singlink.svg)][winget]        |

=== ":material-freebsd: FreeBSD"

    | 类型         | 平台      | 命令                     | 链接                                                                                         |
    |------------|---------|------------------------|--------------------------------------------------------------------------------------------|
    | FreshPorts | FreeBSD | `pkg install singlink` | [![FreeBSD port](https://repology.org/badge/version-for-repo/freebsd/singlink.svg)][ports] |

## :material-alert: 存在问题的源

| 类型         | 平台      | 链接                                                                                        | 原因              |
|------------|---------|-------------------------------------------------------------------------------------------|-----------------|
| DEB        | AOSC    | [aosc-os-abbs](https://github.com/AOSC-Dev/aosc-os-abbs/tree/stable/app-network/singlink) | 存在问题的构建标志列表修改   |
| Homebrew   | /       | [homebrew-core][brew]                                                                     | 存在问题的构建标志列表修改   |
| FreshPorts | FreeBSD | [FreeBSD ports][ports]                                                                    | 太旧的 Go (go1.20) |

如果您是其用户，请向他们报告问题：

1. 在未完全了解相关功能的情况下，请勿修改发布版本标签：启用非默认标签可能会导致性能下降；缺少默认标签可能会引起用户混淆。
2. singlink 支持使用一些较旧的 Go 版本进行编译，但不推荐使用（特别是已不再受 Go 支持的版本）。

## :material-book-multiple: 服务管理

对于带有 [systemd][systemd] 的 Linux 系统，通常安装已经包含 singlink 服务，
您可以使用以下命令管理服务：

| 行动   | 命令                                            |
|------|-----------------------------------------------|
| 启用   | `sudo systemctl enable singlink`              |
| 禁用   | `sudo systemctl disable singlink`             |
| 启动   | `sudo systemctl start singlink`               |
| 停止   | `sudo systemctl stop singlink`                |
| 强行停止 | `sudo systemctl kill singlink`                |
| 重新启动 | `sudo systemctl restart singlink`             |
| 查看日志 | `sudo journalctl -u singlink --output cat -e` |
| 实时日志 | `sudo journalctl -u singlink --output cat -f` |

[alpine]: https://pkgs.alpinelinux.org/packages?name=singlink

[aur]: https://aur.archlinux.org/packages/singlink

[nixpkgs]: https://github.com/NixOS/nixpkgs/blob/nixos-unstable/pkgs/tools/networking/singlink/default.nix

[brew]: https://formulae.brew.sh/formula/singlink

[choco]: https://chocolatey.org/packages/singlink

[scoop]: https://github.com/ScoopInstaller/Main/blob/master/bucket/singlink.json

[winget]: https://github.com/microsoft/winget-pkgs/tree/master/manifests/s/SagerNet/singlink

[ports]: https://www.freshports.org/net/singlink

[aosc]: https://packages.aosc.io/packages/singlink

[systemd]: https://systemd.io/
