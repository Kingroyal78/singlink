#!/usr/bin/env bash

set -e -o pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
BINARY_NAME="singlink"
SERVICE_NAME="singlink"

INSTALL_BIN_PATH="/usr/local/bin"
INSTALL_CONFIG_PATH="/etc/singlink"
INSTALL_DATA_PATH="/var/lib/singlink"
SYSTEMD_SERVICE_PATH="/etc/systemd/system"

DEFAULT_BUILD_TAGS="$(cat "$PROJECT_DIR/release/DEFAULT_BUILD_TAGS_OTHERS")"

setup_environment() {
    if [ -d /usr/local/go ]; then
        export PATH="$PATH:/usr/local/go/bin"
    fi

    if ! command -v go &> /dev/null; then
        echo "Error: Go is not installed or not in PATH"
        echo "Run install_go.sh to install Go"
        exit 1
    fi
}

get_build_tags() {
    local extra_tags="$1"
    if [ -n "$extra_tags" ]; then
        echo "${DEFAULT_BUILD_TAGS},${extra_tags}"
    else
        echo "${DEFAULT_BUILD_TAGS}"
    fi
}

get_version() {
    cd "$PROJECT_DIR"
    GOHOSTOS=$(go env GOHOSTOS)
    GOHOSTARCH=$(go env GOHOSTARCH)
    CGO_ENABLED=0 GOOS=$GOHOSTOS GOARCH=$GOHOSTARCH go run ./cmd/internal/read_tag
}

get_ldflags() {
    local version
    version=$(get_version)
    local shared_ldflags
    shared_ldflags=$(cat "$PROJECT_DIR/release/LDFLAGS")
    echo "-X 'github.com/singlink/singlink/constant.Version=${version}' ${shared_ldflags} -s -w -buildid="
}

build_singlink() {
    local tags="$1"
    local ldflags
    ldflags=$(get_ldflags)

    echo "Building singlink with tags: $tags"
    cd "$PROJECT_DIR"
    export GOTOOLCHAIN=local
    go build -v -trimpath -o "$(go env GOPATH)/bin/${BINARY_NAME}" -ldflags "$ldflags" -tags "$tags" ./cmd/singlink
}

install_binary() {
    local gopath
    gopath=$(go env GOPATH)
    echo "Installing binary to $INSTALL_BIN_PATH/$BINARY_NAME"
    sudo install -Dm755 "${gopath}/bin/${BINARY_NAME}" "${INSTALL_BIN_PATH}/${BINARY_NAME}"
}

setup_config() {
    echo "Setting up configuration"
    sudo install -d "$INSTALL_CONFIG_PATH"
    if [ ! -f "$INSTALL_CONFIG_PATH/config.json" ]; then
        sudo cp "$PROJECT_DIR/release/config/config.json" "$INSTALL_CONFIG_PATH/config.json"
        echo "Default config installed to $INSTALL_CONFIG_PATH/config.json"
    else
        echo "Config already exists at $INSTALL_CONFIG_PATH/config.json (not overwriting)"
    fi
}

setup_systemd() {
    echo "Setting up systemd service"
    sudo install -Dm644 "$SCRIPT_DIR/${SERVICE_NAME}.service" "$SYSTEMD_SERVICE_PATH/${SERVICE_NAME}.service"
    sudo systemctl daemon-reload
}

stop_service() {
    if systemctl is-active --quiet "$SERVICE_NAME"; then
        echo "Stopping $SERVICE_NAME service"
        sudo systemctl stop "$SERVICE_NAME"
    fi
}

start_service() {
    echo "Starting $SERVICE_NAME service"
    sudo systemctl start "$SERVICE_NAME"
}

restart_service() {
    echo "Restarting $SERVICE_NAME service"
    sudo systemctl restart "$SERVICE_NAME"
}
