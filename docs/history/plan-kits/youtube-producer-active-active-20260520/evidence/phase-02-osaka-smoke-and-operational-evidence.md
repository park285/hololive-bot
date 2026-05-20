# Phase 02 Evidence: Osaka Smoke And Operational Evidence

## Header

- Phase: 02 - Osaka Smoke And Operational Evidence
- Date/time: 2026-05-20T02:11:04Z
- Host: kapu; remote checked: kapu-iris-osaka-1
- Branch/commit: main / b1c65a400e473721171e87786c4ba34e26e77779
- Operator: Codex
- Live system touched: read-only inspection yes; mutation no
- Approval evidence if live mutation occurred: n/a

## Commands

```bash
./scripts/logs/osaka-smoke.sh
```

Exit code: 1

Important output:

```text
no such service: youtube-producer-a
```

```bash
curl -fsS http://127.0.0.1:30005/ready && printf '\n---\n' && curl -fsS http://127.0.0.1:30015/ready
```

Exit code: 7

Important output:

```text
curl: (7) Failed to connect to 127.0.0.1 port 30005 after 0 ms: Could not connect to server
```

```bash
sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f docker-compose.prod.yml -f docker-compose.osaka.yml ps youtube-producer-a youtube-producer-b
```

Exit code: 0

Important output:

```text
NAME      IMAGE     COMMAND   SERVICE   CREATED   STATUS    PORTS
```

```bash
ssh -F /dev/null -i ./KR.key -o IdentitiesOnly=yes -o BatchMode=yes ubuntu@kapu-iris-osaka-1 'set -eu; cd ~/hololive-bot; sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f docker-compose.prod.yml -f docker-compose.osaka.yml ps youtube-producer-a youtube-producer-b'
```

Exit code: 1

Important output:

```text
no such service: youtube-producer-a
```

```bash
ssh -F /dev/null -i ./KR.key -o IdentitiesOnly=yes -o BatchMode=yes ubuntu@kapu-iris-osaka-1 'set -eu; cd ~/hololive-bot; sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f docker-compose.prod.yml -f docker-compose.osaka.yml ps youtube-scraper-a youtube-scraper-b'
```

Exit code: 0

Important output:

```text
NAME                         IMAGE                           COMMAND                  SERVICE             CREATED       STATUS                 PORTS
hololive-youtube-scraper-a   hololive-youtube-scraper:prod   "./bin/youtube-scrap…"   youtube-scraper-a   3 hours ago   Up 3 hours (healthy)   127.0.0.1:30005->30005/tcp
hololive-youtube-scraper-b   hololive-youtube-scraper:prod   "./bin/youtube-scrap…"   youtube-scraper-b   3 hours ago   Up 3 hours (healthy)   30005/tcp, 127.0.0.1:30015->30015/tcp
```

```bash
ssh -F /dev/null -i ./KR.key -o IdentitiesOnly=yes -o BatchMode=yes ubuntu@kapu-iris-osaka-1 'set -eu; curl -fsS http://127.0.0.1:30005/ready; printf "\n---\n"; curl -fsS http://127.0.0.1:30015/ready'
```

Exit code: 0

Important output:

```json
{"active_active":true,"instance_id":"youtube-scraper-a","job_lease_enabled":true,"mode":"active-active","photo_sync_enabled":true,"runtime":"youtube-scraper","scraping_paused":false,"status":"ready","valkey_available":true}
---
{"active_active":true,"instance_id":"youtube-scraper-b","job_lease_enabled":true,"mode":"active-active","photo_sync_enabled":false,"runtime":"youtube-scraper","scraping_paused":false,"status":"ready","valkey_available":true}
```

```bash
ssh -F /dev/null -i ./KR.key -o IdentitiesOnly=yes -o BatchMode=yes ubuntu@kapu-iris-osaka-1 'set -eu; for port in 30005 30015; do echo "== metrics:$port =="; curl -fsS http://127.0.0.1:$port/metrics | grep -E "youtube_poller_job_claim_total|youtube_poller_job_lease_renew_total|youtube_poller_job_mark_completed_total|youtube_poller_job_release_total" | sed -n "1,80p"; done'
```

Exit code: 0

Important output:

```text
== metrics:30005 ==
curl: (22) The requested URL returned error: 401
== metrics:30015 ==
curl: (22) The requested URL returned error: 401
```

```bash
ssh -F /dev/null -i ./KR.key -o IdentitiesOnly=yes -o BatchMode=yes ubuntu@kapu-iris-osaka-1 'set -eu; SINCE=2026-05-19T22:47:21Z; for c in hololive-youtube-scraper-a hololive-youtube-scraper-b; do echo "== $c risk scan =="; docker logs --since "$SINCE" "$c" 2>&1 | grep -E "ERR|panic|permission denied|x509|no such file" || true; done'
```

Exit code: 0

Important output:

```text
== hololive-youtube-scraper-a risk scan ==
== hololive-youtube-scraper-b risk scan ==
```

## Checks

| Check | Result | Evidence |
|---|---|---|
| compose service names `youtube-producer-a/b` | fail | remote compose reports `no such service: youtube-producer-a` |
| `/ready` AP-A | fail | remote AP-A reports `runtime=youtube-scraper`, `instance_id=youtube-scraper-a` |
| `/ready` AP-B | fail | remote AP-B reports `runtime=youtube-scraper`, `instance_id=youtube-scraper-b` |
| metrics | blocked | `/metrics` returned 401; no API key or secret-read approval used |
| log scan | pass for observed scraper containers | filtered scan since container start emitted no high-risk markers |
| duplicate SQL | pass | central `holo-postgres` read-only query returned `(0 rows)` at 2026-05-20T02:36:30Z |
| live mutation guard | pass | no `up`, `restart`, `stop`, `rm`, rollback, deploy, env write, or secret write command was run |

## Findings

