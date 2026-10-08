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
- Admin plane: dashboard-facing admin HTTP control plane, trigger client, worker alarm HTTP client.

## Owns

- Kakao/Iris webhook ingress, user-facing command routing and reply orchestration (bot plane)
- Major event/member news subscription, digest generation, internal trigger endpoints, LLM summary cache and notification intent production (llm plane)
- Dashboard-facing admin HTTP API and operational trigger/worker alarm clients (admin plane)
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

## Consumes

| Dependency | Purpose | Failure impact |
|---|---|---|
| PostgreSQL | command/domain/admin data, subscriptions, summaries, outbox | command/admin/scheduling failures, stale reads |
| Valkey | cache/session/coordination/member epoch PubSub | degraded command, admin, and cache behavior |
| Iris | KakaoTalk ingress/reply automation | webhook/reply delivery failure |
| cliproxy/LLM | external summary generation where configured | summary generation degradation |
| Alarm API | alarm CRUD/query | alarm commands and admin operations fail |

API 설정은 `internal/config`, plane별 공통 서비스 생성은 `internal/apifoundation`이 소유합니다.
Admin 조립·수명은 `internal/planes/admin/runtime`, router와 Stream/OAuth/WebSocket handler는
`internal/planes/admin/internal/httpapi`, 공통 trigger handler/router는 `internal/httpapi`에 있습니다.
Observation consume·canonical/replay/retention과 private reducer는 `internal/youtube/`가 소유합니다.
Bot·admin은 필수 `ALARM_INTERNAL_URL`의 worker provider를 사용하며 in-process alarm service를 생성하지 않습니다.

## Must not own

- Alarm checker/scheduler loops owned by `alarm-worker`
- Proactive alarm dispatch queue consumption owned by `alarm-worker`
- Proactive Iris/Kakao notification egress owned by `alarm-worker`

## Password reset API

- `POST /api/auth/password/reset-request`와 `POST /api/auth/password/reset` 경로는 유지하지만, 재설정 링크 전달 수단이 없어 HTTP 503과 기존 인증 오류 본문 `{"success":false,"error":"INTERNAL_ERROR"}`으로 응답합니다.
- 기존 IP 허용 목록 검사를 통과한 요청은 본문·계정 존재·토큰 유효성과 관계없이 같은 응답을 받습니다. 본문을 해석하거나 계정을 조회하지 않으며, 토큰 발급·소비, 비밀번호·세션 세대 변경 및 링크 발송 성공 응답은 하지 않습니다. 허용 목록 밖의 요청은 기존 403 차단을 유지합니다.
- 다시 지원하려면 링크 전달 수단과 복구 절차를 먼저 구현하고 공개 API 계약 변경 승인을 받아야 합니다.

## Live query behavior

