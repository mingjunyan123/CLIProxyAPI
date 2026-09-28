# Fork differences vs upstream

This repository tracks [router-for-me/CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) and keeps a small set of local changes.

Protocol translation, thinking suffixes, cloaking algorithms, signature validation, rate-limit handling, and the Claude Code version floor match upstream. Cloak versus pass-through is decided by native-client detection. This fork only adds two adapter entrypoints to that detection.

Fork-only code lives in dedicated files where possible. Upstream files keep a short hook so the next rebase does not replay the whole feature.

| Feature | Dedicated files | Remaining hook in an upstream file |
|---|---|---|
| Adapter native entrypoints | `helps/claude_adapter_compat.go` | one `\|\|` in `claude_client_detection.go` |
| Claude OAuth outbound logs | `helps/claude_oauth_request_log.go` | one call in `logging_helpers.go`; inbound skip in `request_logging.go`; config field and v8 path |
| GHCR image | `.github/workflows/ghcr-image.yml` | none |

## Claude native pass-through

Upstream treats only `cli`, `sdk-cli`, and `claude-vscode` as native Claude Code surfaces.

This fork also treats **`local-agent`** and **`claude-desktop-3p`** as native when the usual Claude Code strong signals are present (`X-App: cli`, a plausible `claude-cli/…` User-Agent, `claude-code-20250219` in Anthropic-Beta, and `metadata.user_id` except on `count_tokens`).

The version floor is upstream's. The measured baseline is Claude Code `2.1.280`. A client counts when its major and minor match that baseline and its patch is at least `280`. A confirmed native request keeps the caller's `User-Agent`, including the entrypoint suffix. `X-Stainless-Package-Version` and `X-Stainless-Runtime-Version` stay on the measured baseline (`0.112.1` / `v26.3.0`) when they differ. A client below the floor is not native, and its `User-Agent` is replaced with `claude-cli/2.1.280 (external, cli)`.

For those confirmed requests, cliproxyapi does not cloak the body into a CLI shape. It keeps:

- the caller's `User-Agent`
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

Direct `/v1/messages` OAuth requests that are not confirmed native are cloaked, matching upstream.

## Claude OAuth outbound logging

Two logging knobs differ from upstream.

`observability.logs.request-log: true` (legacy key `request-log`)

- Inbound HTTP capture is skipped.
- Only **Claude OAuth outbound** request payloads are written.

`observability.logs.claude-oauth-outbound-log: true` (legacy key `claude-oauth-outbound-log`)

- Independent of `request-log`.
- Writes the same Claude OAuth outbound payloads as gzip, without buffering the full request-log pipeline.
- Default is `false` (`config.example.yaml`).

Files go under `logs/claude-oauth/<account>/` as gzip, named `{entrypoint}-YYYYMMDD-HHMMSS.ffffff.gz` (for example `cli-20260907-095330.425323.gz`). `less` can open them. API-key Claude traffic and non-Claude providers are not written there.

## Container image

`.github/workflows/ghcr-image.yml` publishes this fork's image:

- `ghcr.io/mingjunyan123/cliproxyapi:latest` and `:sha`
- `linux/amd64` only (no arm64 build)

Upstream does not ship this workflow or this image name.
