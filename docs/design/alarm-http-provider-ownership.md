# Alarm HTTP Provider Ownership

## Decision

`alarm.http` keeps `admin-api` as the current HTTP provider registration. `alarm-worker` is the alarm domain owner and remains the target provider for a later migration.

## Current State

- `admin-api` registers `hololive-shared/pkg/service/alarm.APIHandler` when `AlarmCRUD` is configured.
- `bot` and the admin facade consume `/internal/alarm/*`.
- `alarm-worker` owns alarm checking and dispatch queue publishing, but it does not currently own the HTTP provider process.

## Short-Term Rule

- Keep `/internal/alarm/*` registered in `admin-api`.
- Do not split the current route set across multiple providers.
- Any HTTP DTO or error-envelope polish must preserve the existing `success`/`message` response compatibility.

## Long-Term Target

- Move `/internal/alarm/*` provider registration to `alarm-worker` in a dedicated migration PR.
- Keep `admin-api` as a facade/client if dashboard behavior still needs the routes.
- Update `CONTRACT_MAP.md`, `CONTRACT_MANIFEST.txt`, `docs/current/contracts/alarm.md`, service docs, and affected runbooks in the same migration.

## Required Migration Checks

- Route constants and client paths remain centralized.
- `bot`, `admin-api`, and dashboard callers keep backward-compatible behavior during rollout.
- Queue ownership remains within `alarm-worker` for publish, consume, and proactive egress.
- No RPC/gRPC transport is introduced.