- Completed: Read-only Osaka connectivity, current AP readiness payloads, container status, and filtered log scan were collected.
- Blocked: Phase 02 cannot be completed because the live Osaka compose/runtime still uses `youtube-scraper-a/b` service, container, runtime, and instance names instead of the required `youtube-producer-a/b`.
- Blocked: metrics evidence requires authenticated access and was not attempted with secrets.
- Completed: recent `youtube_notification_outbox` duplicate SQL returned `(0 rows)` through central `holo-postgres` without printing secret values.
- Inconclusive: claim metrics are not proven.
- Follow-up: Deploy/recreate Osaka active-active `youtube-producer-a/b` with the approved wrapper, then rerun `osaka-smoke.sh`, authenticated metrics collection, and duplicate SQL read-only query.

## Recheck: 2026-05-20T02:25:20Z

```bash
date -u +%Y-%m-%dT%H:%M:%SZ && ./scripts/logs/osaka-smoke.sh
```

Exit code: 1

Important output:

```text
2026-05-20T02:25:20Z
no such service: youtube-producer-a
```

```bash
ssh -F /dev/null -i ./KR.key -o IdentitiesOnly=yes -o BatchMode=yes ubuntu@kapu-iris-osaka-1 'set -eu; date -u +%Y-%m-%dT%H:%M:%SZ; cd ~/hololive-bot; sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f docker-compose.prod.yml -f docker-compose.osaka.yml ps youtube-producer-a youtube-producer-b'
```

Exit code: 1

Important output:

```text
2026-05-20T02:25:21Z
no such service: youtube-producer-a
```

```bash
ssh -F /dev/null -i ./KR.key -o IdentitiesOnly=yes -o BatchMode=yes ubuntu@kapu-iris-osaka-1 'set -eu; date -u +%Y-%m-%dT%H:%M:%SZ; curl -fsS http://127.0.0.1:30005/ready; printf "\n---\n"; curl -fsS http://127.0.0.1:30015/ready'
```

Exit code: 0

Important output:

```json
{"active_active":true,"instance_id":"youtube-scraper-a","job_lease_enabled":true,"mode":"active-active","photo_sync_enabled":true,"runtime":"youtube-scraper","scraping_paused":false,"status":"ready","valkey_available":true}
---
{"active_active":true,"instance_id":"youtube-scraper-b","job_lease_enabled":true,"mode":"active-active","photo_sync_enabled":false,"runtime":"youtube-scraper","scraping_paused":false,"status":"ready","valkey_available":true}
```

```bash
ssh -F /dev/null -i ./KR.key -o IdentitiesOnly=yes -o BatchMode=yes ubuntu@kapu-iris-osaka-1 'set -eu; cd ~/hololive-bot; sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f docker-compose.prod.yml -f docker-compose.osaka.yml ps youtube-scraper-a youtube-scraper-b; docker inspect hololive-youtube-scraper-a hololive-youtube-scraper-b --format "{{.Name}} {{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{end}} {{.State.StartedAt}} {{.Config.Image}}"'
```

Exit code: 0

Important output:

```text
hololive-youtube-scraper-a   hololive-youtube-scraper:prod   youtube-scraper-a   Up 4 hours (healthy)
hololive-youtube-scraper-b   hololive-youtube-scraper:prod   youtube-scraper-b   Up 4 hours (healthy)
/hololive-youtube-scraper-a running healthy 2026-05-19T22:47:21.588581605Z hololive-youtube-scraper:prod
/hololive-youtube-scraper-b running healthy 2026-05-19T22:47:21.586372189Z hololive-youtube-scraper:prod
```

```bash
./scripts/deploy/osaka-active-active-deploy.sh --dry-run
```

Exit code: 0

Important output:

```text
[DRY-RUN] No remote files or containers changed.
```

Recheck conclusion: the blocker remains current. The deploy wrapper dry-run passed and changed no remote files or containers, but live mutation approval is still required before the Osaka `youtube-producer-a/b` runtime can be deployed and Phase 02 can be completed.

## Recheck: 2026-05-20T02:30:32Z

```bash
COMPOSE_ENV_FILE=/run/hololive-bot/env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f docker-compose.prod.yml -f docker-compose.osaka.yml config --quiet
```

Exit code: 0

Important output:

```text
No output; local target compose renders successfully.
```

```bash
COMPOSE_ENV_FILE=/run/hololive-bot/env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f docker-compose.prod.yml -f docker-compose.osaka.yml config --services | rg '^youtube-producer-(a|b)$|^youtube-scraper-(a|b)$'
```

Exit code: 0

Important output:

```text
youtube-producer-a
youtube-producer-b
```

```bash
sha256sum docker-compose.osaka.yml docker-compose.prod.yml
```

Exit code: 0

Important output:

```text
94bdb3007df966e846717de0217bba47ad972b1b5fddbceda1a5c4d0152c6df5  docker-compose.osaka.yml
ed1b11b8251f31e2c267faa35edcec9c630e876188fe6c615e75d7bcf61dcca8  docker-compose.prod.yml
```

```bash
ssh -F /dev/null -i ./KR.key -o IdentitiesOnly=yes -o BatchMode=yes ubuntu@kapu-iris-osaka-1 'set -eu; cd ~/hololive-bot; sha256sum docker-compose.osaka.yml docker-compose.prod.yml; printf "services="; sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f docker-compose.prod.yml -f docker-compose.osaka.yml config --services | grep -E "^youtube-(producer|scraper)-(a|b)$" | paste -sd, -'
```

Exit code: 0

Important output:

```text
95aa8eff1993bfd5efc03c26db4197bc895f3e21d10d2be8efded0ee2bcb44c2  docker-compose.osaka.yml
63d72d45e2651e2ea098e00089ede72512446b6471c03e7b2c514fcb121985a4  docker-compose.prod.yml
services=youtube-scraper-a,youtube-scraper-b
```

