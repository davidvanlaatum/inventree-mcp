# Local CLI Self-Update Policy

`inventree-mcp self-update` is an explicit local CLI operation for binaries installed from this repository's GitHub release archives. It is not an MCP tool, is never registered through STDIO or HTTP, does not run in the background, and cannot be triggered by an MCP request.

## Supported Installations

The initial updater supports release binaries built for:

| Operating system | Architectures | Install source |
| --- | --- | --- |
| Linux | `amd64`, `arm64` | Direct GitHub release archive only. |
| macOS | `amd64`, `arm64` | Direct GitHub release archive only. |

Windows is intentionally unsupported because replacing a running executable safely requires a tested helper/relaunch design. The updater never attempts privilege elevation. The current executable and its parent directory must be owned by the effective user; the executable must be a regular executable with one hard link, and neither it nor its parent may be a symlink. The parent directory must not be group- or world-writable.

The repository's `deb`, `rpm`, and `apk` packages install `/usr/bin/inventree-mcp`; that path is always refused with package-manager guidance. Homebrew-managed macOS paths under `/opt/homebrew` or a `Cellar` are also refused. Operators must use the package manager or perform a reviewed manual update for every refused installation.

Because path heuristics cannot identify every alternate package prefix, a direct archive installation must be adopted once before its first update:

```sh
inventree-mcp self-update --adopt-direct-install
```

Adoption performs the same platform, exact-release-version, executable, ownership, link, parent-directory, and trusted-ancestor checks as an update, refuses every known package path regardless of operator intent, and creates an adjacent owner-only `inventree-mcp.direct-install` marker bound to the canonical repository and executable path. It performs no network request and does not update the binary. Operators must run it only after independently confirming that the current binary came directly from this repository's canonical GitHub release archive. Normal updates fail closed when the marker is missing, unsafe, or belongs to another path.

## Version Policy

With no flags, `self-update` resolves GitHub's latest stable release. `--version vX.Y.Z` selects one exact stable release. The current build and target must both use exact released semantic versions. Prereleases, build-metadata versions, downgrades, development builds, unsupported platforms, and current-version requests fail or return a no-op without changing the executable.

## Version Output Format And Its One-Time Migration

`inventree-mcp version` prints a versioned, key-based porcelain-style machine format: an explicit `porcelain: 1` marker followed by `key: value` lines for `version`, `commit`, and `date`, plus the optional `inventree_version`/`inventree_api` InvenTree baseline whenever the build carries `internal/buildinfo`'s baseline constants. Parsers split each line on the first `": "` occurrence — not a fixed line count and not a naive split on every colon, since `date` is RFC3339 and itself contains colons — and look up known keys by name, tolerating any additional keys a later revision adds. Only an incompatible change to an existing key's meaning requires bumping the `porcelain` marker.

`self-update`'s safety check (`requireVersion`) runs this same `version` command against the staged candidate before replacing the installed executable, requires the `porcelain` marker to be a version it understands, and fails closed otherwise. It reuses that same pre-rename parse — not a second execution of the candidate — to report the old-to-new InvenTree baseline alongside the old-to-new MCP version in `self-update`'s result summary, omitting the baseline line when the InvenTree fields are legitimately absent from an otherwise-valid response.

