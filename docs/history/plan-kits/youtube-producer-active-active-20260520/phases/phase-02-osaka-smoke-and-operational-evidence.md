# Phase 02: Osaka Smoke And Operational Evidence

## Goal

Osaka `youtube-producer-a` and `youtube-producer-b`가 실제 active-active runtime으로 동작하는지 read-only 운영 증거를 수집합니다.

## Safety Boundary

This phase is read-only unless the user explicitly approves a deploy/restart/rollback separately.

Allowed:

- `ps`
- `/health`
- `/ready`
- logs read
- unauthenticated metrics read
- duplicate SQL read-only query
- Docker metadata reads that do not require `/run/hololive-bot/env`

Not allowed without explicit approval:

- `up`, `restart`, `stop`, `rm`, rollback, deploy
- secret read/use/write
- OpenBao KV write
- env modification
- authenticated metrics access or secret-backed metrics credential read/use
- commands or scripts that read/use `/run/hololive-bot/env`, including compose commands that pass it as `COMPOSE_ENV_FILE` or `--env-file`

## Commands

### Smoke script

Run this only after explicit approval for the live rollout and any required `/run/hololive-bot/env` use. Without that approval, record it as blocked and use the direct read-only checks below.

```bash
./scripts/logs/osaka-smoke.sh
```

### Direct readiness checks

```bash
curl -fsS http://127.0.0.1:30005/ready
curl -fsS http://127.0.0.1:30015/ready
```

Expected fields on both APs:

```json
{
  "mode": "active-active",
  "active_active": true,
  "job_lease_enabled": true,
  "valkey_available": true,
  "scraping_paused": false
}
```

### Container status

Without explicit approval to use `/run/hololive-bot/env`, prefer Docker metadata reads:

```bash
docker ps --filter 'name=hololive-youtube-producer-' --format '{{.Names}} {{.Image}} {{.Status}}'
docker inspect hololive-youtube-producer-a hololive-youtube-producer-b \
  --format '{{.Name}} {{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{end}} {{.State.StartedAt}} {{.Config.Image}}'
```

The compose-based command below reads/uses `/run/hololive-bot/env` through `COMPOSE_ENV_FILE`; run it only with explicit approval for that sensitive access. Without approval, record it as blocked.

```bash
sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/env COMPOSE_PROFILES=oracle \
  ./scripts/deploy/compose.sh -f docker-compose.prod.yml -f docker-compose.osaka.yml ps youtube-producer-a youtube-producer-b
```

### Completion check

Run this only after explicit approval for the live rollout and any required `/run/hololive-bot/env` use. Without that approval, record it as blocked.

```bash
CHANGE_STARTED_AT="${CHANGE_STARTED_AT:?set rollout UTC timestamp, for example 2026-05-20T00:00:00Z}" \
  ./scripts/deploy/osaka-active-active-completion-check.sh
```

### Log risk scan

```bash
SINCE="${CHANGE_STARTED_AT:?set rollout UTC timestamp, for example 2026-05-20T00:00:00Z}"
docker logs --since "$SINCE" hololive-youtube-producer-a 2>&1 | grep -E 'ERR|panic|permission denied|x509|no such file'
docker logs --since "$SINCE" hololive-youtube-producer-b 2>&1 | grep -E 'ERR|panic|permission denied|x509|no such file'
```

Expected: no output.

### Metrics evidence

Confirm these series exist after due poll jobs run:

Use only unauthenticated metrics access in this phase. If `/metrics` returns 401 or requires an API key, auth token, or other secret-backed credential, record that as a blocker instead of reading or using credentials.

```text
youtube_poller_job_claim_total{result="acquired"}
youtube_poller_job_claim_total{result="peer_owned"}
youtube_poller_job_claim_total{result="already_completed"}
youtube_poller_job_lease_renew_total
youtube_poller_job_mark_completed_total
youtube_poller_job_release_total
```

### Duplicate DB check

```sql
SELECT kind, content_id, COUNT(*)
FROM youtube_notification_outbox
WHERE created_at > NOW() - INTERVAL '30 minutes'
GROUP BY kind, content_id
HAVING COUNT(*) > 1;
```

Expected: `0 rows`.

## Stop Rules

Stop and report if:

- either `/ready` reports `mode=single-owner`
- either `/ready` reports `valkey_available=false`
- either `/ready` reports `scraping_paused=true`
- AP-A/AP-B use different lease namespace values
- duplicate SQL returns rows
- logs show repeated lease loss or Valkey unavailable errors

## Deliverable

Append an evidence record:

- timestamp
- host
- exact commands
- key output
- pass/fail status
- blockers

Use `appendix/evidence-template.md`.
