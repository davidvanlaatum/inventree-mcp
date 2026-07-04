#!/usr/bin/env sh

set -eu

if command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet inventree-mcp.service; then
    systemctl stop inventree-mcp.service
fi
