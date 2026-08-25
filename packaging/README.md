# Package Upgrade Boundary

The `deb`, `rpm`, and `apk` packages own `/usr/bin/inventree-mcp` and must be upgraded with the same package manager that installed them. The local `inventree-mcp self-update` command always refuses that path, and `--adopt-direct-install` never overrides the refusal.

Direct GitHub archive installations use a separate explicit adoption marker adjacent to the executable. Package installation scripts must not create that marker. Alternate package formats or prefixes must likewise omit it so the updater fails closed when installation source is ambiguous.

## Packaged Configuration

The `deb`, `rpm`, and `apk` packages install a commented YAML configuration template at `/etc/inventree-mcp/config.yml` (mode `0600`, `noreplace`), and the packaged systemd unit runs `inventree-mcp serve --config /etc/inventree-mcp/config.yml`. Earlier packages installed an `EnvironmentFile`-based `/etc/inventree-mcp/inventree-mcp.env` instead; upgrading an existing packaged install requires copying the env file's values into the new YAML file before restarting the service, since the old file is left in place unmanaged and is no longer read by the unit.

See [`docs/self-update.md`](../docs/self-update.md) for the complete install-source, trust, archive, locking, and rollback policy.
