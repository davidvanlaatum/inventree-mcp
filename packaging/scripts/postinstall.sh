#!/usr/bin/env sh

set -eu

if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload

    if systemctl is-enabled --quiet inventree-mcp.service || systemctl is-active --quiet inventree-mcp.service; then
        systemctl restart inventree-mcp.service
    fi
fi
