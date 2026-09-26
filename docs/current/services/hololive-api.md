# Service: hololive-api

## Runtime identity

| Field | Value |
|---|---|
| Module | `hololive-api` |
| Binary | `hololive-api` |
| Compose service | `hololive-api` |
| Port | `30001` (bot) / `30003` (llm) / `30006` (admin) |
| Health endpoint | `https://127.0.0.1:30001/health` through container `./bin/healthcheck` |
| Ready endpoint | `https://127.0.0.1:30003/internal/ready` through container `./bin/healthcheck --api-key-env API_SECRET_KEY` (llm plane dependencies) |

## Role

bot/admin/llm plane과 YouTube Community consume plane을 한 프로세스에서 호스팅하는 통합 runtime입니다.

- Bot plane: Kakao/Iris webhook ingress와 사용자 명령 routing, reply orchestration.
- LLM plane: major event/member news scheduling, LLM digest 생성, internal subscription/trigger 제공.
- Admin plane: dashboard-facing admin HTTP control plane, trigger client facade, alarm HTTP 호환 facade.

## Owns

- Kakao/Iris webhook ingress, user-facing command routing and reply orchestration (bot plane)
- Major event/member news subscription, digest generation, internal trigger endpoints, LLM summary cache and notification intent production (llm plane)
- Dashboard-facing admin HTTP API, operational trigger client facade, alarm HTTP compatibility facade during migration (admin plane)
- Bot-side clients for major event, member news, and alarm operations
- Observation claim/finalize, canonical persist, notification intent, live-end finalizer, and retention/replay (YouTube plane)
- `members.photo` Holodex PhotoSync product path (admin plane). YouTube channel photos are the `channel_photo` reducer.

## Provides

| Contract | Type | Path/Event/Queue | Consumers |
|---|---|---|---|
| Iris webhook boundary | external HTTP/H3 | webhook/reply/send | Iris / Redroid |
| membernews | HTTP JSON | `/internal/membernews/*` | `hololive-api` (bot plane) |
| majorevent | HTTP JSON | `/internal/majorevent/*` | `hololive-api` (bot plane) |
| trigger | HTTP JSON | `/internal/trigger/*` | `hololive-api` (admin plane) |
| Admin HTTP API | HTTP JSON | 검토 필요 | `admin-dashboard` |
| settings.update | Valkey Pub/Sub | `config:update` | `hololive-api`, `alarm-worker` |
| alarm HTTP compatibility | HTTP JSON | `/internal/alarm/*` | migration callers (target owner is `alarm-worker`) |

## Consumes

| Dependency | Purpose | Failure impact |
|---|---|---|
| PostgreSQL | command/domain/admin data, subscriptions, summaries, outbox | command/admin/scheduling failures, stale reads |
| Valkey | cache/config/session/coordination/PubSub | degraded command, admin, and cache behavior |
| Iris | KakaoTalk ingress/reply automation | webhook/reply delivery failure |
| cliproxy/LLM | external summary generation where configured | summary generation degradation |
| Alarm API | alarm CRUD/query | alarm commands and admin operations fail |

## Must not own

- Alarm checker/scheduler loops owned by `alarm-worker`
- Proactive alarm dispatch queue consumption owned by `alarm-worker`
- Proactive Iris/Kakao notification egress owned by `alarm-worker`

## Live query behavior

- `!라이브`는 기존 bot DB pool의 단일 snapshot으로 확정 방송과 수집 coverage를 읽습니다. 무인자는 우이를 포함한 활성 등록 Hololive 채널, 멤버 지정은 해석된 채널을 직접 조회합니다. freshness는 대상 주기의 두 배에 30초를 더하되 최대 5분이며 DB 조회 예산은 1초입니다. `DEC-20260926-hololive-live-query-read-model`과 [실행 계획](../plans/2026-09-26-live-query-read-model.md)이 소유합니다. 운영 migration·배포는 별도 승인과 coverage 확인 뒤 수행합니다.
- fresh positive만 표시하며 미수집·만료·종료 확인 중·저장 상태 불일치는 조회 미완료로 표시합니다. fresh 목록 0건을 방송 없음으로 바꾸지 않습니다. 전체 scope가 확인된 빈 결과만 방송 없음이며 표시 한도는 100개입니다. `!예정`·`!일정`과 Stream HTTP API는 기존 Holodex 원천과 조직·기간·5분 캐시·응답 계약을 유지합니다.
- Holodex live/upcoming 목록은 Service 인스턴스 안에서 같은 cache key의 미스를 조정합니다. owner가 조회·저장을 마치면 대기 caller가 캐시를 다시 읽습니다. caller의 취소/기한을 유지하고 결과 포인터나 오류를 공유하지 않습니다. cache write 실패·원천 실패·서로 다른 인스턴스의 요청까지 한 번으로 합친다는 보장은 없습니다.
- `DEC-20260926-youtube-only-stream-providers`에 따라 `!라이브`는 YouTube만 조회합니다. Chzzk·Twitch client/설정/DI와 명령 내 플랫폼 병합은 제거했습니다. YouTube 조회 실패는 기존 조회 실패 응답으로 전달합니다. 멤버 저장 데이터와 프로필의 정적 링크, Stream HTTP JSON 필드는 보존하며 해당 필드가 제공자 지원을 뜻하지는 않습니다.
- 새로운 자동 재시도·DB 실패 시 원천 전환·부분 결과 fallback은 추가하지 않습니다. 기존 org=all의 부분 캐시와 예약 retry 의미도 이번 cache-fill 개선에서는 변경하지 않습니다.

