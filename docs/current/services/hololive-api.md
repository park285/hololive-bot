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
| settings.update | HTTP JSON + `settings.json` | `POST /api/holo/settings`, `POST /api/holo/settings/llm` | `iris-console`, `alarm-worker` |
| alarm HTTP compatibility | HTTP JSON | `/internal/alarm/*` | migration callers (target owner is `alarm-worker`) |

## Consumes

| Dependency | Purpose | Failure impact |
|---|---|---|
| PostgreSQL | command/domain/admin data, subscriptions, summaries, outbox | command/admin/scheduling failures, stale reads |
| Valkey | cache/session/coordination/member epoch PubSub | degraded command, admin, and cache behavior |
| Iris | KakaoTalk ingress/reply automation | webhook/reply delivery failure |
| cliproxy/LLM | external summary generation where configured | summary generation degradation |
| Alarm API | alarm CRUD/query | alarm commands and admin operations fail |

## Must not own

- Alarm checker/scheduler loops owned by `alarm-worker`
- Proactive alarm dispatch queue consumption owned by `alarm-worker`
- Proactive Iris/Kakao notification egress owned by `alarm-worker`

## Live query behavior

- `!라이브`는 기존 bot DB pool의 단일 snapshot으로 확정 방송과 채널 확인 최신값을 읽으며 원천을 호출하지 않습니다. 무인자는 우이를 포함한 활성 등록 Hololive 채널, 멤버 지정은 해석된 채널을 직접 조회합니다. freshness는 `min(5분, 2×poll interval+30초)`, 기본 270초이며 DB 조회 예산은 1초입니다. `DEC-20260926-hololive-live-absence-evidence`와 [실행 계획](../plans/2026-09-26-live-absence-evidence.md)이 소유합니다.
- 공개·멤버 한정·최초공개의 fresh positive는 `/live`가 예정 영상이나 채널 페이지를 고르더라도 표시합니다. 방송 탭·Holodex 누락·absence slot은 채널 coverage가 아닙니다. 모든 채널의 신선한 음성 확인과 해소된 현재 후보가 갖춰진 빈 결과만 '현재 방송 중인 멤버가 없습니다.'로 안내합니다. 그 밖의 빈 결과는 '현재 방송 상태를 확인할 수 없습니다.'입니다. 멤버 지정 빈 결과의 `CMD_MEMBER_NOT_LIVE`와 100개 표시 한도는 유지합니다.
- 세션·head 없는 EXPLICIT_END와 session=ENDED는 완전성을 막지 않고 `live query incomplete`의 `nonblocking_diagnostics`로 남깁니다. LIVE/head 없음은 계속 차단합니다. 신선한 공개 불가 사실은 정상 head가 있는 stale LIVE 후보만 제외하며, 만료나 후속 UNKNOWN은 다시 차단합니다. pending을 삭제하거나 표시 전용 종료 상태를 만들지 않습니다. 사용자 응답에는 조회 사유·기준 시각·범위 설명을 붙이지 않습니다(`DEC-20260926-hololive-list-reply-fold-default`).
- YouTube plane은 `channel_live_check`를 최신값에만 저장하고 reducer로 보내지 않습니다. `video_live_check`의 identity가 맞고 유효한 upstream 종료 시각이 있을 때만 기존 명시적 종료 경로로 반영합니다. 공개 불가·해석 불가 UNKNOWN만으로 수명 상태를 바꾸지 않습니다. `!예정`·`!일정`과 Stream HTTP API는 기존 Holodex 원천·조직·기간·5분 캐시·응답 필드를 유지합니다. 단, `org=all` 부분 실패의 응답은 아래처럼 바뀌었습니다.
- Holodex live/upcoming 목록은 Service 인스턴스 안에서 같은 cache key의 미스를 조정합니다. owner가 조회·저장을 마치면 대기 caller가 캐시를 다시 읽습니다. caller의 취소/기한을 유지하고 결과 포인터나 오류를 공유하지 않습니다. cache write 실패·원천 실패·서로 다른 인스턴스의 요청까지 한 번으로 합친다는 보장은 없습니다.
- 원천 장애 때의 꼬리 지연(stack audit D10, PLN-20260926-stack-audit-refactoring T12 측정 기록, 동작 변경 없음): 실패는 캐시하지 않으므로 같은 key의 동시 miss는 한 명씩 원천을 다시 조회하고, k번째 caller는 앞선 k-1번의 실패 조회를 기다립니다. 2026-09-27 로컬 측정(`GetLiveStreams`, 같은 key caller 6명, MockRequester가 100ms 뒤 응답, miniredis, 3회 반복)에서 성공이면 원천 호출 1번에 6명 모두 100–120ms에 끝났고, 실패면 원천 호출 6번에 완료가 100·200·300·400·500·610ms로 늘어났습니다. caller 기한을 350ms로 두면 3명은 원천 오류로, 3명은 350ms에 `context.DeadlineExceeded`로 끝났습니다(원천 호출 4번, 4번째 조회는 기한으로 취소). 운영 값은 측정하지 않았습니다. 코드상으로는 한 번의 실패 조회가 Holodex client의 시도당 제한(기본 20초)과 재시도·backoff로 길어질 수 있고, 연속 3번 실패하면 circuit breaker가 열려(30초) 이후 대기 caller는 원천을 부르지 않고 바로 실패합니다. 대기 시간의 상한은 각 caller의 ctx 기한입니다.
- `DEC-20260926-youtube-only-stream-providers`에 따라 `!라이브`는 YouTube만 조회합니다. Chzzk·Twitch client/설정/DI와 명령 내 플랫폼 병합은 제거했습니다. YouTube 조회 실패는 기존 조회 실패 응답으로 전달합니다. 멤버 저장 데이터와 프로필의 정적 링크, Stream HTTP JSON 필드는 보존하며 해당 필드가 제공자 지원을 뜻하지는 않습니다.
- 새로운 자동 재시도·DB 실패 시 원천 전환·부분 결과 fallback은 추가하지 않습니다. 예약 retry 의미는 cache-fill 개선에서 변경하지 않았습니다.
- `org=all`에서 일부 org만 실패하면 provider는 성공한 org의 stream과 `PartialStreamsError`를 함께 돌려주고 캐시하지 않습니다(stack audit B1, PLN-20260926-stack-audit-refactoring T09). Stream HTTP API(`/api/holo/streams/live`, `/api/holo/streams/upcoming`)는 이 오류를 다른 원천 실패와 같이 500으로 응답하며 부분 목록을 내보내지 않습니다. 이전에는 부분 목록을 200으로 응답하고 캐시했습니다. 부분 목록과 실패 org를 함께 돌려주는 응답 필드는 공개 계약 변경이라 별도 결정 전에는 추가하지 않습니다. 소비자(iris-console admin-web의 streams 화면)는 한 org 장애 동안 `org=all` 조회 실패를 받습니다.

