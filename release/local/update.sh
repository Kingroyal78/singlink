#!/usr/bin/env bash

set -e -o pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/common.sh"

echo "Updating singlink from git repository..."
cd "$PROJECT_DIR"
git pull --ff-only

echo ""
echo "Running reinstall..."
exec "$SCRIPT_DIR/reinstall.sh"