- `!라이브`는 기존 bot DB pool의 단일 snapshot으로 확정 방송과 채널 확인 최신값을 읽으며 원천을 호출하지 않습니다. 무인자는 우이를 포함한 활성 등록 Hololive 채널, 멤버 지정은 해석된 채널을 직접 조회합니다. freshness는 `min(5분, 2×poll interval+30초)`, 기본 270초이며 DB 조회 예산은 1초입니다. `DEC-20260926-hololive-live-absence-evidence`와 [실행 계획](../plans/2026-09-26-live-absence-evidence.md)이 소유합니다.
- 공개·멤버 한정·최초공개의 fresh positive는 `/live`가 예정 영상이나 채널 페이지를 고르더라도 표시합니다. 방송 탭·Holodex 누락·absence slot은 채널 coverage가 아닙니다. 모든 채널의 신선한 음성 확인과 해소된 현재 후보가 갖춰진 빈 결과만 '현재 방송 중인 멤버가 없습니다.'로 안내합니다. 그 밖의 빈 결과는 '현재 방송 상태를 확인할 수 없습니다.'입니다. 멤버 지정 빈 결과의 `CMD_MEMBER_NOT_LIVE`와 100개 표시 한도는 유지합니다.
- 세션·head 없는 EXPLICIT_END와 session=ENDED는 완전성을 막지 않고 `live query incomplete`의 `nonblocking_diagnostics`로 남깁니다. LIVE/head 없음은 계속 차단합니다. 신선한 공개 불가 사실과 익명 수집에서 비공개·삭제 영상이 남기는 신선한 `identity_missing` UNKNOWN은 정상 head가 있는 stale LIVE 후보만 제외하며, 만료나 그 밖의 UNKNOWN 사유는 다시 차단합니다. pending을 삭제하거나 표시 전용 종료 상태를 만들지 않습니다. 사용자 응답에는 조회 사유·기준 시각·범위 설명을 붙이지 않습니다(`DEC-20260926-hololive-list-reply-fold-default`).
- YouTube plane은 `channel_live_check`를 최신값에만 저장하고 reducer로 보내지 않습니다. `video_live_check`의 identity가 맞고 유효한 upstream 종료 시각이 있을 때만 기존 명시적 종료 경로로 반영합니다. 공개 불가·해석 불가 UNKNOWN만으로 수명 상태를 바꾸지 않되, 시작을 관측한 LIVE 세션의 `identity_missing`이 `YOUTUBE_PLANE_LIVE_UNRESOLVABLE_GRACE_SECONDS`(기본 600초) 이상 이어지고 같은 채널의 신선한 `/live` identity 확인 음성이 있으며 신선한 positive가 없으면 `UNRESOLVABLE_VIDEO`로 끝냅니다(ended_at은 첫 `identity_missing` 시각, 알림 없음). UPCOMING 세션과 다른 UNKNOWN 사유는 대상이 아닙니다. `!예정`·`!일정`과 Stream HTTP API는 기존 Holodex 원천·조직·기간·5분 캐시·응답 필드를 유지합니다. 단, `org=all` 부분 실패의 응답은 아래처럼 바뀌었습니다.
- Holodex live/upcoming 목록은 Service 인스턴스 안에서 같은 cache key의 미스를 조정합니다. owner가 조회·저장을 마치면 대기 caller가 캐시를 다시 읽습니다. caller의 취소/기한을 유지하고 결과 포인터나 오류를 공유하지 않습니다. cache write 실패·원천 실패·서로 다른 인스턴스의 요청까지 한 번으로 합친다는 보장은 없습니다.
- 원천 장애 때의 꼬리 지연(stack audit D10, PLN-20260926-stack-audit-refactoring T12 측정 기록, 동작 변경 없음): 실패는 캐시하지 않으므로 같은 key의 동시 miss는 한 명씩 원천을 다시 조회하고, k번째 caller는 앞선 k-1번의 실패 조회를 기다립니다. 2026-09-27 로컬 측정(`GetLiveStreams`, 같은 key caller 6명, MockRequester가 100ms 뒤 응답, miniredis, 3회 반복)에서 성공이면 원천 호출 1번에 6명 모두 100–120ms에 끝났고, 실패면 원천 호출 6번에 완료가 100·200·300·400·500·610ms로 늘어났습니다. caller 기한을 350ms로 두면 3명은 원천 오류로, 3명은 350ms에 `context.DeadlineExceeded`로 끝났습니다(원천 호출 4번, 4번째 조회는 기한으로 취소). 운영 값은 측정하지 않았습니다. 코드상으로는 한 번의 실패 조회가 Holodex client의 시도당 제한(기본 20초)과 재시도·backoff로 길어질 수 있고, 연속 3번 실패하면 circuit breaker가 열려(30초) 이후 대기 caller는 원천을 부르지 않고 바로 실패합니다. 대기 시간의 상한은 각 caller의 ctx 기한입니다.
- `DEC-20260926-youtube-only-stream-providers`에 따라 `!라이브`는 YouTube만 조회합니다. Chzzk·Twitch client/설정/DI와 명령 내 플랫폼 병합은 제거했습니다. YouTube 조회 실패는 기존 조회 실패 응답으로 전달합니다. 멤버 저장 데이터와 프로필의 정적 링크, Stream HTTP JSON 필드는 보존하며 해당 필드가 제공자 지원을 뜻하지는 않습니다.
- 새로운 자동 재시도·DB 실패 시 원천 전환·부분 결과 fallback은 추가하지 않습니다. 예약 retry 의미는 cache-fill 개선에서 변경하지 않았습니다.
- `org=all`에서 일부 org만 실패하면 provider는 성공한 org의 stream과 `PartialStreamsError`를 함께 돌려주고 캐시하지 않습니다(stack audit B1, PLN-20260926-stack-audit-refactoring T09). Stream HTTP API(`/api/holo/streams/live`, `/api/holo/streams/upcoming`)는 이 오류를 다른 원천 실패와 같이 500으로 응답하며 부분 목록을 내보내지 않습니다. 이전에는 부분 목록을 200으로 응답하고 캐시했습니다. 부분 목록과 실패 org를 함께 돌려주는 응답 필드는 공개 계약 변경이라 별도 결정 전에는 추가하지 않습니다. 소비자(iris-console admin-web의 streams 화면)는 한 org 장애 동안 `org=all` 조회 실패를 받습니다.

