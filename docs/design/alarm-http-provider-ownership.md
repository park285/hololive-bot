# Alarm HTTP Provider Ownership

## Decision

`alarm.http` is in a staged provider migration. `alarm-worker` now registers the target `/internal/alarm/*` provider route set, while `admin-api` remains as a compatibility provider until all callers are cut over.

## Current State

- `alarm-worker` registers `hololive-shared/pkg/service/alarm.Handler` when `AlarmCRUD` is configured.
- `admin-api` still registers the same shared handler during the migration window.
- `bot` and the admin facade consume `/internal/alarm/*`.
- `alarm-worker` owns alarm checking, dispatch queue publishing, dispatch consumption, and proactive egress.

## Short-Term Rule

- Keep the route set identical between `alarm-worker` and `admin-api` compatibility registration.
- Do not split the current route set across different providers; staged duplication is allowed only while the full route set exists on both sides.
- Any HTTP DTO or error-envelope polish must preserve the existing `success`/`message` response compatibility.
- Move caller base URLs to `alarm-worker` before removing the `admin-api` compatibility provider.

## Long-Term Target

- Remove `/internal/alarm/*` provider registration from `admin-api` after caller cutover.
- Keep `admin-api` as a facade/client if dashboard behavior still needs the routes.
- Keep `alarm-worker` as the sole alarm HTTP provider and domain owner.
- Update `CONTRACT_MAP.md`, `CONTRACT_MANIFEST.txt`, `docs/current/contracts/alarm.md`, service docs, and affected runbooks in the same migration.

## Required Migration Checks

- Route constants and client paths remain centralized.
- `bot`, `admin-api`, and dashboard callers keep backward-compatible behavior during rollout.
- Queue ownership remains within `alarm-worker` for publish, consume, and proactive egress.
- No RPC/gRPC transport is introduced.
