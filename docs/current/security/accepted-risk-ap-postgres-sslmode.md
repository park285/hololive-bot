# Accepted Risk: AP Postgres SSL Mode Downgrade

Status: accepted-risk, bounded
Opened: 2026-06-06
Owner: IRIS stack operator
Expiry: 2026-07-06

## Scope

This exception covers `POSTGRES_SSLMODE_ALLOW_INSECURE=true` in two bounded paths:

1. **AP path (primary scope).** Hololive AP youtube-producer containers (`youtube-producer-c`) running on the Osaka and Seoul AP hosts over the private Tailscale path, rendered by `deploy/compose/docker-compose.main-ap.live-compat.yml`. No other AP service receives the flag.
2. **Central live-compat opt-in (secondary scope).** The central host's explicit opt-in overlay `deploy/compose/docker-compose.live-compat.yml` inherits the flag to `hololive-bot`, `hololive-admin-api`, `hololive-alarm-worker`, `youtube-producer`, and `llm-scheduler` through its shared `x-live-postgres-env` anchor while preserving pre-hardening behavior. The base stack (`docker-compose.prod.yml` alone) renders the flag for none of these services; central exposure exists only while an operator explicitly stacks the live-compat overlay, with effective `POSTGRES_SSLMODE=require` (TLS on, certificate verification relaxed — never plaintext).

`admin-dashboard` receives the flag in neither path.

## Risk

`POSTGRES_SSLMODE_ALLOW_INSECURE=true` weakens normal Postgres TLS verification. The temporary justification is AP-to-central Postgres connectivity over a private Tailscale route while AP Postgres certificate SAN and server-name verification are being aligned. The central live-compat inheritance is a pre-hardening compatibility carry-over and shares the same expiry.

## Guardrails

- AP compose rendering must keep Iris egress tokens out of all AP youtube-producer containers.
- In the AP overlays the insecure flag must render only for AP youtube-producer services.
- The base prod stack must not render the insecure flag for any central service (contract: `TestRepoAPPostgresSSLModeLedgerScopeMatchesComposeRendering`).
- AP preflight and completion checks must require runtime and persisted QUIC UDP socket buffer values.
- This exception expires unless renewed by a dated follow-up commit.

## Exit Criteria

Remove this exception when AP Postgres TLS can run with `verify-full` using a stable server name and mounted CA bundle, and the central `docker-compose.live-compat.yml` anchor drops `POSTGRES_SSLMODE_ALLOW_INSECURE` (or the overlay itself is retired).