```bash
ssh -F /dev/null -i ./KR.key -o IdentitiesOnly=yes -o BatchMode=yes ubuntu@kapu-iris-osaka-1 'set -eu; sudo -n sh -c "cut -d= -f1 /run/hololive-bot/env | sort | grep -E \"^(YOUTUBE_PRODUCER_ACTIVE_ACTIVE_ENABLED|YOUTUBE_PRODUCER_INSTANCE_ID|YOUTUBE_PRODUCER_LEASE_NAMESPACE|YOUTUBE_PRODUCER_RUNTIME_ALLOWED|YOUTUBE_SCRAPER_ACTIVE_ACTIVE_ENABLED|YOUTUBE_SCRAPER_INSTANCE_ID)$\" || true"'
```

Exit code: 0

Important output:

```text
No matching key names were printed.
```

Recheck conclusion: the local target compose is ready to render `youtube-producer-a/b`, while the remote Osaka compose files differ by hash and still render `youtube-scraper-a/b`. At this point the blocker narrowed to remote file sync/deploy not yet applied, plus authenticated metrics and DB evidence access gaps.

## Recheck: 2026-05-20T02:36:30Z

```bash
docker exec -u postgres holo-postgres psql -d hololive -v ON_ERROR_STOP=1 -c "SELECT kind, content_id, COUNT(*) FROM youtube_notification_outbox WHERE created_at > NOW() - INTERVAL '30 minutes' GROUP BY kind, content_id HAVING COUNT(*) > 1;"
```

Exit code: 2

Important output:

```text
psql: error: connection to server on socket "/var/run/postgresql/.s.PGSQL.5433" failed: FATAL:  role "postgres" does not exist
```

```bash
docker exec holo-postgres psql -h 127.0.0.1 -p 5433 -U hololive_runtime -d hololive -v ON_ERROR_STOP=1 -c "SELECT kind, content_id, COUNT(*) FROM youtube_notification_outbox WHERE created_at > NOW() - INTERVAL '30 minutes' GROUP BY kind, content_id HAVING COUNT(*) > 1;"
```

Exit code: 0

Important output:

```text
 kind | content_id | count
------+------------+-------
(0 rows)
```

Recheck conclusion: the duplicate SQL requirement is now satisfied for the latest 30-minute window. Phase 02 remains blocked because live Osaka still has not deployed `youtube-producer-a/b` and authenticated claim metrics remain unavailable without approved metrics credentials.

## Recheck: 2026-05-20T02:50:50Z

```bash
date -u +%Y-%m-%dT%H:%M:%SZ
```

Exit code: 0

Important output:

```text
2026-05-20T02:50:50Z
```

```bash
./scripts/logs/osaka-smoke.sh
```

Exit code: 1

Important output:

```text
no such service: youtube-producer-a
```

```bash
COMPOSE_ENV_FILE=/run/hololive-bot/env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f docker-compose.prod.yml -f docker-compose.osaka.yml config --services | rg '^youtube-(producer|scraper)-(a|b)$'
```

Exit code: 0

Important output:

```text
youtube-producer-a
youtube-producer-b
```

```bash
ssh -F /dev/null -i ./KR.key -o IdentitiesOnly=yes -o BatchMode=yes ubuntu@kapu-iris-osaka-1 'set -eu; date -u +%Y-%m-%dT%H:%M:%SZ; cd ~/hololive-bot; sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f docker-compose.prod.yml -f docker-compose.osaka.yml config --services | grep -E "^youtube-(producer|scraper)-(a|b)$" | paste -sd, -'
```

Exit code: 0

Important output:

```text
2026-05-20T02:50:51Z
youtube-scraper-b,youtube-scraper-a
```

```bash
ssh -F /dev/null -i ./KR.key -o IdentitiesOnly=yes -o BatchMode=yes ubuntu@kapu-iris-osaka-1 'set -eu; curl -fsS http://127.0.0.1:30005/ready; printf "\n---\n"; curl -fsS http://127.0.0.1:30015/ready'
```

Exit code: 0

Important output:

```json
{"active_active":true,"goroutines":54,"http_server_started":true,"instance_id":"youtube-scraper-a","job_lease_enabled":true,"mode":"active-active","photo_sync_enabled":true,"runtime":"youtube-scraper","scraping_paused":false,"shutting_down":false,"status":"ready","uptime":"4h3m47s","valkey_available":true,"version":"2.0.0","youtube_enabled":true}
---
{"active_active":true,"goroutines":53,"http_server_started":true,"instance_id":"youtube-scraper-b","job_lease_enabled":true,"mode":"active-active","photo_sync_enabled":false,"runtime":"youtube-scraper","scraping_paused":false,"shutting_down":false,"status":"ready","uptime":"4h3m47s","valkey_available":true,"version":"2.0.0","youtube_enabled":true}
```

```bash
ssh -F /dev/null -i ./KR.key -o IdentitiesOnly=yes -o BatchMode=yes ubuntu@kapu-iris-osaka-1 'set -eu; cd ~/hololive-bot; sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f docker-compose.prod.yml -f docker-compose.osaka.yml ps youtube-scraper-a youtube-scraper-b; docker inspect hololive-youtube-scraper-a hololive-youtube-scraper-b --format "{{.Name}} {{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{end}} {{.State.StartedAt}} {{.Config.Image}}"'
```

Exit code: 0

Important output:

```text
hololive-youtube-scraper-a   hololive-youtube-scraper:prod   youtube-scraper-a   Up 4 hours (healthy)
hololive-youtube-scraper-b   hololive-youtube-scraper:prod   youtube-scraper-b   Up 4 hours (healthy)
/hololive-youtube-scraper-a running healthy 2026-05-19T22:47:21.588581605Z hololive-youtube-scraper:prod
/hololive-youtube-scraper-b running healthy 2026-05-19T22:47:21.586372189Z hololive-youtube-scraper:prod
```

