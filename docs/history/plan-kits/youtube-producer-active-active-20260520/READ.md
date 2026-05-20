# READ FIRST: YouTube Producer Active-Active

이 파일은 LLM 작업자가 phase 작업을 시작하기 전에 반드시 읽어야 하는 짧은 기준 문서입니다.

## 정본 런타임

현재 정본은 `youtube-producer`입니다.

| 과거 문서 용어 | 현재 코드 기준 |
|---|---|
| `youtube-scraper` | `youtube-producer` |
| `hololive-stream-ingester` YouTube runtime | 이 작업 범위에서는 retired |
| `YOUTUBE_SCRAPER_ACTIVE_ACTIVE_ENABLED` | `YOUTUBE_PRODUCER_ACTIVE_ACTIVE_ENABLED` |
| `YOUTUBE_SCRAPER_INSTANCE_ID` | `YOUTUBE_PRODUCER_INSTANCE_ID` |
| scraper AP | `youtube-producer-a`, `youtube-producer-b` |

## 작업 대상 경로

- Runtime module: `hololive/hololive-youtube-producer`
- Shared poller/scheduler: `hololive/hololive-shared/pkg/service/youtube/poller/internal/polling`
- Config loader: `hololive/hololive-shared/pkg/config/internal/settings`
- Osaka compose: `docker-compose.osaka.yml`
- Current service doc: `docs/current/services/youtube-producer.md`
- Current runbook: `docs/current/runbooks/youtube-producer.md`

## 이미 구현된 핵심

- `JobRunGuard` per-job lease/cooldown
- scheduler `JobClaimer` hook
- active-active에서 global `lock:ingestion:runtime` 우회
- Valkey-backed claimer 주입
- PendingPublishedAtResolver candidate claim
- PhotoSync singleton lease 코드
- readiness payload
- job claim/renew/complete/release metrics
- Osaka `youtube-producer-a`, `youtube-producer-b` compose

## 아직 운영 완료로 볼 수 없는 이유

- live Osaka smoke evidence가 아직 문서화되지 않았습니다.
- `/ready` 양쪽 payload의 실제 값이 기록되어야 합니다.
- active-active claim metrics가 실제로 관측되어야 합니다.
- 최근 outbox duplicate SQL 결과가 기록되어야 합니다.
- Valkey readiness는 현재 reactive라서 첫 job claim 전 순간에는 엄격하지 않을 수 있습니다.
- PhotoSync failover는 compose상 AP-A 전용입니다.
- 두 scheduler 동시 실행 regression test가 별도 보강되면 좋습니다.

## 금지사항

- retired `hololive-stream-ingester` YouTube runtime을 되살리지 마세요.
- `YOUTUBE_SCRAPER_*` env를 새로 추가하지 마세요.
- active-active 복구를 위해 JobRunGuard를 끄지 마세요.
- metric label에 `channel_id`를 추가하지 마세요.
- live deploy, restart, rollback, env modification, secret read/use/write, OpenBao KV write, authenticated metrics access는 명시 승인 없이 실행하지 마세요.
- 문서만 다루는 phase에서 코드/compose를 변경하지 마세요.

## 완료 표현 기준

fresh command output 없이 “완료”, “통과”, “운영 검증됨”이라고 쓰지 마세요.

가능한 표현:

- “코드상 구현 근거는 확인됨”
- “운영 검증은 아직 미실행”
- “명령 X를 실행했고 exit 0을 확인함”
- “live smoke는 승인/접근 권한이 없어 미실행”

## phase 실행 방식

한 번에 하나의 phase만 수행하세요. phase 파일의 safety/scope, allowed/not allowed changes, Stop Rules, Verification/Commands를 먼저 읽고, phase 밖 변경은 하지 마세요.