## Source fallback retirement

- 영속 reply INSERT의 접수 결과 불명(`ErrReplyStagingFailed`)과 Iris 전송 결과 불명은 같은 추가 응답 억제 규칙을 따릅니다. 도움말·달력 이미지 실패의 대체 텍스트나 공통 오류 응답을 추가하지 않고 명령 결과 불명을 보존합니다.
- Iris가 접수한 reply의 handoff 결과를 확정하지 못하면 자동 재발송하지 않는 `manual_review`로 정산합니다. 정산은 caller 취소와 분리된 기존 `settlement_timeout_ms` 예산을 사용하며, 적용되지 않은 정산은 결과 불명으로 집계합니다.
- 멤버 조회 backend 오류는 미발견 응답으로 바꾸지 않습니다. 달력 cache miss의 공유 조회는 기존 bot 명령 시간 예산으로 제한하고, 각 대기 요청은 자신의 context 취소에 따라 반환합니다. shared member cache의 epoch 구독·재조회 작업은 각 plane의 DB·Valkey 정리 전에 종료합니다.

`DEC-20260926-hololive-source-fallbacks-retirement`(PLN-20260926-stack-audit-refactoring T19)에 따라 계약 없는 원천·표시 폴백을 오류 반환 단일 경로로 바꿨습니다. T18(2026-09-26) 30일 로그에서 아래 경로의 fallback·fail-open 경고는 0건이었습니다.