```bash
ssh -F /dev/null -i ./KR.key -o IdentitiesOnly=yes -o BatchMode=yes ubuntu@kapu-iris-osaka-1 'set -eu; for port in 30005 30015; do printf "metrics:%s " "$port"; curl -fsS -o /dev/null -w "%{http_code}\n" http://127.0.0.1:$port/metrics || true; done'
```

Exit code: 0

Important output:

```text
metrics:30005 401
metrics:30015 401
```

```bash
docker exec holo-postgres psql -h 127.0.0.1 -p 5433 -U hololive_runtime -d hololive -v ON_ERROR_STOP=1 -c "SELECT kind, content_id, COUNT(*) FROM youtube_notification_outbox WHERE created_at > NOW() - INTERVAL '30 minutes' GROUP BY kind, content_id HAVING COUNT(*) > 1;"
```

Exit code: 0

Important output:

```text
 kind | content_id | count
------+------------+-------
(0 rows)
```

```bash
./scripts/deploy/osaka-active-active-deploy.sh --dry-run
```

Exit code: 0

Important output:

```text
[DRY-RUN] No remote files or containers changed.
```

Recheck conclusion: the blocker remains current. The local target compose still renders `youtube-producer-a/b`, but live Osaka still renders and runs `youtube-scraper-a/b`; unauthenticated metrics still return 401; the latest duplicate SQL read returned `(0 rows)`. No live mutation was performed.

## Recheck: 2026-05-20T02:57:04Z

```bash
date -u +%Y-%m-%dT%H:%M:%SZ
```

Exit code: 0

Important output:

```text
2026-05-20T02:57:04Z
```

```bash
./scripts/logs/osaka-smoke.sh
```

Exit code: 1

Important output:

```text
no such service: youtube-producer-a
```

```bash
COMPOSE_ENV_FILE=/run/hololive-bot/env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f docker-compose.prod.yml -f docker-compose.osaka.yml config --services | rg '^youtube-(producer|scraper)-(a|b)$'
```

Exit code: 0

Important output:

```text
youtube-producer-a
youtube-producer-b
```

```bash
ssh -F /dev/null -i ./KR.key -o IdentitiesOnly=yes -o BatchMode=yes ubuntu@kapu-iris-osaka-1 'set -eu; date -u +%Y-%m-%dT%H:%M:%SZ; cd ~/hololive-bot; sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f docker-compose.prod.yml -f docker-compose.osaka.yml config --services | grep -E "^youtube-(producer|scraper)-(a|b)$" | paste -sd, -'
```

Exit code: 0

Important output:

```text
2026-05-20T02:57:05Z
youtube-scraper-a,youtube-scraper-b
```

```bash
ssh -F /dev/null -i ./KR.key -o IdentitiesOnly=yes -o BatchMode=yes ubuntu@kapu-iris-osaka-1 'set -eu; curl -fsS http://127.0.0.1:30005/ready; printf "\n---\n"; curl -fsS http://127.0.0.1:30015/ready'
```

Exit code: 0

Important output:

```json
{"active_active":true,"goroutines":55,"http_server_started":true,"instance_id":"youtube-scraper-a","job_lease_enabled":true,"mode":"active-active","photo_sync_enabled":true,"runtime":"youtube-scraper","scraping_paused":false,"shutting_down":false,"status":"ready","uptime":"4h9m58s","valkey_available":true,"version":"2.0.0","youtube_enabled":true}
---
{"active_active":true,"goroutines":53,"http_server_started":true,"instance_id":"youtube-scraper-b","job_lease_enabled":true,"mode":"active-active","photo_sync_enabled":false,"runtime":"youtube-scraper","scraping_paused":false,"shutting_down":false,"status":"ready","uptime":"4h9m58s","valkey_available":true,"version":"2.0.0","youtube_enabled":true}
```

```bash
ssh -F /dev/null -i ./KR.key -o IdentitiesOnly=yes -o BatchMode=yes ubuntu@kapu-iris-osaka-1 'set -eu; for port in 30005 30015; do code=$(curl -sS -o /dev/null -w "%{http_code}" http://127.0.0.1:$port/metrics); printf "metrics:%s %s\n" "$port" "$code"; done'
```

Exit code: 0

Important output:

```text
metrics:30005 401
metrics:30015 401
```

```bash
docker exec holo-postgres psql -h 127.0.0.1 -p 5433 -U hololive_runtime -d hololive -v ON_ERROR_STOP=1 -c "SELECT kind, content_id, COUNT(*) FROM youtube_notification_outbox WHERE created_at > NOW() - INTERVAL '30 minutes' GROUP BY kind, content_id HAVING COUNT(*) > 1;"
```

Exit code: 0

Important output:

```text
 kind | content_id | count
------+------------+-------
(0 rows)
```

```bash
./scripts/deploy/osaka-active-active-deploy.sh --dry-run
```

Exit code: 0

Important output:

```text
[DRY-RUN] No remote files or containers changed.
```

Recheck conclusion: the blocker remains current. The local target compose still renders `youtube-producer-a/b`, but live Osaka still renders and runs `youtube-scraper-a/b`; unauthenticated metrics still return 401; the latest duplicate SQL read returned `(0 rows)`. No live mutation was performed.

## Recheck: 2026-05-20T03:29:22Z

This recheck used only direct read-only commands that do not read or use `/run/hololive-bot/env`, `COMPOSE_ENV_FILE`, `--env-file`, `osaka-smoke.sh`, or compose wrappers. No live mutation, secret read/use/write, authenticated metrics access, or env-file access was performed.

```bash
date -u +%Y-%m-%dT%H:%M:%SZ
```

Exit code: 0

Important output:

```text
2026-05-20T03:29:22Z
```