## Source fallback retirement

`DEC-20260926-hololive-source-fallbacks-retirement`(PLN-20260926-stack-audit-refactoring T19)에 따라 계약 없는 원천·표시 폴백을 오류 반환 단일 경로로 바꿨습니다. T18(2026-09-26) 30일 로그에서 아래 경로의 fallback·fail-open 경고는 0건이었습니다.

- Holodex `GetChannel`은 YouTube scraper로 부분 Channel을 만들지 않고, `GetChannels`는 목록 API 실패를 개별 조회로 보충하지 않으며, `GetChannelSchedule`은 YouTube·공식 일정으로 보충하거나 그 결과를 캐시하지 않습니다([공식 일정 계약](../contracts/schedule-hololive-tv-api.md)).
- 멤버 matcher의 부분 일치 결과는 roster(멤버 데이터·Valkey 동적 멤버) 이름을 채널명 정본으로 씁니다. Holodex 조회 보강과 그 실패 시 후보명·알림 멤버명 캐시로 채우던 체인은 없습니다. 정확 일치 결과와 같은 이름을 씁니다.
- major event 주간·월간 요약은 LLM 실패·빈 결과·외부 내용 guard 실패를 이벤트 목록만 보내는 대체 경로로 바꾸지 않습니다. 요약 실패는 스케줄러 오류이며 enqueue도 이벤트 표시도 하지 않습니다. 자동 재시도는 없습니다. 스케줄러는 `Failed to send weekly notification`·`Failed to send monthly notification` error 로그만 남기고 다음 실행을 일주일·한 달 뒤로 잡으며, 다음 실행은 새 주(KST 월–일)·새 달 키로 조회하므로 실패한 주기의 digest는 다시 보내지 않습니다. 복구는 같은 KST 주·달 안에 `/internal/trigger/majorevent-weekly`·`/internal/trigger/majorevent-monthly` 수동 trigger로만 합니다([trigger 계약](../contracts/trigger.md), [hololive-api runbook](../runbooks/hololive-api.md)). LLM이 설정되지 않은 모드(`llm == nil`)만 이벤트 목록을 보냅니다.
- `/api/holo` rate limit은 fail-closed입니다. rate limit이 켜져 있는데 cache가 없거나 limiter 초기화에 실패하면 admin API가 기동하지 않고, 판정 실패는 503으로 거절하며 `hololive_admin_rate_limit_check_failures_total`로 셉니다. `hololive_admin_rate_limit_fail_open_total`은 삭제했습니다.
- `domain.MemberDataProvider`는 오류를 돌려주는 `LoadAllMembers` 하나로 전체 멤버를 적재합니다. 오류를 흡수하던 `GetAllMembers`와 선택적 `MemberDataLoader`는 삭제했습니다. 공식 일정 식별 색인·멤버 목록 응답·alarm 콜라보 표시명은 멤버 적재 실패를 빈 결과로 바꾸지 않고 오류로 드러냅니다.
- 알림 멤버 표시명 폴백만 예외 계약으로 남습니다([alarm 계약](../contracts/alarm.md)의 멤버 표시명 예외 계약).