- Holodex `GetChannel`은 YouTube scraper로 부분 Channel을 만들지 않고, `GetChannels`는 목록 API 실패를 개별 조회로 보충하지 않으며, `GetChannelSchedule`은 YouTube·공식 일정으로 보충하거나 그 결과를 캐시하지 않습니다([공식 일정 계약](../contracts/schedule-hololive-tv-api.md)).
- 멤버 matcher의 부분 일치 결과는 roster(멤버 데이터·Valkey 동적 멤버) 이름을 채널명 정본으로 씁니다. Holodex 조회 보강과 그 실패 시 후보명·알림 멤버명 캐시로 채우던 체인은 없습니다. 정확 일치 결과와 같은 이름을 씁니다.
- major event 주간·월간 요약은 LLM 실패·빈 결과·외부 내용 guard 실패를 이벤트 목록만 보내는 대체 경로로 바꾸지 않습니다. 요약 실패는 스케줄러 오류이며 enqueue도 이벤트 표시도 하지 않습니다. 자동 재시도는 없습니다. 스케줄러는 `Failed to send weekly notification`·`Failed to send monthly notification` error 로그만 남기고 다음 실행을 일주일·한 달 뒤로 잡으며, 다음 실행은 새 주(KST 월–일)·새 달 키로 조회하므로 실패한 주기의 digest는 다시 보내지 않습니다. 복구는 같은 KST 주·달 안에 `/internal/trigger/majorevent-weekly`·`/internal/trigger/majorevent-monthly` 수동 trigger로만 합니다([trigger 계약](../contracts/trigger.md), [hololive-api runbook](../runbooks/hololive-api.md)). LLM이 설정되지 않은 모드(`llm == nil`)만 이벤트 목록을 보냅니다.
- `/api/holo` rate limit은 fail-closed입니다. rate limit이 켜져 있는데 cache가 없거나 limiter 초기화에 실패하면 admin API가 기동하지 않고, 판정 실패는 503으로 거절하며 `hololive_admin_rate_limit_check_failures_total`로 셉니다. `hololive_admin_rate_limit_fail_open_total`은 삭제했습니다.
- `domain.MemberDataProvider`는 오류를 돌려주는 `LoadAllMembers` 하나로 전체 멤버를 적재합니다. 오류를 흡수하던 `GetAllMembers`와 선택적 `MemberDataLoader`는 삭제했습니다. 공식 일정 식별 색인·멤버 목록 응답·alarm 콜라보 표시명은 멤버 적재 실패를 빈 결과로 바꾸지 않고 오류로 드러냅니다.
- 알림 멤버 표시명 폴백만 예외 계약으로 남습니다([alarm 계약](../contracts/alarm.md)의 멤버 표시명 예외 계약).

## 도움말·달력 이미지의 텍스트 대체 예외 계약

`!도움말`과 `!달력`은 이미지 응답이 정상 경로이고, 이미지가 확정적으로 실패했을 때만 같은 내용을 텍스트로 보냅니다(2026-10-02 계약화).

| 항목 | 계약 |
|---|---|
| Trigger | 이미지 적재·렌더링 실패나 빈 결과(`render_failed`), 또는 이미지 전송이 전달되지 않았다고 확정된 실패(`send_failed`) |
| 한도 | 명령 한 번에 텍스트 응답 한 번입니다. 이미지 재시도는 없습니다. 이미지 전송 결과가 불명(`IsReplyOutcomeUnknown`)이면 이미 전달됐을 수 있으므로 텍스트를 보내지 않습니다(`outcome_unknown`). |
| 종단 | 텍스트 전송도 실패하면 두 오류를 합쳐 명령 오류로 반환합니다(도움말). 이미지 provider·renderer·전송 callback이 설정되지 않았으면 텍스트로 바꾸지 않고 명령 의존성 오류를 반환합니다. 운영 조립은 이 의존성을 항상 연결합니다. |
| Telemetry | `hololive_bot_image_text_fallback_total{command,reason}`(`command`: `help`, `calendar`; `reason`: `render_failed`, `send_failed`, `outcome_unknown`)와 Warn 로그(`help_image_fallback`, `calendar image ... falling back to text`) |
| Owner | `hololive-api` bot plane의 command handlers(`internal/planes/bot/internal/command/handlers`) |
| 검토 조건 | `render_failed`가 0이 아니면 이미지 렌더러 결함으로 보고 원인을 고칩니다. 두 명령의 `render_failed`와 `send_failed`가 90일 동안 0이면 텍스트 대체를 지우고 이미지 실패를 명령 오류로 반환합니다. |

## Plane별 공유 자원

