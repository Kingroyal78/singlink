#!/bin/sh

set -eu

repo="Kingroyal78/singlink"
version=""
install_dir="/usr/local/bin"
config_dir="/etc/singlink"
data_dir="/var/lib/singlink"
install_service="true"

usage() {
  cat <<EOF
Usage: $0 [options]

Options:
  --repo <owner/repo>       GitHub repository. Default: ${repo}
  --version <version>      Release version, with or without leading v
  --install-dir <path>     Binary install directory. Default: ${install_dir}
  --config-dir <path>      Config directory. Default: ${config_dir}
  --data-dir <path>        Working data directory. Default: ${data_dir}
  --no-service             Do not install systemd service
  -h, --help               Show this help
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --repo)
      shift
      [ "$#" -gt 0 ] || { echo "missing value for --repo" >&2; exit 1; }
      repo="$1"
      ;;
    --version)
      shift
      [ "$#" -gt 0 ] || { echo "missing value for --version" >&2; exit 1; }
      version="$1"
      ;;
    --install-dir)
      shift
      [ "$#" -gt 0 ] || { echo "missing value for --install-dir" >&2; exit 1; }
      install_dir="$1"
      ;;
    --config-dir)
      shift
      [ "$#" -gt 0 ] || { echo "missing value for --config-dir" >&2; exit 1; }
      config_dir="$1"
      ;;
    --data-dir)
      shift
      [ "$#" -gt 0 ] || { echo "missing value for --data-dir" >&2; exit 1; }
      data_dir="$1"
      ;;
    --no-service)
      install_service="false"
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
  shift
done

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

need_cmd curl
need_cmd tar
need_cmd uname

if command -v sha256sum >/dev/null 2>&1; then
  sha256_cmd="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
  sha256_cmd="shasum -a 256"
else
  echo "missing required command: sha256sum or shasum" >&2
  exit 1
fi

if command -v sudo >/dev/null 2>&1 && [ "$(id -u)" -ne 0 ]; then
  sudo_cmd="sudo"
else
  sudo_cmd=""
fi

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
if [ "$os" != "linux" ]; then
  echo "unsupported OS for this installer: $os" >&2
  exit 1
fi

machine="$(uname -m)"
case "$machine" in
  x86_64|amd64) arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
  armv7l|armv7*) arch="armv7" ;;
  armv6l|armv6*) arch="armv6" ;;
  i386|i686|x86) arch="386" ;;
  riscv64) arch="riscv64" ;;
  ppc64le) arch="ppc64le" ;;
  s390x) arch="s390x" ;;
  *)
    echo "unsupported architecture: $machine" >&2
    exit 1
    ;;
esac

github_api_headers=""
if [ -n "${GITHUB_TOKEN:-}" ]; then
  github_api_headers="-H Authorization: Bearer ${GITHUB_TOKEN}"
fi

if [ -z "$version" ]; then
  # shellcheck disable=SC2086
  latest_json="$(curl -fsSL $github_api_headers "https://api.github.com/repos/${repo}/releases/latest")"
  version="$(printf '%s\n' "$latest_json" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"v\{0,1\}\([^"]*\)".*/\1/p' | head -n 1)"
fi

version="${version#v}"
if [ -z "$version" ]; then
  echo "could not determine release version" >&2
  exit 1
fi

tag="v${version}"
asset="singlink_${version}_${os}_${arch}.tar.gz"
base_url="https://github.com/${repo}/releases/download/${tag}"

tmp_dir="$(mktemp -d)"
cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT INT TERM

echo "Downloading ${asset}"
curl -fL "${base_url}/${asset}" -o "${tmp_dir}/${asset}"
curl -fL "${base_url}/checksums.txt" -o "${tmp_dir}/checksums.txt"

expected_line="$(grep " ${asset}\$" "${tmp_dir}/checksums.txt" || true)"
if [ -z "$expected_line" ]; then
  echo "checksum entry not found for ${asset}" >&2
  exit 1
fi

actual_sum="$($sha256_cmd "${tmp_dir}/${asset}" | awk '{print $1}')"
expected_sum="$(printf '%s\n' "$expected_line" | awk '{print $1}')"
if [ "$actual_sum" != "$expected_sum" ]; then
  echo "checksum mismatch for ${asset}" >&2
  echo "expected: ${expected_sum}" >&2
  echo "actual:   ${actual_sum}" >&2
  exit 1
fi

tar -C "$tmp_dir" -xzf "${tmp_dir}/${asset}"
package_dir="${tmp_dir}/singlink_${version}_${os}_${arch}"

echo "Installing singlink to ${install_dir}/singlink"
$sudo_cmd install -d "$install_dir"
$sudo_cmd install -m 0755 "${package_dir}/singlink" "${install_dir}/singlink"

echo "Installing default config if missing"
$sudo_cmd install -d "$config_dir"
if [ ! -f "${config_dir}/config.json" ]; then
  $sudo_cmd install -m 0644 "${package_dir}/config.example.json" "${config_dir}/config.json"
fi

$sudo_cmd install -d "$data_dir"

if [ "$install_service" = "true" ] && command -v systemctl >/dev/null 2>&1; then
  service_path="/etc/systemd/system/singlink.service"
  echo "Installing systemd service to ${service_path}"
  $sudo_cmd tee "$service_path" >/dev/null <<EOF
[Unit]
Description=singlink service
Documentation=https://github.com/${repo}
After=network.target nss-lookup.target network-online.target

[Service]
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE CAP_SYS_PTRACE CAP_DAC_READ_SEARCH
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE CAP_SYS_PTRACE CAP_DAC_READ_SEARCH
ExecStart=${install_dir}/singlink -D ${data_dir} -C ${config_dir} run
ExecReload=/bin/kill -HUP \$MAINPID
Restart=on-failure
RestartSec=10s
LimitNOFILE=infinity

[Install]
WantedBy=multi-user.target
EOF
  $sudo_cmd systemctl daemon-reload
fi

echo "Installed singlink ${version} for ${os}/${arch}."
echo "Config: ${config_dir}/config.json"
if [ "$install_service" = "true" ] && command -v systemctl >/dev/null 2>&1; then
  echo "Start with: sudo systemctl enable --now singlink"
else
  echo "Run with: ${install_dir}/singlink -D ${data_dir} -C ${config_dir} run"
fi