## Shorts observation processing

- 쇼츠 알림 초기화는 `SHORT` watermark와 저장된 canonical 영상이 소유합니다. 비어 있지 않은 유효 목록은 `PARTIAL / GAP_UNRESOLVED`여도 최초 기준 목록으로 저장하며 알리지 않습니다. 빈 부분 목록은 초기화하지 않고, 검증된 complete-empty 목록은 초기화합니다.
- 현재 writer는 빈 부분 목록만으로 초기화하지 않습니다. T18에서 구버전의 빈 부분 목록 초기화 잔여 행이 0건임을 확인한 뒤 해당 watermark를 미초기화로 되돌리던 분기와 보조 조회를 제거했습니다. 일반 영상으로 먼저 저장된 ID도 유효한 Shorts 기준 목록과 canonical 중복 판정에서 제외하지 않으며, 영상 종류를 강제로 변경하지 않습니다. 이미 저장된 목록이나 complete-empty 기준은 유지합니다.
- 초기화 이후 새 canonical video ID만 기존 `NEW_SHORT` outbox로 전달합니다. 이미 저장된 쇼츠는 알림 이력이 없어도 자동 backfill하지 않습니다. 기존 영상·watermark·전송 이력을 지우거나 가짜 `SENT`를 만들지 않습니다.
- `shorts_list` claim은 같은 채널의 더 앞선 `(scheduled_for, id)` 관측이 replay epoch 안에서 유효하고 `PENDING` 또는 `PROCESSING`이면 후속 관측을 선택하지 않습니다. 대기 중인 후속 관측의 attempt는 증가하지 않습니다. 다른 채널과 다른 kind는 이 순서 제약의 대상이 아니며, 기존 retry/lease recovery/dead-letter 정책은 유지합니다.
- 선행 관측 조회는 기존 queue partial index로 활성 Shorts 집합을 먼저 materialize합니다. 각 후보마다 완료된 관측 이력을 순회하지 않으며, custom/generic plan 모두 보존 이력 증가에 따른 조회 증폭을 회귀 검사합니다.
- 관측 순서 제약을 적용하는 첫 배포에서는 기존 YouTube consumer를 drain한 뒤 교체해야 합니다. 이미 구버전에서 claim된 작업까지 새 claim SQL이 재정렬하지는 않습니다. source replay epoch 변경이나 과거 관측 일괄 replay는 배포 절차에 포함하지 않습니다.
- 부분 목록은 삭제·비공개 근거가 아니며 `earliest_complete_effective_at`을 채우지 않습니다. 일반 영상과 Premiere의 기존 알림 정책은 변경하지 않습니다. 최초 목록 이전의 관측이나 수집 범위 밖의 영상까지 복구한다는 보장은 하지 않습니다.

## Observation storage and retention

- 채널 수치 통계·구독자 수 명령·통계 알림은 제거합니다. 채널 profile/photo, 방송·일정과 알림 구독은 유지합니다.
- 각 successful collection slot의 관측은 독립 저장합니다. payload만 kind/schema/canonical profile과 전체 32바이트 SHA-256으로 공유하며, JSONB의 PostgreSQL LZ4 압축을 사용합니다. identity와 외부 hex hash 계약은 바꾸지 않습니다.
- claim/replay는 같은 SQL에서 payload를 조회합니다. 참조 누락·kind/schema 불일치·hash 손상은 오류이며 구 저장 경로로 되돌아가지 않습니다. GC는 참조가 없는 payload만 잠금 후 새 snapshot으로 확인하여 제한된 수만 삭제합니다.
- application은 entity별 멱등·CANONICALIZED 결과를 그대로 저장하되 한 관측의 결과를 한 INSERT로 보냅니다. 유예는 원본 kind 기간 뒤 3일이며 orphan만 정리합니다. receipt 집약은 감사 조회 지연이 늘어 채택하지 않았습니다.
- RETIRED projection 정리는 reasons+targets 합계를 batch 상한 이하로 제한하며 CURRENT/STAGING, lease 참조 generation을 보호합니다. 호출 timeout은 8초입니다.
- 운영 cutover와 복구 경계는 [API runbook](../runbooks/hololive-api.md#youtube-관측-저장-구조-전환)을 따릅니다. 소스 기본값 변경은 운영 TTL 적용 완료를 뜻하지 않습니다.

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