- bot·admin plane은 `BuildInfraModule`, llm plane은 `BuildLLMSchedulerRuntime`에서 각자 Valkey client 1개, PostgreSQL pool 1개, 멤버 캐시 1개를 만듭니다. YouTube plane은 PostgreSQL pool만 둡니다. 한 프로세스 안에 Valkey client 3개, PostgreSQL pool 4개, 멤버 캐시 3개가 있습니다.
- 각 plane은 종료 때 멤버 캐시(epoch 작업 정지와 대기) → PostgreSQL → Valkey 순서로 닫습니다. 멤버 캐시의 epoch 작업이 Valkey client를 쓰기 때문입니다.
- 이 구조는 `DEC-20260825-hololive-api-dedicated-plane-pools`가 PostgreSQL pool을 plane별로 나눈 것(장애 격리와 독립 drain)과 같은 lifecycle 모델을 Valkey client와 멤버 캐시에도 적용한 것입니다. `DEC-20260626-hololive-api-three-runtime-consolidation`의 "shared Valkey client 1개" 조항은 이 구조로 대체합니다(2026-10-02 결정).
- 비용은 시작 때 멤버 적재 3회, epoch 구독 연결 3개, 15초마다 epoch 조회 3회입니다. 하나로 합치면 한 plane의 pool에 다른 plane이 기대거나 root 소유 pool을 새로 둬야 하므로 합치지 않습니다.

## Shorts observation processing

- 쇼츠 알림 초기화는 `SHORT` watermark와 저장된 canonical 영상이 소유합니다. 비어 있지 않은 유효 목록은 `PARTIAL / GAP_UNRESOLVED`여도 최초 기준 목록으로 저장하며 알리지 않습니다. 빈 부분 목록은 초기화하지 않고, 검증된 complete-empty 목록은 초기화합니다.
- 현재 writer는 빈 부분 목록만으로 초기화하지 않습니다. T18에서 구버전의 빈 부분 목록 초기화 잔여 행이 0건임을 확인한 뒤 해당 watermark를 미초기화로 되돌리던 분기와 보조 조회를 제거했습니다. 일반 영상으로 먼저 저장된 ID도 유효한 Shorts 기준 목록과 canonical 중복 판정에서 제외하지 않으며, 영상 종류를 강제로 변경하지 않습니다. 이미 저장된 목록이나 complete-empty 기준은 유지합니다.
- 초기화 이후 새 canonical video ID만 기존 `NEW_SHORT` outbox로 전달합니다. 이미 저장된 쇼츠는 알림 이력이 없어도 자동 backfill하지 않습니다. 기존 영상·watermark·전송 이력을 지우거나 가짜 `SENT`를 만들지 않습니다.
- `shorts_list`와 `video_list` claim은 같은 채널·kind의 더 앞선 `(scheduled_for, id)` 관측이 replay epoch 안에서 유효하고 `PENDING` 또는 `PROCESSING`이면 후속 관측을 선택하지 않습니다. 대기 중인 후속 관측의 attempt는 증가하지 않습니다. 다른 채널과 다른 kind는 이 순서 제약의 대상이 아니며, 기존 retry/lease recovery/dead-letter 정책은 유지합니다.
- 활성 backlog를 한 번 materialize하여 채널·kind별 선행 목록을 선택한 뒤 due 순서로 필요한 queue 행만 잠급니다. 완료 이력을 후보별로 재조회하지 않으며 custom/generic plan에서 전체 backlog 순회와 실제 claim 후보 조회를 따로 제한합니다.
- 관측 순서 제약을 적용하는 첫 배포에서는 기존 YouTube consumer를 drain한 뒤 교체해야 합니다. 이미 구버전에서 claim된 작업까지 새 claim SQL이 재정렬하지는 않습니다. source replay epoch 변경이나 과거 관측 일괄 replay는 배포 절차에 포함하지 않습니다.
- 부분 목록은 삭제·비공개 근거가 아니며 `earliest_complete_effective_at`을 채우지 않습니다. 최초 목록 이전의 관측이나 수집 범위 밖의 영상까지 복구한다는 보장은 하지 않습니다.

## 일반 영상·최초공개 신규성

