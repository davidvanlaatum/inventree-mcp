# Package Upgrade Boundary

The `deb`, `rpm`, and `apk` packages own `/usr/bin/inventree-mcp` and must be upgraded with the same package manager that installed them. The local `inventree-mcp self-update` command always refuses that path, and `--adopt-direct-install` never overrides the refusal.

Direct GitHub archive installations use a separate explicit adoption marker adjacent to the executable. Package installation scripts must not create that marker. Alternate package formats or prefixes must likewise omit it so the updater fails closed when installation source is ambiguous.

## Packaged Configuration

The `deb`, `rpm`, and `apk` packages install a commented YAML configuration template at `/etc/inventree-mcp/config.yml` (mode `0600`, `noreplace`, owned by the `inventree-mcp` system user/group), and the packaged systemd unit runs `inventree-mcp serve --config /etc/inventree-mcp/config.yml` as that non-root `inventree-mcp` user. The unit routes both output streams explicitly to journald with the `inventree-mcp` identifier, so startup failures are visible in `systemctl status` and `journalctl -u inventree-mcp.service`. `postinstall.sh` restarts an already-enabled/active service unconditionally on upgrade; if `config.yml` is still the placeholder template at that point, startup validation fails closed and the service stays down until the operator finishes configuring it.

## Non-Root Service Identity

`preinstall.sh` creates a dedicated `inventree-mcp` system user and group (via `useradd`/`groupadd`, falling back to Alpine's `adduser`/`addgroup`) before the package unpacks any files, so nfpm's per-file owner/group metadata for `config.yml` resolves correctly at install time. `postinstall.sh` also re-applies `inventree-mcp:inventree-mcp` ownership and mode `0600` to `config.yml` on every install and upgrade, covering the case where a package manager's conffile-preservation logic left an existing, still-root-owned `config.yml` from a pre-F-S87 install untouched. `postremove.sh` deletes the system user and group only on final package removal — `dpkg purge`, the final `rpm` erase (`$1 = 0`), or any `apk` removal, which has no separate purge concept — never on a version upgrade, so the identity and file ownership stay stable across in-place upgrades.

A package-created static system user/group was chosen over systemd's `DynamicUser=yes` because a dynamically allocated identity's UID/GID is not guaranteed to persist across reboots unless the unit also declares a `StateDirectory=`/`CacheDirectory=`/`LogsDirectory=`, which this service does not otherwise need; that would have risked `config.yml` ownership silently drifting out from under the service after a reboot.

Deleting the system user/group on purge/final removal is narrower than the common distro convention of never removing system accounts (adopted elsewhere specifically to avoid a future reinstall, or an unrelated new account, being allocated the same numeric UID/GID and inheriting access to anything still owned by it). This package accepts that trade-off deliberately: the only path ever chowned to the `inventree-mcp` UID/GID is the packaged `config.yml` conffile, which purge/final removal also deletes, and the unit has no `StateDirectory=`/`CacheDirectory=`/`LogsDirectory=` that could leave other state behind — so there is no lingering file for a future UID/GID reuse to expose.

See [`docs/self-update.md`](../docs/self-update.md) for the complete install-source, trust, archive, locking, and rollback policy.
