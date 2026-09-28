# Fork differences vs upstream

This repository tracks [router-for-me/CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) and keeps a small set of local changes.

Protocol translation, thinking suffixes, cloaking algorithms, and rate-limit handling match upstream. Cloak vs pass-through is decided by native-client detection; this fork only widens that detection.

Fork-only code lives in dedicated files where possible. Upstream files keep a short hook so the next rebase does not replay the whole feature.

| Feature | Dedicated files | Remaining hook in an upstream file |
|---|---|---|
| Adapter native entrypoints | `helps/claude_adapter_compat.go` | one `\|\|` in `claude_client_detection.go` |
| Claude OAuth outbound logs | `helps/claude_oauth_request_log.go` | one call in `logging_helpers.go`; inbound skip in `request_logging.go`; config field |
| GHCR image | `.github/workflows/ghcr-image.yml` | none |
| Native version floor | — | `claude_device_profile.go`, helper check in `claude_client_detection.go` |
| Model-free CAIS signatures (pending data) | extra tests in `internal/signature/claude_test.go` | `claude_validation.go`, `provider_compatibility.go`, warn in `claude_executor.go` |

## Claude native pass-through

Upstream treats only `cli`, `sdk-cli`, and `claude-vscode` as native Claude Code surfaces.

This fork also treats **`local-agent`** and **`claude-desktop-3p`** as native when the usual Claude Code strong signals are present (`X-App: cli`, a plausible `claude-cli/…` User-Agent at or above the measured baseline, `claude-code-20250219` in Anthropic-Beta, and `metadata.user_id` except on `count_tokens`).

Native confirmation is a **version floor**, not an exact match: `claude-cli/2.1.259+` still counts. A confirmed native request keeps the caller's `User-Agent` verbatim, version and entrypoint suffix included. `X-Stainless-Package-Version` and `X-Stainless-Runtime-Version` are still pinned to the measured baseline (`0.112.1` / `v26.3.0`) when they differ. A client below the floor is not native; its `User-Agent` is replaced with the baseline CLI identity, `claude-cli/2.1.258 (external, cli)`.

For those confirmed requests, cliproxyapi does **not** cloak the body into a CLI shape. It keeps:

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

## Claude OAuth outbound logging

Two logging knobs differ from upstream.

`request-log: true`

- Inbound HTTP capture is skipped.
- Only **Claude OAuth outbound** request payloads are written.

`claude-oauth-outbound-log: true`

- Independent of `request-log`.
- Writes the same Claude OAuth outbound payloads as gzip, without buffering the full request-log pipeline.
- Default is `false` (`config.example.yaml`).

Files go under `logs/claude-oauth/<account>/` as gzip, named `{entrypoint}-YYYYMMDD-HHMMSS.ffffff.gz` (for example `cli-20260907-095330.425323.gz`). `less` can open them. API-key Claude traffic and non-Claude providers are not written there.

## Signatures

Model-free CAIS thinking signatures (Fable 5.1 generation) are still in the tree versus upstream. Keep or drop after looking at traffic; this is not a decided fork difference yet.

## Container image

`.github/workflows/ghcr-image.yml` publishes this fork's image:

- `ghcr.io/mingjunyan123/cliproxyapi:latest` and `:sha`
- `linux/amd64` only (no arm64 build)

Upstream does not ship this workflow or this image name.
