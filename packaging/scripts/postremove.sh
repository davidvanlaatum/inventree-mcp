#!/usr/bin/env sh

set -eu

if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload
fi

# This package is built once per format (deb/rpm/apk) and only runs on that format's own target
# distribution, so detecting the active package manager by which tool is present on PATH is
# equivalent to detecting which format installed this package.
final_removal=0
if command -v dpkg >/dev/null 2>&1; then
    if [ "${1:-}" = "purge" ]; then
        final_removal=1
    fi
elif command -v rpm >/dev/null 2>&1; then
    if [ "${1:-}" = "0" ]; then
        final_removal=1
    fi
elif command -v apk >/dev/null 2>&1; then
    final_removal=1
fi

if [ "$final_removal" -eq 1 ]; then
    if command -v userdel >/dev/null 2>&1; then
        userdel inventree-mcp >/dev/null 2>&1 || true
    elif command -v deluser >/dev/null 2>&1; then
        deluser inventree-mcp >/dev/null 2>&1 || true
    fi

    if command -v groupdel >/dev/null 2>&1; then
        groupdel inventree-mcp >/dev/null 2>&1 || true
    elif command -v delgroup >/dev/null 2>&1; then
        delgroup inventree-mcp >/dev/null 2>&1 || true
    fi
fi