## Shorts observation processing

- 쇼츠 알림 초기화는 `SHORT` watermark와 저장된 canonical 영상이 소유합니다. 비어 있지 않은 유효 목록은 `PARTIAL / GAP_UNRESOLVED`여도 최초 기준 목록으로 저장하며 알리지 않습니다. 빈 부분 목록은 초기화하지 않고, 검증된 complete-empty 목록은 초기화합니다.
- 구버전이 빈 부분 목록에 남긴 `initialized=true`만으로는 기준 목록을 인정하지 않습니다. `SHORT` watermark의 `last_content_id`, 저장된 쇼츠, complete 근거가 모두 없으면 현재 관측 적용 전에 미초기화로 판단하고 첫 유효 목록을 알림 없이 저장합니다. 일반 영상으로 먼저 저장된 ID도 유효한 Shorts 기준 목록과 canonical 중복 판정에서 제외하지 않으며, 영상 종류를 강제로 변경하지 않습니다. 이미 저장된 목록이나 complete-empty 기준은 유지합니다.
- 초기화 이후 새 canonical video ID만 기존 `NEW_SHORT` outbox로 전달합니다. 이미 저장된 쇼츠는 알림 이력이 없어도 자동 backfill하지 않습니다. 기존 영상·watermark·전송 이력을 지우거나 가짜 `SENT`를 만들지 않습니다.
- `shorts_list` claim은 같은 채널의 더 앞선 `(scheduled_for, id)` 관측이 replay epoch 안에서 유효하고 `PENDING` 또는 `PROCESSING`이면 후속 관측을 선택하지 않습니다. 대기 중인 후속 관측의 attempt는 증가하지 않습니다. 다른 채널과 다른 kind는 이 순서 제약의 대상이 아니며, 기존 retry/lease recovery/dead-letter 정책은 유지합니다.
- 선행 관측 조회는 기존 queue partial index로 활성 Shorts 집합을 먼저 materialize합니다. 각 후보마다 완료된 관측 이력을 순회하지 않으며, custom/generic plan 모두 보존 이력 증가에 따른 조회 증폭을 회귀 검사합니다.
- 관측 순서 제약을 적용하는 첫 배포에서는 기존 YouTube consumer를 drain한 뒤 교체해야 합니다. 이미 구버전에서 claim된 작업까지 새 claim SQL이 재정렬하지는 않습니다. source replay epoch 변경이나 과거 관측 일괄 replay는 배포 절차에 포함하지 않습니다.
- 부분 목록은 삭제·비공개 근거가 아니며 `earliest_complete_effective_at`을 채우지 않습니다. 일반 영상과 Premiere의 기존 알림 정책은 변경하지 않습니다. 최초 목록 이전의 관측이나 수집 범위 밖의 영상까지 복구한다는 보장은 하지 않습니다.

## Startup requirements

- Iris URL/cert/token configuration
- PostgreSQL and Valkey availability
- Internal API base URLs and key configuration for scheduler, trigger, and alarm services
- CLIPROXY/LLM settings where enabled
- Uber Fx v1.24.0 is the process lifecycle owner for this binary only. It is an implementation detail, not an operator-selectable mode, and does not change ports, routes, config keys, or dependency readiness requirements.

## Shutdown behavior

- Fx is the single process signal owner. It starts the optional YouTube plane, then llm, admin, and bot; shutdown cancels the runtime context and drains bot, admin, llm, then the optional YouTube plane.
- Stop HTTP/H3 ingress and scheduler workers gracefully within the existing 10-second plane-drain budget. The whole Fx stop is capped at 30 seconds inside the Compose 45-second grace period.
- A runtime fatal, plane-drain failure, or process-stop timeout remains process-fatal. Cleanup is attempted once and no legacy lifecycle fallback is selected.
- Do not drain or mutate dispatch queues during shutdown.
- Preserve delivery/outbox state in PostgreSQL.

## Observability

- Logs: `./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs -f hololive-api`
- Health: `https://127.0.0.1:30001/health`, `https://127.0.0.1:30003/health`, `https://127.0.0.1:30006/health` through container `./bin/healthcheck`
- Ready: `https://127.0.0.1:30003/internal/ready` through container `./bin/healthcheck --api-key-env API_SECRET_KEY`
- Metrics: 검토 필요

## Related documents

- Project Map: `../PROJECT_MAP.md`
- Contract Map: `../CONTRACT_MAP.md`
- Process trust domain: `../architecture/hololive-api-trust-domain.md`
- Runbook: `../runbooks/hololive-api.md`