```bash
ssh -F /dev/null -i ./KR.key -o IdentitiesOnly=yes -o BatchMode=yes ubuntu@kapu-iris-osaka-1 'set -eu; date -u +%Y-%m-%dT%H:%M:%SZ; docker ps --format "{{.Names}} {{.Image}} {{.Status}}" | grep -E "hololive-youtube-(producer|scraper)-[ab]" || true'
```

Exit code: 0

Important output:

```text
2026-05-20T03:29:24Z
hololive-youtube-scraper-b hololive-youtube-scraper:prod Up 5 hours (healthy)
hololive-youtube-scraper-a hololive-youtube-scraper:prod Up 5 hours (healthy)
```

```bash
ssh -F /dev/null -i ./KR.key -o IdentitiesOnly=yes -o BatchMode=yes ubuntu@kapu-iris-osaka-1 'set -eu; docker inspect hololive-youtube-scraper-a hololive-youtube-scraper-b --format "{{.Name}} {{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{end}} {{.State.StartedAt}} {{.Config.Image}}"'
```

Exit code: 0

Important output:

```text
/hololive-youtube-scraper-a running healthy 2026-05-19T22:47:21.588581605Z hololive-youtube-scraper:prod
/hololive-youtube-scraper-b running healthy 2026-05-19T22:47:21.586372189Z hololive-youtube-scraper:prod
```

```bash
ssh -F /dev/null -i ./KR.key -o IdentitiesOnly=yes -o BatchMode=yes ubuntu@kapu-iris-osaka-1 'set -eu; curl -fsS http://127.0.0.1:30005/ready; printf "\n---\n"; curl -fsS http://127.0.0.1:30015/ready'
```

Exit code: 0

Important output:

```json
{"active_active":true,"goroutines":55,"http_server_started":true,"instance_id":"youtube-scraper-a","job_lease_enabled":true,"mode":"active-active","photo_sync_enabled":true,"runtime":"youtube-scraper","scraping_paused":false,"shutting_down":false,"status":"ready","uptime":"4h42m2s","valkey_available":true,"version":"2.0.0","youtube_enabled":true}
---
{"active_active":true,"goroutines":53,"http_server_started":true,"instance_id":"youtube-scraper-b","job_lease_enabled":true,"mode":"active-active","photo_sync_enabled":false,"runtime":"youtube-scraper","scraping_paused":false,"shutting_down":false,"status":"ready","uptime":"4h42m2s","valkey_available":true,"version":"2.0.0","youtube_enabled":true}
```

```bash
ssh -F /dev/null -i ./KR.key -o IdentitiesOnly=yes -o BatchMode=yes ubuntu@kapu-iris-osaka-1 'set -eu; for port in 30005 30015; do code=$(curl -sS -o /dev/null -w "%{http_code}" http://127.0.0.1:$port/metrics); printf "metrics:%s %s\n" "$port" "$code"; done'
```

Exit code: 0

Important output:

```text
metrics:30005 401
metrics:30015 401
```

```bash
ssh -F /dev/null -i ./KR.key -o IdentitiesOnly=yes -o BatchMode=yes ubuntu@kapu-iris-osaka-1 'set -eu; SINCE=2026-05-20T03:29:22Z; for c in hololive-youtube-scraper-a hololive-youtube-scraper-b; do echo "== $c risk scan =="; docker logs --since "$SINCE" "$c" 2>&1 | grep -E "ERR|panic|permission denied|x509|no such file" || true; done'
```

Exit code: 0

Important output:

```text
== hololive-youtube-scraper-a risk scan ==
== hololive-youtube-scraper-b risk scan ==
```

```bash
docker exec holo-postgres psql -h 127.0.0.1 -p 5433 -U hololive_runtime -d hololive -v ON_ERROR_STOP=1 -c "SELECT kind, content_id, COUNT(*) FROM youtube_notification_outbox WHERE created_at > NOW() - INTERVAL '30 minutes' GROUP BY kind, content_id HAVING COUNT(*) > 1;"
```

Exit code: 0

Important output:

```text
 kind | content_id | count
------+------------+-------
(0 rows)
```

Recheck conclusion: the blocker remains current. The latest safe direct evidence still shows live Osaka running `hololive-youtube-scraper-a/b`, `/ready` still reports `runtime="youtube-scraper"` and `instance_id="youtube-scraper-a/b"`, unauthenticated metrics still return 401, and duplicate SQL still returns `(0 rows)`. Phase 02 remains blocked until an operator explicitly approves the live rollout, any required `/run/hololive-bot/env` use, and authenticated metrics access if credentials are required.

## Recheck: 2026-05-20T03:55:56Z

This recheck used only direct read-only commands that do not read or use `/run/hololive-bot/env`, `COMPOSE_ENV_FILE`, `--env-file`, `osaka-smoke.sh`, or compose wrappers. No live mutation, secret read/use/write, authenticated metrics access, or env-file access was performed.

```bash
date -u +%Y-%m-%dT%H:%M:%SZ
```

Exit code: 0

Important output:

```text
2026-05-20T03:55:56Z
```

```bash
ssh -F /dev/null -i ./KR.key -o IdentitiesOnly=yes -o BatchMode=yes ubuntu@kapu-iris-osaka-1 'set -eu
 date -u +%Y-%m-%dT%H:%M:%SZ
 docker ps --format "{{.Names}} {{.Image}} {{.Status}}" | grep -E "hololive-youtube-(producer|scraper)-[ab]" || true'
```

Exit code: 0

Important output:

```text
2026-05-20T03:56:23Z
hololive-youtube-scraper-b hololive-youtube-scraper:prod Up 5 hours (healthy)
hololive-youtube-scraper-a hololive-youtube-scraper:prod Up 5 hours (healthy)
```