This porcelain format replaced a fixed 3-line `version`/`commit`/`date` output as a deliberate, one-time breaking change, accepted while the operator is the only current user. Every binary released before that change shipped (`v0.0.1`-`v0.0.11` and any that followed before it merged) has a compiled-in `requireVersion` that still expects the old exact-3-line shape; it rejects the first release containing the new format as "malformed version output" and refuses to self-update into it. That specific release must be installed manually: download its release archive from the repository's GitHub Releases page (see README's [Install From A Release](../README.md#install-from-a-release) section for the archive layout), verify it against the published `checksums.txt`, replace the executable, then run `inventree-mcp version` to confirm it reports the new format. Every release after it self-updates normally on the new versioned contract. This must not recur casually after `v1.0.0` — the same porcelain marker should be used to negotiate any future breaking change instead of repeating an undocumented break.

## Release Trust And Network Policy

The initial trust root is canonical GitHub HTTPS plus control of `davidvanlaatum/inventree-mcp` releases. The updater downloads the exact platform archive and `checksums.txt`, then requires the archive's published SHA-256 checksum before parsing or executing it. This detects corruption and mismatched assets, but it does not protect against compromise of the GitHub repository or release workflow because the archive and checksum share the same trust root. A future independently authenticated signature or attestation requires a separate release-policy decision and pinned verification identity.

The updater uses a dedicated HTTP transport with no cookie jar and no inherited proxy settings. It permits only the canonical GitHub API, repository release URLs, and reviewed GitHub asset/CDN redirect hosts; it caps redirects, response headers, total time, metadata, checksum, and archive bytes. If `GITHUB_TOKEN` is set, it is sent only to `https://api.github.com` and is removed before every cross-origin redirect. InvenTree and MCP credentials are never added to update requests or subprocess environments.

## Archive And Replacement Policy

Future GoReleaser archives contain exactly one regular `inventree-mcp` executable. Extraction rejects directories, extra or duplicate entries, traversal, symlinks, hardlinks, devices, missing execute bits, oversized or truncated content, gzip multistreams, and trailing compressed or tar payload. Release `v0.0.1` predates this archive contract and contains `README.md`; it is intentionally not a valid update target. The first usable target is the next release produced with the single-binary archive configuration.

Before replacement, the updater runs only the staged binary's bounded `version` command with disconnected stdin, a safe working directory, bounded output, a short timeout, and an allowlisted environment containing only locale and system `PATH` values. The known project `version` path performs no configuration or network initialization. This sanitization is not an operating-system sandbox against an artifact accepted from a compromised trust root.

The updater then:

1. Opens the persistent owner-only `<executable>.self-update.lock` without following symlinks and acquires a non-blocking kernel advisory lock. The kernel releases the lock automatically when an updater exits or is killed, so recovery never unlinks or guesses the age of a stale pathname and cannot race a newly acquired lock.
2. Creates unpredictable owner-only staging and rollback files on the executable's filesystem.
3. Revalidates the executable's identity, mode, size, ownership, link count, and parent directory immediately before replacement.
4. Writes and syncs an owner-only transaction record, atomically renames the staged executable into place, and syncs the parent directory.
5. Re-runs the bounded `version` command through the installed path.
6. On success, publishes the prior binary as `<executable>.previous` while retaining the transaction rollback source until a cleanup-only committed record or its removal is durably synced. On any pre-commit failure, it restores the exact prior executable and pre-existing `.previous` bytes and mode, syncs that restored state, and transitions the record to cleanup-only before deleting artifacts. A later invocation restores an interrupted pre-commit transaction, exits non-zero without selecting or downloading a release, and tells the operator to rerun. Inspect `inventree-mcp version`, then rerun the original self-update command so release selection uses the restored executable. A leftover cleanup-only record from either a completed install or rollback removes transaction artifacts idempotently without changing the current executable, then continues normally.

No-op, unsupported, network, metadata, checksum, archive, staging, and pre-replacement failures leave the installed executable and existing `.previous` recovery binary unchanged.

The updater does not expose an automated downgrade or rollback command. If a later smoke test finds an operational regression, stop every process using the binary, verify no updater is running, confirm the `.previous` file is an owner-controlled regular executable in the same protected directory, copy it to a new owner-only same-directory staging file, run the staging file's `version` command, and atomically rename that staging file over the current executable. Re-run `version` before restarting service processes. Do not use this procedure for package-managed paths or to bypass ownership and privilege refusals.

## Maintained Library Evaluation

The implementation evaluated `github.com/creativeprojects/go-selfupdate` `v1.6.0`, the actively maintained successor to `rhysd/go-github-selfupdate`. It supports GitHub release discovery, GoReleaser-style filenames, a single `checksums.txt`, context cancellation, archive decompression, Windows, and basic rollback. Its release/checksum model matches this repository better than `minio/selfupdate`, which focuses on applying caller-supplied binary streams and has a less recent tagged release.

The library was not adopted because F-S18 would still need project-owned code for the strict canonical-origin/credential policy, exact-one-entry and trailing-payload archive rules, compressed/expanded bounds, package-manager refusal and direct-install adoption, ownership/symlink/hardlink checks, target identity revalidation, kernel-held cross-process locking and killed-process recovery, isolated staged/installed version probes, durable transaction recovery, and exact `.previous` handling. Using the library only for discovery would retain most custom security-critical code while adding a second HTTP and release-selection abstraction. The implementation therefore keeps a small injectable GitHub release client and replacement engine in `internal/selfupdate` and tests those responsibilities directly.
