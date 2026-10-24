# Fork differences vs upstream

This repository tracks [router-for-me/CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) and keeps a small set of local changes.

Protocol translation, thinking suffixes, cloaking algorithms, signature validation, rate-limit handling, and the Claude Code version floor match upstream. Cloak versus pass-through is decided by native-client detection. This fork adds two adapter entrypoints to that detection, and it does not require `metadata.user_id` for confirmation.

Fork-only code lives in dedicated files where possible. Upstream files keep a short hook so the next rebase does not replay the whole feature.

| Feature | Dedicated files | Remaining hook in an upstream file |
|---|---|---|
| Adapter native entrypoints | `helps/claude_adapter_compat.go` | one `\|\|` in `claude_client_detection.go` |
| Native confirmation ignores `metadata.user_id` | none | `standardSignals` in `claude_client_detection.go` drops that term |
| Inbound `device_id` | `helps/claude_adapter_compat.go` | one call in `ApplyClaudeCredentialMetadata` |
| Claude OAuth outbound logs | `helps/claude_oauth_request_log.go`, `internal/config/claude_oauth_outbound_log.go` | one call in `logging_helpers.go`; config field at the end of `Config`; v8 path appended after the upstream prefix list |
| GHCR image | `.github/workflows/ghcr-image.yml` | none |

## Claude native pass-through

Upstream treats `cli`, `sdk-cli`, `sdk-ts`, and `claude-vscode` as native Claude Code surfaces.

This fork also treats **`local-agent`** and **`claude-desktop-3p`** as native when the usual Claude Code strong signals are present (`X-App: cli`, a plausible `claude-cli/…` User-Agent, and `claude-code-20250219` in Anthropic-Beta). A missing or invalid `metadata.user_id` does not by itself make the request unconfirmed. The measured Haiku helper profile still checks that field as part of its own body shape.

The version floor is upstream's. The default measured baseline is Claude Code `2.1.280`. A client counts when its major and minor match the configured baseline and its patch is at least that baseline's patch. A confirmed native request keeps the caller's `User-Agent`, including the entrypoint suffix. Without device-profile stabilization or explicit version pins, plausible `X-Stainless-Package-Version` and `X-Stainless-Runtime-Version` values at or above the baseline (`0.112.1` / `v26.3.0` by default) are preserved. With effective cloak mode `never`, confirmed caller versions at or above the configured baseline take precedence over device-profile stabilization and version pins; missing, invalid, or older values fall back to the configured defaults. A client below the User-Agent floor is not confirmed native and receives the configured CLI fingerprint.

For those confirmed requests, cliproxyapi does not cloak the body into a CLI shape. It keeps:

- the caller's `User-Agent`
- the caller's billing header (`cc_version` suffix and `cc_entrypoint`)
- system blocks as sent
- tool names as sent (including names unknown to the CLI cloak)
- sampling fields, subject only to Anthropic validity rules
- no automatic `context_management` injection (that path is cloak-only)

It still rewrites OAuth identity the same way as upstream:

- a non-empty inbound `metadata.user_id.device_id` string is kept; a missing or blank one comes from the credential file's `claude_device_ids`
- `account_uuid` comes from the OAuth credential
- `session_id` is the CPA session
- extra `user_id` fields such as `parent_session_id` are kept
- OAuth still signs `cch=`
- `advisor-tool-2026-03-01` is still inserted when the body declares an advisor tool

Other entrypoints (`sdk-py`, `claude-desktop`, remote, and so on) remain unconfirmed by default and are cloaked as CLI on OAuth. With effective cloak mode `never`, requests with a confirmed Claude Code shape also preserve caller headers and use native cache and sampling handling. Copying the User-Agent alone is insufficient.

Direct `/v1/messages` OAuth requests that are not confirmed native are cloaked by default, matching upstream; an explicit cloak mode can override that default.

## Claude OAuth outbound logging

`request-log` matches upstream. Inbound HTTP capture and the normal request log stay on that switch.

`observability.logs.claude-oauth-outbound-log: true` (legacy key `claude-oauth-outbound-log`)

- Independent of `request-log`.
- Writes Claude OAuth upstream request payloads as gzip.
- Default is `false` (`config.example.yaml`).

Files go under `logs/claude-oauth/<account>/` as gzip, named `{entrypoint}-YYYYMMDD-HHMMSS.ffffff.gz` (for example `cli-20260907-095330.425323.gz`). `less` can open them. API-key Claude traffic and non-Claude providers are not written there.

## Container image

`.github/workflows/ghcr-image.yml` publishes this fork's image:

- `ghcr.io/mingjunyan123/cliproxyapi:latest` and `:sha`
- `linux/amd64` only (no arm64 build)

Upstream does not ship this workflow or this image name.
