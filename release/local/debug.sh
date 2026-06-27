#!/usr/bin/env bash

set -e -o pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/common.sh"

setup_environment

echo "Updating singlink from git repository..."
cd "$PROJECT_DIR"
git pull --ff-only

BUILD_TAGS=$(get_build_tags "debug")

build_singlink "$BUILD_TAGS"

stop_service
install_binary
start_service

echo ""
echo "Following service logs (Ctrl+C to exit)..."
sudo journalctl -u "$SERVICE_NAME" --output cat -f