- migration 260은 `video_list` generation 2와 `earliest_baseline_effective_at`, 항목별 `novelty_pending`을 도입합니다. generation 1은 보관 관측의 canonical/replay 의미를 유지하지만 신규성 증거로 새 `NEW_VIDEO`를 만들지 않습니다.
- 첫 유효한 비어 있지 않은 목록은 PARTIAL이어도 조용히 기준을 세웁니다. 빈 부분 목록은 기준이 아니며 complete-empty는 기준입니다. 기존 complete 기준은 보존하고 year-1 first-positive clock을 신뢰 가능한 과거 기준으로 추정하지 않습니다.
- 기준 이후 처음 발견한 영상은 신뢰 가능한 게시 시각이 기준 이후일 때만 알립니다. 과거에 게시됐지만 처음 목록에 나타난 영상은 알리지 않습니다. 게시 근거가 없으면 pending으로 저장하며 후속 증거가 확인되면 한 번 알립니다. 이미 알려진 canonical ID·다른 채널/Shorts에서 먼저 저장된 ID는 자동 backfill하지 않습니다.
- 새 미래 최초공개는 검증된 예정 시각으로 한 번 알립니다. 기준 목록에 있던 최초공개와 공개 전환은 재알림하지 않습니다. 부분 목록은 끝까지 COMPLETE 부재 근거와 구분합니다.
- 요청 상한 안에서 증거를 확보하지 못하거나 목록 범위를 벗어난 영상의 알림 복구를 보장하지 않습니다. 이 정책은 알림 누락을 감수하고 과거 영상의 오알림을 막습니다.

## Collection projection heartbeat와 eligibility

- migration 261부터 `youtube_collection_projection_generations.valid_until`만 projection 만료의 정본입니다. target의 `valid_until`은 삭제합니다. API의 bot LiveQuery·수집 진단, collector acquire·Publish membership은 모두 유효한 CURRENT header를 요구하며, acquire의 bundle 조회 statement도 만료를 다시 검사합니다.
- API는 기존 projection guard를 입력 Build **이전**부터 한 번 배타 잠금합니다. READ COMMITTED에서 대기한 writer는 앞선 commit 이후의 입력을 읽으므로 오래된 Build의 역순 활성화를 막습니다. 조회·검증·저장 실패는 transaction을 rollback하며 validity를 연장하지 않습니다.
- header의 `validity_refreshed_at`은 마지막으로 수락한 supplied refresh 시각입니다. 그보다 최신이거나 PostgreSQL 마이크로초 정밀도에서 같은 시각이면 `valid_until`을 해당 시각+현재 TTL로 대체하여 설정 하향·상향을 모두 반영합니다. 더 과거 시각의 호출만 기존 만료를 보존합니다. migration 이전 시각은 NULL로 남기고 첫 성공 refresh에서 수립하며, 과거 TTL을 추정하지 않습니다. API 재시작 후에도 이 기준은 DB에 유지됩니다.
- `TestCollectionRealPolicyGuardCancellationAndExpiry`와 `TestCollectionRealPolicySharedGuardLoad`는 실제 PolicyBuilder/rosterReader의 10,000 target·영상 1,000개·현재 검토 500개·과거 영수증 15,000개에 독립 collector 공유 guard 조회 4개를 병행합니다. 실제 lock 대기, reader/writer 취소, 대기 중 header 만료 거부와 회복을 검증하고 30회 refresh의 guard/transaction p95·p99를 기록합니다. 이는 해당 DB 경로의 격리 부하이며 외부 provider부터 intent까지의 전체 지연이나 운영 지속 부하를 뜻하지 않습니다.
- `LiveCheckVideos` SQL은 구조적 LIVE/지난 일정·출처 미상 UPCOMING과 현재 원본에 적용되는 검토 영수증(`youtube_live_review_current_receipt`, migration 262) 제외를 cap+1 이전에 적용하고, 같은 statement의 DB 시각과 필요한 사실만 반환합니다. API가 미래/NULL 근거를 거부하고 밀리초 단위 기존 예산으로 positive/availability deadline을 계산합니다. notification/operational/live 입력은 READ COMMITTED의 별도 statement이며 하나의 공통 snapshot이라고 보장하지 않습니다. 지연된 refresh 호출 시각은 기존 header validity를 줄이지 않습니다.
- 구조 identity(subject/kind/priority/cadence/enabled)가 같으면 generation/hash와 연속 `member_since_generation`·`created_at`을 유지합니다. 실제 `not_before` 변경 row만 UPDATE하고 같은 transaction에서 header의 양수 `eligibility_version`을 한 번 올립니다(새 generation 기본값 1). 변경 없는 heartbeat는 header만 쓰며 target/reason 행을 갱신하지 않습니다. 이유만 바뀌면 기존처럼 이유를 교체하되 eligibility version은 바꾸지 않습니다.
- collector는 현재 header·target·lease를 같은 후보 statement에서 확인하는 관계형 조회를 유지합니다. 전체 target/lease를 캐시해 AP에서 정렬하는 안은 전송량 회귀로 채택하지 않았으며 target별 eligibility version도 추가하지 않습니다. 기존 상한 10,000 targets/50,000 reasons/1,000 live 입력, membership·lease/fence·DB slot·원자적 Publish와 기존 bounded retention의 CURRENT/STAGING·lease 보호, collector의 reasons/canonical 접근 금지는 유지합니다.
- 259/260은 이미 적용된 migration이며 수정·재적용하지 않습니다. **261은 rolling 호환 변경이 아닙니다.** 별도 운영 승인 아래 구 API(모든 plane)와 모든 collector를 drain·정지한 상태에서 새 migration을 적용하고, 새 API가 유효한 완전 projection을 만든 뒤 같은 계약의 fleet을 재개해야 합니다. 구 binary만 재시작하는 rollback은 지원하지 않으며 schema와 binary의 호환 복구가 필요합니다. 로컬 구현은 운영 activation·보존 단축 승인이 아닙니다.