```bash
ssh -F /dev/null -i ./KR.key -o IdentitiesOnly=yes -o BatchMode=yes ubuntu@kapu-iris-osaka-1 'set -eu
 docker inspect hololive-youtube-scraper-a hololive-youtube-scraper-b --format "{{.Name}} {{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{end}} {{.State.StartedAt}} {{.Config.Image}}"'
```

Exit code: 0

Important output:

```text
/hololive-youtube-scraper-a running healthy 2026-05-19T22:47:21.588581605Z hololive-youtube-scraper:prod
/hololive-youtube-scraper-b running healthy 2026-05-19T22:47:21.586372189Z hololive-youtube-scraper:prod
```

```bash
ssh -F /dev/null -i ./KR.key -o IdentitiesOnly=yes -o BatchMode=yes ubuntu@kapu-iris-osaka-1 'set -eu
 curl -fsS http://127.0.0.1:30005/ready
 printf "\n---\n"
 curl -fsS http://127.0.0.1:30015/ready'
```

Exit code: 0

Important output:

```json
{"active_active":true,"goroutines":54,"http_server_started":true,"instance_id":"youtube-scraper-a","job_lease_enabled":true,"mode":"active-active","photo_sync_enabled":true,"runtime":"youtube-scraper","scraping_paused":false,"shutting_down":false,"status":"ready","uptime":"5h9m2s","valkey_available":true,"version":"2.0.0","youtube_enabled":true}
---
{"active_active":true,"goroutines":53,"http_server_started":true,"instance_id":"youtube-scraper-b","job_lease_enabled":true,"mode":"active-active","photo_sync_enabled":false,"runtime":"youtube-scraper","scraping_paused":false,"shutting_down":false,"status":"ready","uptime":"5h9m2s","valkey_available":true,"version":"2.0.0","youtube_enabled":true}
```

```bash
ssh -F /dev/null -i ./KR.key -o IdentitiesOnly=yes -o BatchMode=yes ubuntu@kapu-iris-osaka-1 'set -eu
 for port in 30005 30015; do code=$(curl -sS -o /dev/null -w "%{http_code}" http://127.0.0.1:$port/metrics); printf "metrics:%s %s\n" "$port" "$code"; done'
```

Exit code: 0

Important output:

```text
metrics:30005 401
metrics:30015 401
```

```bash
ssh -F /dev/null -i ./KR.key -o IdentitiesOnly=yes -o BatchMode=yes ubuntu@kapu-iris-osaka-1 'set -eu
 SINCE=2026-05-20T03:55:56Z
 for c in hololive-youtube-scraper-a hololive-youtube-scraper-b; do printf "%s\n" "risk_scan:$c"; docker logs --since "$SINCE" "$c" 2>&1 | grep -E "ERR|panic|permission denied|x509|no such file" || true; done'
```

Exit code: 0

Important output:

```text
risk_scan:hololive-youtube-scraper-a
risk_scan:hololive-youtube-scraper-b
```

```bash
docker exec holo-postgres psql -h 127.0.0.1 -p 5433 -U hololive_runtime -d hololive -v ON_ERROR_STOP=1 -c "SELECT kind, content_id, COUNT(*) FROM youtube_notification_outbox WHERE created_at > NOW() - INTERVAL '30 minutes' GROUP BY kind, content_id HAVING COUNT(*) > 1;"
```

Exit code: 0

Important output:

```text
 kind | content_id | count
------+------------+-------
(0 rows)
```

Recheck conclusion: the blocker remains current. The latest safe direct evidence still shows live Osaka running `hololive-youtube-scraper-a/b`, `/ready` still reports `runtime="youtube-scraper"` and `instance_id="youtube-scraper-a/b"`, unauthenticated metrics still return 401, filtered logs show no high-risk markers for the observed scraper containers since the recheck timestamp, and duplicate SQL still returns `(0 rows)`. Phase 02 remains blocked until an operator explicitly approves the live rollout, any required `/run/hololive-bot/env` use, and authenticated metrics access if credentials are required.

## Required Approval To Unblock

Phase 02 cannot be completed with read-only work alone. To proceed, an operator must explicitly approve the live mutation, any required `/run/hololive-bot/env` use by deploy/smoke/completion scripts, and post-change authenticated metrics access.

Required deploy approval text must include:

```text
I approve running on host kapu-iris-osaka-1:
I_APPROVE_OSAKA_ACTIVE_ACTIVE_DEPLOY=true ./scripts/deploy/osaka-active-active-deploy.sh --apply

Approved service scope: youtube-producer-a youtube-producer-b only.
Approved effects: rsync the scoped active-active files, build youtube-producer-a/b, run docker compose up -d --no-deps --force-recreate --remove-orphans for youtube-producer-a/b.
Expected impact: replace the current Osaka youtube-scraper-a/b active-active AP runtime with youtube-producer-a/b; brief readiness disruption is acceptable during recreate.
Rollback point: wrapper backup directory backups/osaka-active-active-<timestamp>/ plus prechange container snapshot generated by the script.
Approved sensitive access: the approved deploy/smoke/completion scripts may use `/run/hololive-bot/env` only as needed for compose parsing and post-rollout verification; secret values must not be printed.
```

Required post-change evidence after deploy:

- `./scripts/logs/osaka-smoke.sh` exits 0.
- `CHANGE_STARTED_AT=<script output timestamp> ./scripts/deploy/osaka-active-active-completion-check.sh` exits 0.
- both AP `/ready` payloads report `mode=active-active`, `job_lease_enabled=true`, `valkey_available=true`, and `scraping_paused=false`.
- container names are `hololive-youtube-producer-a` and `hololive-youtube-producer-b`, both healthy and started after `CHANGE_STARTED_AT`.
- filtered logs since `CHANGE_STARTED_AT` contain no `ERR`, `panic`, `permission denied`, `x509`, or `no such file` markers.
- authenticated metrics show claim activity for `acquired`, `peer_owned`, and `already_completed`, plus `mark_completed` success; renew/release series are recorded if emitted, and absence during a healthy window is documented as a metrics caveat.
- duplicate SQL is rerun for the latest 30-minute window and returns `(0 rows)`.

