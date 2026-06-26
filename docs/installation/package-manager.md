---
icon: material/package
---

# Package Manager

## :material-tram: Repository Installation

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

## :material-download-box: Manual Installation

The script download and install the latest package from GitHub releases
for deb or rpm based Linux distributions, ArchLinux and OpenWrt.

```shell
curl -fsSL https://singlink.app/install.sh | sh
```

or latest beta:

```shell
curl -fsSL https://singlink.app/install.sh | sh -s -- --beta
```

or specific version:

```shell
curl -fsSL https://singlink.app/install.sh | sh -s -- --version <version>
```

## :material-book-lock-open: Managed Installation

=== ":material-linux: Linux"

    | Type     | Platform      | Command                      | Link                                                                                                          |
    |----------|---------------|------------------------------|---------------------------------------------------------------------------------------------------------------|
    | AUR      | Arch Linux    | `? -S singlink`              | [![AUR package](https://repology.org/badge/version-for-repo/aur/singlink.svg)][aur]                           |
    | nixpkgs  | NixOS         | `nix-env -iA nixos.singlink` | [![nixpkgs unstable package](https://repology.org/badge/version-for-repo/nix_unstable/singlink.svg)][nixpkgs] |
    | Homebrew | macOS / Linux | `brew install singlink`      | [![Homebrew package](https://repology.org/badge/version-for-repo/homebrew/singlink.svg)][brew]                |
    | APK      | Alpine        | `apk add singlink`           | [![Alpine Linux Edge package](https://repology.org/badge/version-for-repo/alpine_edge/singlink.svg)][alpine]  |
    | DEB      | AOSC          | `apt install singlink`       | [![AOSC package](https://repology.org/badge/version-for-repo/aosc/singlink.svg)][aosc]                        |

=== ":material-apple: macOS"

    | Type     | Platform | Command                 | Link                                                                                           |
    |----------|----------|-------------------------|------------------------------------------------------------------------------------------------|
    | Homebrew | macOS    | `brew install singlink` | [![Homebrew package](https://repology.org/badge/version-for-repo/homebrew/singlink.svg)][brew] |

=== ":material-microsoft-windows: Windows"

    | Type       | Platform | Command                   | Link                                                                                                |
    |------------|----------|---------------------------|-----------------------------------------------------------------------------------------------------|
    | Scoop      | Windows  | `scoop install singlink`  | [![Scoop package](https://repology.org/badge/version-for-repo/scoop/singlink.svg)][scoop]           |
    | Chocolatey | Windows  | `choco install singlink`  | [![Chocolatey package](https://repology.org/badge/version-for-repo/chocolatey/singlink.svg)][choco] |
    | winget     | Windows  | `winget install singlink` | [![winget package](https://repology.org/badge/version-for-repo/winget/singlink.svg)][winget]        |

=== ":material-android: Android"

    | Type   | Platform | Command            | Link                                                                                         |
    |--------|----------|--------------------|----------------------------------------------------------------------------------------------|
    | Termux | Android  | `pkg add singlink` | [![Termux package](https://repology.org/badge/version-for-repo/termux/singlink.svg)][termux] |

=== ":material-freebsd: FreeBSD"

    | Type       | Platform | Command                | Link                                                                                       |
    |------------|----------|------------------------|--------------------------------------------------------------------------------------------|
    | FreshPorts | FreeBSD  | `pkg install singlink` | [![FreeBSD port](https://repology.org/badge/version-for-repo/freebsd/singlink.svg)][ports] |

## :material-alert: Problematic Sources

| Type       | Platform | Link                                                                                      | Promblem(s)                             |
|------------|----------|-------------------------------------------------------------------------------------------|-----------------------------------------|
| DEB        | AOSC     | [aosc-os-abbs](https://github.com/AOSC-Dev/aosc-os-abbs/tree/stable/app-network/singlink) | Problematic build tag list modification |
| Homebrew   | /        | [homebrew-core][brew]                                                                     | Problematic build tag list modification |
| Termux     | Android  | [termux-packages][termux]                                                                 | Problematic build tag list modification |
| FreshPorts | FreeBSD  | [FreeBSD ports][ports]                                                                    | Old Go  (go1.20)                        |

If you are a user of them, please report issues to them:

1. Please do not modify release build tags without full understanding of the related functionality: enabling non-default
   labels may result in decreased performance; the lack of default labels may cause user confusion.
2. singlink supports compiling with some older Go versions, but it is not recommended (especially versions that are no
   longer supported by Go).

## :material-book-multiple: Service Management

For Linux systems with [systemd][systemd], usually the installation already includes a singlink service,
you can manage the service using the following command:

| Operation | Command                                       |
|-----------|-----------------------------------------------|
| Enable    | `sudo systemctl enable singlink`              |
| Disable   | `sudo systemctl disable singlink`             |
| Start     | `sudo systemctl start singlink`               |
| Stop      | `sudo systemctl stop singlink`                |
| Kill      | `sudo systemctl kill singlink`                |
| Restart   | `sudo systemctl restart singlink`             |
| Logs      | `sudo journalctl -u singlink --output cat -e` |
| New Logs  | `sudo journalctl -u singlink --output cat -f` |

[alpine]: https://pkgs.alpinelinux.org/packages?name=singlink

[aur]: https://aur.archlinux.org/packages/singlink

[nixpkgs]: https://github.com/NixOS/nixpkgs/blob/nixos-unstable/pkgs/tools/networking/singlink/default.nix

[brew]: https://formulae.brew.sh/formula/singlink

[openwrt]: https://github.com/openwrt/packages/tree/master/net/singlink

[immortalwrt]: https://github.com/immortalwrt/packages/tree/master/net/singlink

[choco]: https://chocolatey.org/packages/singlink

[scoop]: https://github.com/ScoopInstaller/Main/blob/master/bucket/singlink.json

[winget]: https://github.com/microsoft/winget-pkgs/tree/master/manifests/s/SagerNet/singlink

[termux]: https://github.com/termux/termux-packages/tree/master/packages/singlink

[ports]: https://www.freshports.org/net/singlink

[aosc]: https://packages.aosc.io/packages/singlink

[systemd]: https://systemd.io/
