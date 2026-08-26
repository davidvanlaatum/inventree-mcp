#!/usr/bin/env sh

set -eu

nologin_shell=/usr/sbin/nologin
if [ ! -x "$nologin_shell" ]; then
    nologin_shell=/sbin/nologin
fi
if [ ! -x "$nologin_shell" ]; then
    nologin_shell=/bin/false
fi

if ! getent group inventree-mcp >/dev/null 2>&1; then
    if command -v groupadd >/dev/null 2>&1; then
        groupadd --system inventree-mcp
    elif command -v addgroup >/dev/null 2>&1; then
        addgroup -S inventree-mcp
    else
        echo "preinstall.sh: neither groupadd nor addgroup is available; cannot create the inventree-mcp group" >&2
        exit 1
    fi
fi

if ! getent passwd inventree-mcp >/dev/null 2>&1; then
    if command -v useradd >/dev/null 2>&1; then
        useradd --system --gid inventree-mcp --home-dir /nonexistent \
            --no-create-home --shell "$nologin_shell" inventree-mcp
    elif command -v adduser >/dev/null 2>&1; then
        adduser -S -D -H -h /nonexistent -s "$nologin_shell" -G inventree-mcp inventree-mcp
    else
        echo "preinstall.sh: neither useradd nor adduser is available; cannot create the inventree-mcp user" >&2
        exit 1
    fi
fi