Separate authenticated metrics approval is required if the metric query needs reading or using an API key, auth token, or other secret-backed credential. Secret values must not be printed.

Retrospective safety note: older evidence in this file includes compose and helper-script checks that used `/run/hololive-bot/env` or `COMPOSE_ENV_FILE` before the Phase 02 safety boundary was tightened. Do not repeat those checks without explicit approval for that sensitive access; use direct `/ready`, unauthenticated `/metrics`, `docker ps`, `docker inspect`, and filtered `docker logs` metadata reads when approval is missing.

## Completion Claim

Earlier conclusion is superseded by the approved rollout and post-rollout evidence below.

## Approved Rollout: 2026-05-20T04:38:39Z

Approval is recorded in `evidence/osaka-active-active-deploy-approval-and-result-20260520.md`. Scope was limited to `youtube-producer-a` and `youtube-producer-b` on `kapu-iris-osaka-1`, with brief readiness disruption accepted and secret values kept redacted.

```bash
I_APPROVE_OSAKA_ACTIVE_ACTIVE_DEPLOY=true ./scripts/deploy/osaka-active-active-deploy.sh --apply
```

Exit code: 0

Important output:

```text
backup_dir=backups/osaka-active-active-20260520T043836Z
change_started_at=2026-05-20T04:38:39Z
Container hololive-youtube-producer-a  Started
Container hololive-youtube-producer-b  Started
active-active completion check passed
```

## Post-Rollout Recheck: 2026-05-20T04:43:29Z

```bash
CHANGE_STARTED_AT=2026-05-20T04:38:39Z ./scripts/deploy/osaka-active-active-completion-check.sh
```

Exit code: 0

Important output:

```text
hololive-youtube-producer-a   youtube-producer-a   Up About a minute (healthy)
hololive-youtube-producer-b   youtube-producer-b   Up About a minute (healthy)
active-active completion check passed
```

```bash
./scripts/logs/osaka-smoke.sh
```

Exit code: 0

Important output:

```text
external smoke skipped (set OSAKA_SMOKE_EXTERNAL=true to run healthcheck --smoke)
/hololive-youtube-producer-a ["CMD","./bin/healthcheck","http://127.0.0.1:30005/health"] 1000:1000 hololive-youtube-producer:prod
/hololive-youtube-producer-b ["CMD","./bin/healthcheck","http://127.0.0.1:30015/health"] 1000:1000 hololive-youtube-producer:prod
```

```bash
docker ps --format "{{.Names}} {{.Image}} {{.Status}}" | grep -E "hololive-youtube-(producer|scraper)-?[ab]?"
```

Exit code: 0

Important output:

```text
hololive-youtube-producer-b hololive-youtube-producer:prod Up 2 minutes (healthy)
hololive-youtube-producer-a hololive-youtube-producer:prod Up 2 minutes (healthy)
```

```bash
docker inspect hololive-youtube-producer-a hololive-youtube-producer-b --format "{{.Name}} {{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{end}} started={{.State.StartedAt}} image={{.Config.Image}}"
```

Exit code: 0

Important output:

```text
/hololive-youtube-producer-a running healthy started=2026-05-20T04:39:13.540117631Z image=hololive-youtube-producer:prod
/hololive-youtube-producer-b running healthy started=2026-05-20T04:39:13.541887484Z image=hololive-youtube-producer:prod
```

```bash
curl -fsS http://127.0.0.1:30005/ready
curl -fsS http://127.0.0.1:30015/ready
```

Exit code: 0

Important output:

```json
{"active_active":true,"instance_id":"youtube-producer-a","job_lease_enabled":true,"mode":"active-active","photo_sync_enabled":true,"runtime":"youtube-producer","scraping_paused":false,"status":"ready","valkey_available":true}
{"active_active":true,"instance_id":"youtube-producer-b","job_lease_enabled":true,"mode":"active-active","photo_sync_enabled":false,"runtime":"youtube-producer","scraping_paused":false,"status":"ready","valkey_available":true}
```

```bash
docker logs --since 2026-05-20T04:38:39Z hololive-youtube-producer-a
docker logs --since 2026-05-20T04:38:39Z hololive-youtube-producer-b
```

Filtered result:

```text
no matching errors since 2026-05-20T04:38:39Z
```

Observed runtime signals:

```text
Cache store connected addr=100.100.1.3:6379
postgres_pool_connected host=100.100.1.3 port=5433
Photo sync service started runtime=youtube-producer
Photo sync completed service=photo_sync success=0 failed=0 total=44
```

Authenticated metrics access used `API_SECRET_KEY` from `/run/hololive-bot/env` only as an in-memory `X-API-Key` header. The key value was not printed.

Important metric output:

```text
port 30005: youtube_poller_job_claim_total{poller="live",result="acquired"} 7
port 30005: youtube_poller_job_claim_total{poller="live",result="already_completed"} 11
port 30005: youtube_poller_job_claim_total{poller="live",result="peer_owned"} 12
port 30005: youtube_poller_job_mark_completed_total{poller="live",result="success"} 7
port 30015: youtube_poller_job_claim_total{poller="live",result="acquired"} 12
port 30015: youtube_poller_job_claim_total{poller="live",result="already_completed"} 9
port 30015: youtube_poller_job_claim_total{poller="live",result="peer_owned"} 7
port 30015: youtube_poller_job_mark_completed_total{poller="live",result="success"} 12
```

`youtube_poller_job_lease_renew_total` and `youtube_poller_job_release_total` did not emit active series during the healthy post-rollout window. Code inspection shows release metrics are emitted on poll errors, while successful short jobs complete through `MarkCompleted`; absence of release series is recorded as no observed release path in this window.

