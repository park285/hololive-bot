# Accepted Risk: AP Postgres SSL Mode Downgrade

Status: accepted-risk, bounded
Opened: 2026-06-06
Owner: IRIS stack operator
Expiry: 2026-07-06

## Scope

This exception applies only to Hololive AP youtube-producer containers running on the Osaka and Seoul AP hosts over the private Tailscale path.

It must not apply to central egress services: `hololive-bot`, `hololive-alarm-worker`, `hololive-admin-api`, `admin-dashboard`, or `llm-scheduler`.

## Risk

`POSTGRES_SSLMODE_ALLOW_INSECURE=true` weakens normal Postgres TLS verification. The temporary justification is AP-to-central Postgres connectivity over a private Tailscale route while AP Postgres certificate SAN and server-name verification are being aligned.

## Guardrails

- AP compose rendering must keep Iris egress tokens out of all AP youtube-producer containers.
- The insecure flag must render only for AP youtube-producer services.
- AP preflight and completion checks must require runtime and persisted QUIC UDP socket buffer values.
- This exception expires unless renewed by a dated follow-up commit.

## Exit Criteria

Remove this exception when AP Postgres TLS can run with `verify-full` using a stable server name and mounted CA bundle.
