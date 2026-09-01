# Fork differences vs upstream

This repository tracks [router-for-me/CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) and keeps a small set of local changes.

Compared at `main` against upstream `main`. Protocol translation, thinking suffixes, cloaking algorithms, rate-limit handling, and credential `device_id` selection match upstream.

## Claude native pass-through

Upstream treats only `cli`, `sdk-cli`, and `claude-vscode` as native Claude Code surfaces.

This fork also treats **`local-agent`** and **`claude-desktop-3p`** as native when the usual Claude Code strong signals are present (`X-App: cli`, a plausible `claude-cli/…` User-Agent, Anthropic-Beta, and `metadata.user_id` except on `count_tokens`).

For those confirmed requests, cliproxyapi does **not** cloak the body into a CLI shape. It keeps:

- the caller's billing header (`cc_version` suffix and `cc_entrypoint`)
- system blocks as sent
- tool names as sent (including names unknown to the CLI cloak)
- sampling fields, subject only to Anthropic validity rules
- no automatic `context_management` injection (that path is cloak-only)

It still rewrites OAuth identity the same way as upstream:

- `metadata.user_id.device_id` comes from the credential file's `claude_device_ids`
- `account_uuid` comes from the OAuth credential
- `session_id` is the CPA session
- extra `user_id` fields such as `parent_session_id` are kept
- OAuth still signs `cch=`
- `advisor-tool-2026-03-01` is still inserted when the body declares an advisor tool

Other entrypoints (`sdk-ts`, `sdk-py`, `claude-desktop`, remote, copies of the User-Agent, and so on) are still cloaked as CLI.

## Claude OAuth outbound logging

Two logging knobs differ from upstream.

`request-log: true`

- Inbound HTTP capture is skipped.
- Only **Claude OAuth outbound** request payloads are written, under `logs/claude-oauth/<account>/`.

`claude-oauth-outbound-log: true`

- Independent of `request-log`.
- Writes the same Claude OAuth outbound payloads as gzip files, without buffering the full request-log pipeline.
- Default is `false` (`config.example.yaml`).

API-key Claude traffic and non-Claude providers are not covered by these outbound files.

## Container image

`.github/workflows/ghcr-image.yml` publishes this fork's image:

- `ghcr.io/mingjunyan123/cliproxyapi:latest` and `:sha`
- `linux/amd64` only (no arm64 build)

Upstream does not ship this workflow or this image name.