```bash
docker exec holo-postgres psql -h 127.0.0.1 -p 5433 -U hololive_runtime -d hololive -v ON_ERROR_STOP=1 -c "SELECT kind, content_id, COUNT(*) AS duplicates FROM youtube_notification_outbox WHERE created_at > NOW() - INTERVAL '30 minutes' GROUP BY kind, content_id HAVING COUNT(*) > 1 ORDER BY duplicates DESC, kind, content_id LIMIT 20;"
```

Exit code: 0

Important output:

```text
 kind | content_id | duplicates
------+------------+------------
(0 rows)
```

## Updated Checks

| Check | Result | Evidence |
|---|---|---|
| approved live mutation scope | pass | approval named `kapu-iris-osaka-1`, exact apply command, `youtube-producer-a/b` scope, impact, rollback, `/run/hololive-bot/env`, and authenticated metrics access |
| deploy wrapper | pass | apply command exited 0; backup dir `backups/osaka-active-active-20260520T043836Z`; `change_started_at=2026-05-20T04:38:39Z` |
| compose service names `youtube-producer-a/b` | pass | completion check and smoke show both `youtube-producer-a/b` services |
| `/ready` AP-A | pass | `runtime=youtube-producer`, `instance_id=youtube-producer-a`, `mode=active-active`, `valkey_available=true`, `scraping_paused=false` |
| `/ready` AP-B | pass | `runtime=youtube-producer`, `instance_id=youtube-producer-b`, `mode=active-active`, `valkey_available=true`, `scraping_paused=false` |
| container freshness | pass | both producer containers started at `2026-05-20T04:39:13Z`, after `change_started_at` |
| metrics | pass with noted non-emitted series | authenticated metrics show `acquired`, `peer_owned`, `already_completed`, and `mark_completed` success on both APs; renew/release series did not emit in the healthy window |
| log scan | pass | filtered logs since `2026-05-20T04:38:39Z` show no high-risk markers |
| duplicate SQL | pass | central `holo-postgres` query returned `(0 rows)` |
| live mutation guard | pass | mutation stayed inside approved `youtube-producer-a/b` wrapper scope; no secret values were printed |

## Updated Completion Claim

Phase 02 is complete for the approved Osaka `youtube-producer-a/b` rollout evidence. Remaining note: `youtube_poller_job_lease_renew_total` and `youtube_poller_job_release_total` were not observed during the healthy post-rollout window, so they are documented as non-emitted rather than as active production series.

## Packaging Fix Redeploy Recheck: 2026-05-20T05:06:35Z

The deploy/build reviewer found that the first successful rollout could still rely on stale remote build-context files. The packaging was fixed and re-approved by reviewer:

- Docker build context is restricted by `hololive/hololive-youtube-producer/Dockerfile.dockerignore`.
- Docker build removes every copied source file not listed in `scripts/deploy/osaka-active-active-rsync-files.txt`.
- Clean-context Docker build passed.
- Injected stale broken Go and malformed JSON files were removed by the Dockerfile cleanup, and Docker build still passed.

```bash
I_APPROVE_OSAKA_ACTIVE_ACTIVE_DEPLOY=true ./scripts/deploy/osaka-active-active-deploy.sh --apply
```

Exit code: 0

Important output:

```text
backup_dir=backups/osaka-active-active-20260520T050530Z
change_started_at=2026-05-20T05:05:33Z
Container hololive-youtube-producer-a  Recreated
Container hololive-youtube-producer-b  Recreated
active-active completion check passed
```

```bash
CHANGE_STARTED_AT=2026-05-20T05:05:33Z ./scripts/deploy/osaka-active-active-completion-check.sh
./scripts/logs/osaka-smoke.sh
```

Exit code: 0 for both commands.

Important output:

```text
hololive-youtube-producer-a   youtube-producer-a   Up 30 seconds (healthy)
hololive-youtube-producer-b   youtube-producer-b   Up 30 seconds (healthy)
active-active completion check passed
external smoke skipped (set OSAKA_SMOKE_EXTERNAL=true to run healthcheck --smoke)
```

Latest direct verification:

```text
/hololive-youtube-producer-a running healthy started=2026-05-20T05:05:46.520198935Z image=hololive-youtube-producer:prod
/hololive-youtube-producer-b running healthy started=2026-05-20T05:05:46.521894548Z image=hololive-youtube-producer:prod
hololive-youtube-producer-a: no matching errors since 2026-05-20T05:05:33Z
hololive-youtube-producer-b: no matching errors since 2026-05-20T05:05:33Z
```

```json
{"active_active":true,"instance_id":"youtube-producer-a","job_lease_enabled":true,"mode":"active-active","photo_sync_enabled":true,"runtime":"youtube-producer","scraping_paused":false,"status":"ready","valkey_available":true}
{"active_active":true,"instance_id":"youtube-producer-b","job_lease_enabled":true,"mode":"active-active","photo_sync_enabled":false,"runtime":"youtube-producer","scraping_paused":false,"status":"ready","valkey_available":true}
```

Latest authenticated metrics:

```text
port 30005: youtube_poller_job_claim_total{poller="live",result="acquired"} 3
port 30005: youtube_poller_job_claim_total{poller="live",result="already_completed"} 5
port 30005: youtube_poller_job_claim_total{poller="live",result="peer_owned"} 1
port 30005: youtube_poller_job_mark_completed_total{poller="live",result="success"} 3
port 30015: youtube_poller_job_claim_total{poller="live",result="acquired"} 1
port 30015: youtube_poller_job_claim_total{poller="live",result="already_completed"} 5
port 30015: youtube_poller_job_claim_total{poller="live",result="peer_owned"} 3
port 30015: youtube_poller_job_mark_completed_total{poller="live",result="success"} 1
```

Latest duplicate SQL:

```text
 kind | content_id | duplicates
------+------------+------------
(0 rows)
```