- canonical live session의 다섯 writer는 부재 행의 동시 INSERT와 부분 schedule DTO 병합을 위해 기존 `0047` SQL conflict merge를 유지합니다. `0048` head 저장은 18개 mutable 필드 전체의 `IS DISTINCT FROM`으로 같은 근거의 tuple rewrite를 피하며 `updated_at`·review snapshot을 유지합니다. 실제 사실 시계·ignored-absence 변화는 계속 저장합니다. 이는 canonical 정책의 AP 이관이나 no-op SQL/잠금 제거를 뜻하지 않습니다.

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
- Internal HTTP/H3 client options are loaded once into `BotPlaneConfig.InternalH3` and `AdminPlaneConfig.InternalH3` and passed explicitly; each plane owns timeout and transport cleanup. Iris URL-file dynamic reload remains on its existing path.
- CLIPROXY/LLM settings where enabled
- Uber Fx v1.24.0 is the process lifecycle owner for this binary only. It is an implementation detail, not an operator-selectable mode, and does not change ports, routes, config keys, or dependency readiness requirements.

## Shutdown behavior

- Fx is the single process signal owner. It starts the optional YouTube plane, then llm, admin, and bot; shutdown cancels the runtime context and drains bot, admin, llm, then the optional YouTube plane.
- Stop HTTP/H3 ingress and scheduler workers within one shared 10-second plane-drain budget. The whole Fx stop is capped at 30 seconds inside the Compose 45-second grace period.
- Plane `CloseContext` receives the remaining process-stop context. Each plane joins its background tasks before releasing owned member-cache/DB/cache resources; bot durable samplers are included. A join timeout preserves live resources and the error, while later waiting uses the same cleanup owner rather than starting concurrent cleanup.
- LLM HTTP/H3 shutdown has one active owner. A caller deadline returns control to the remaining planes even when a detached trigger is still running; the original shutdown error remains observable. LLM resources are released only after the actual HTTP handlers and scheduler have joined, including metrics/pprof handlers.
- YouTube claim release waits for the claim producer to finish registering its returned batch. Registration and fenced release share the existing settlement budget; if registration times out, a later `CloseContext` can finish that release. An attempted release failure is retained without automatic retry, and DB cleanup still waits for all workers to join.
- Bot readiness cancellation is normal only when its error matches the runtime's actual shutdown cancellation. An independent readiness deadline or a joined operational error remains fatal.
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
