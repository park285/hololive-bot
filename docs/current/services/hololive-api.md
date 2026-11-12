# Service: hololive-api

## Runtime identity

| Field | Value |
|---|---|
| Module | `hololive-api` |
| Binary | `hololive-api` |
| Quadlet unit | `hololive-api.service` (container `hololive-api`) |
| Port | `30001` (bot) / `30006` (admin) |
| Health endpoint | `https://127.0.0.1:30001/health` through container `./bin/healthcheck` |
| Ready endpoint | `https://127.0.0.1:30001/internal/ready` through container `./bin/healthcheck --api-key-env API_SECRET_KEY` (bot plane PostgreSQL·Valkey) |

## Role

bot/admin plane과 YouTube Community consume plane을 한 프로세스에서 호스팅하는 통합 runtime입니다.

- Bot plane: Kakao/Iris webhook ingress와 사용자 명령 routing, reply orchestration.
- Admin plane: dashboard-facing admin HTTP control plane, worker alarm HTTP client.

## Owns

- Kakao/Iris webhook ingress, user-facing command routing and reply orchestration (bot plane)
- Dashboard-facing admin HTTP API and the worker alarm client (admin plane)
- Bot-side alarm client
- Process-owned single ACL `Service` (bot pool, bot-plane `KAKAO_*` seed) shared by bot ingress and admin ACL routes
- Observation claim/finalize, canonical persist, notification intent, live-end finalizer, and retention (YouTube plane)
- `members.photo` Holodex PhotoSync product path (admin plane). YouTube channel photos are the `channel_photo` reducer.

## Provides

| Contract | Type | Path/Event/Queue | Consumers |
|---|---|---|---|
| Iris webhook boundary | external HTTP/H3 | webhook/reply/send | Iris / Redroid |
| Admin HTTP API | HTTP JSON | `/api/holo/*` (Console 계약 `iris-console/contracts/hololive/openapi.json`에 있는 operation만) | `iris-console` |
| settings.update | HTTP JSON + `settings.json` | `POST /api/holo/settings` | `iris-console`, `alarm-worker` |

## Consumes

| Dependency | Purpose | Failure impact |
|---|---|---|
| PostgreSQL | command/domain/admin data, subscriptions, outbox | command/admin failures, stale reads |
| Valkey | cache, rate limit, outbox wakeup, member epoch Pub/Sub, coordination(job lock) | degraded command, admin, and cache behavior |
| Iris | KakaoTalk ingress/reply automation | webhook/reply delivery failure |
| Alarm API | alarm CRUD/query | alarm commands and admin operations fail |

API 설정은 `internal/config`가 소유합니다. process 공유 자원(Valkey client, 멤버 캐시, bot·admin PostgreSQL pool,
ACL `Service`, Holodex·공식 일정 Service)의 조립과 정리는 `internal/app`의 `buildAPIInfrastructure`가 소유합니다.
Admin 조립·수명은 `internal/planes/admin/runtime`, router와 Stream handler는
`internal/planes/admin/internal/httpapi`에 있습니다. plane 사이 의존(admin→bot Iris 방 목록,
admin·bot ACL)은 `internal/app`이 같은 프로세스 인스턴스를 주입하며 loopback HTTP를 쓰지 않습니다.
Observation consume·canonical/retention과 private reducer는 `internal/youtube/`가 소유합니다.
Bot·admin은 필수 `ALARM_INTERNAL_URL`의 worker provider를 사용하며 in-process alarm service를 생성하지 않습니다.

## Must not own

- Alarm checker/scheduler loops owned by `alarm-worker`
- Claim and consumption of the PostgreSQL proactive delivery outboxes (`alarm_dispatch_deliveries`, `youtube_notification_outbox`) owned by `alarm-worker`
- Proactive Iris/Kakao notification egress owned by `alarm-worker`

## 관리자 멤버 쓰기

- 멤버 생성은 `CreateMemberRequest`로 저장 가능한 선택 입력을 유지하면서 이름·채널 식별자의 앞뒤 공백을 정리합니다. 이름은 필수이며 slug의 100자 한도와 각 저장 열의 문자 수 한도를 검증합니다. Unicode 문자 수 기준이며 선택 필드를 필수로 바꾸지 않습니다.
- `MemberAdminService`가 생성·졸업·이름·채널·별명 추가/삭제의 정본 저장 → 활동 기록 → 캐시 동기화를 소유합니다. 도메인 HTTP 핸들러는 필요한 의존성만 보유하며, 멤버 핸들러는 ACL·Iris 없이 구성됩니다.
- 생성은 HTTP 201, 나머지 변경은 HTTP 200과 기존 `status`·`message`를 유지합니다. 응답의 `persisted`, `cacheSynced`, `memberId`, `replayed`가 저장·전파·동일 요청 복구를 구분합니다. 저장 후 캐시 오류는 저장 실패 500으로 바꾸지 않고 `persisted=true`, `cacheSynced=false`와 경고 문구를 반환합니다.
- 생성 재요청은 slug 유일성 충돌 뒤 **모든 저장 입력과 현재 정본이 같은 경우만** 기존 ID로 캐시 동기화를 수행합니다. 새 행과 생성 활동 기록을 중복 추가하지 않습니다. 한 필드라도 다르면 HTTP 409 `Member already exists with different values`이며 기존 행을 덮어쓰지 않습니다. 자동 재시도·새 원천 fallback·마이그레이션은 없습니다.
- 동시 변경이 다른 요청의 snapshot warmup을 무효화한 경우에도 `cacheSynced=false`를 보존합니다. 오류를 성공으로 숨기거나 반복 갱신하지 않습니다. 요청 유실 뒤에도 동일 생성 요청으로 현재 정본 일치를 확인할 수 있습니다.
- Iris Console의 실제 Go 응답 projection은 기존 201/200·`status`·`message`로 위 응답을 수락합니다. 추가 필드는 현재 projection에서 제거되므로 Console 화면이 캐시 동기화 상태를 표시한다는 보장은 없습니다.
- 활동 기록은 기존 logger sink를 사용합니다. 캐시 실패 때문에 이미 저장된 변경의 기록을 건너뛰지는 않지만, DB commit과 로그 저장 사이의 프로세스 종료까지 원자적으로 보장하는 감사 outbox는 아닙니다.

## Admin 사용자 인증

- admin plane은 사용자 계정·세션을 소유하지 않습니다. Iris Console이 자기 세션으로 사용자를 인증하고 admin plane에는 `/api/holo/*`를 API key로만 호출합니다.
- 독립 대시보드 잔재였던 `/api/auth/*`(register·login·logout·refresh·me)와 `/oauth/callback`, auth service(Valkey `auth:*` 세션·bcrypt·로그인 rate limit), `ADMIN_ALLOWED_IPS`·`AUTH_BCRYPT_COST` 설정, `auth_users` 테이블(migration 348)을 제거했습니다. 비밀번호 재설정 경로와 `auth_password_reset_tokens`(migration 281)는 그보다 먼저 제거했습니다.
- 제거한 경로는 다른 미등록 경로와 같이 NoRoute 응답을 받습니다. API key가 없으면 401, 틀리면 403, 맞으면 404입니다.
- 다시 사용자 인증을 두려면 Iris Console 경계와 겹치지 않는 근거와 공개 API 계약 변경 승인이 먼저 필요합니다.

## Live query behavior

- `!라이브`는 기존 bot DB pool의 단일 snapshot으로 확정 방송과 채널 확인 최신값을 읽으며 원천을 호출하지 않습니다. 무인자는 우이를 포함한 활성 등록 Hololive 채널, 멤버 지정은 해석된 채널을 직접 조회합니다. freshness는 `min(5분, 2×poll interval+30초)`, 기본 270초이며 DB 조회 예산은 1초입니다. `DEC-20260926-hololive-live-absence-evidence`와 [실행 계획](../plans/2026-09-26-live-absence-evidence.md)이 소유합니다.
- 공개·멤버 한정·최초공개의 fresh positive는 `/live`가 예정 영상이나 채널 페이지를 고르더라도 표시합니다. 방송 탭·Holodex 누락·absence slot은 채널 coverage가 아닙니다. 모든 채널의 신선한 음성 확인과 해소된 현재 후보가 갖춰진 빈 결과만 '현재 방송 중인 멤버가 없습니다.'로 안내합니다. 그 밖의 빈 결과는 '현재 방송 상태를 확인할 수 없습니다.'입니다. 멤버 지정 빈 결과의 `CMD_MEMBER_NOT_LIVE`와 100개 표시 한도는 유지합니다.
- 세션·head 없는 EXPLICIT_END와 session=ENDED는 완전성을 막지 않고 `live query incomplete`의 `nonblocking_diagnostics`로 남깁니다. LIVE/head 없음은 계속 차단합니다. 신선한 공개 불가 사실과 익명 수집에서 비공개·삭제 영상이 남기는 신선한 `identity_missing` UNKNOWN은 정상 head가 있는 stale LIVE 후보만 제외하며, 만료나 그 밖의 UNKNOWN 사유는 다시 차단합니다. pending을 삭제하거나 표시 전용 종료 상태를 만들지 않습니다. 사용자 응답에는 조회 사유·기준 시각·범위 설명을 붙이지 않습니다(`DEC-20260926-hololive-list-reply-fold-default`).
- YouTube plane은 `channel_live_check`를 최신값에만 저장하고 reducer로 보내지 않습니다. `video_live_check`의 identity가 맞고 유효한 upstream 종료 시각이 있을 때만 기존 명시적 종료 경로로 반영합니다. 공개 불가·해석 불가 UNKNOWN만으로 수명 상태를 바꾸지 않되, 시작을 관측한 LIVE 세션의 `identity_missing`이 `YOUTUBE_PLANE_LIVE_UNRESOLVABLE_GRACE_SECONDS`(기본 600초) 이상 이어지고 같은 채널의 신선한 `/live` identity 확인 음성이 있으며 신선한 positive가 없으면 `UNRESOLVABLE_VIDEO`로 끝냅니다(ended_at은 첫 `identity_missing` 시각, 알림 없음). UPCOMING 세션과 다른 UNKNOWN 사유는 종료 대상이 아닙니다. 다만 UPCOMING의 `identity_missing`·`identity_mismatch`는 추적만 하여 projection이 그 영상 확인 target의 재확인 주기를 추적 기간에 따라 2분→10분→30분→1시간으로 늦춥니다(10분·1시간·24시간 경계). 추적 시작 이후의 positive가 오면 다음 projection에서 2분으로 돌아오고, collector는 이전 긴 주기의 남은 대기 없이 현재 주기의 다음 slot에서 재확인합니다. head가 없는 `metadata_only`·`legacy_unknown` UPCOMING은 추적하지 않습니다. `!예정`·`!일정`과 Stream HTTP API는 기존 Holodex 원천·조직·기간·5분 캐시를 유지합니다. 응답 필드는 아래 Chzzk·Twitch 필드 제거 외에는 그대로이고, `org=all` 부분 실패의 응답은 아래처럼 바뀌었습니다.
- hololive-api process는 Holodex·공식 일정 Service를 하나만 만듭니다(`app.buildAPIInfrastructure`, bot plane의 `HOLODEX_*`·공식 일정 설정). bot(`!예정`·`!일정`)과 admin(`/streams/*`, PhotoSync)이 같은 인스턴스를 빌리므로 retry scheduler·cache-fill 조정이 process 하나로 묶이고, 종료는 모든 plane이 멈춘 뒤 process 조립이 한 번 합니다. 같은 `HOLODEX_API_KEY`를 쓰는 hololive-api와 alarm-worker는 분산 rate limit(`HOLODEX_DISTRIBUTED_RATELIMIT_ENABLED`, 기본 true)이 켜져 있으면 429 대기와 circuit open을 Valkey로 함께 지킵니다. 429의 `Retry-After` 시각은 `<bucket base>:cooldown`(기본 `holodex:api:cooldown`)에 뒤로만 밀리게 기록하고, 요청 승인 전후에 읽어 로컬 cooldown에 합칩니다. 로컬 breaker가 연속 실패로 열리는 순간에만 `<bucket base>:circuit_open`을 `SET NX PX 30000`으로 열며, 이미 열려 있으면 만료를 늘리지 않습니다. 요청 진입과 각 재시도 전에 이 key를 읽어, 열려 있으면 HTTP 요청 없이 503으로 거절합니다. 연속 실패 카운터는 process마다 따로 셉니다. 공유 상태의 Valkey 오류는 로컬 상태로 진행하고(fail-open) `hololive_holodex_shared_state_failures_total{operation}`과 Warn 로그로 드러냅니다. 호출자 취소·기한 만료로 끝난 읽기는 Valkey 오류로 세지 않습니다. 같은 Valkey를 쓰는 분산 rate limit은 계속 fail-closed입니다. 공유 key는 재시작 뒤에도 남으므로 비정상적으로 긴 `Retry-After`가 기록되면 그 시각까지 두 process가 기다립니다. admin plane 설정은 `HOLODEX_*`·공식 일정 env를 더 읽지 않습니다.
- Holodex live/upcoming 목록은 Service 인스턴스 안에서 같은 cache key의 미스를 조정합니다. owner가 조회·저장을 마치면 대기 caller가 캐시를 다시 읽습니다. caller의 취소/기한을 유지하고 결과 포인터나 오류를 공유하지 않습니다. cache write 실패·원천 실패·다른 process(collector·alarm-worker)의 요청까지 한 번으로 합친다는 보장은 없습니다.
- 원천 장애 때의 꼬리 지연(stack audit D10, PLN-20260926-stack-audit-refactoring T12 측정 기록, 동작 변경 없음): 실패는 캐시하지 않으므로 같은 key의 동시 miss는 한 명씩 원천을 다시 조회하고, k번째 caller는 앞선 k-1번의 실패 조회를 기다립니다. 2026-09-27 로컬 측정(Hololive `GetLiveStreamsByOrg`, 같은 key caller 6명, MockRequester가 100ms 뒤 응답, miniredis, 3회 반복)에서 성공이면 원천 호출 1번에 6명 모두 100–120ms에 끝났고, 실패면 원천 호출 6번에 완료가 100·200·300·400·500·610ms로 늘어났습니다. caller 기한을 350ms로 두면 3명은 원천 오류로, 3명은 350ms에 `context.DeadlineExceeded`로 끝났습니다(원천 호출 4번, 4번째 조회는 기한으로 취소). 운영 값은 측정하지 않았습니다. 코드상으로는 한 번의 실패 조회가 Holodex client의 시도당 제한(기본 20초)과 재시도·backoff로 길어질 수 있고, 연속 3번 실패하면 circuit breaker가 열려(30초) 이후 대기 caller는 원천을 부르지 않고 바로 실패합니다. 대기 시간의 상한은 각 caller의 ctx 기한입니다.
- `DEC-20260926-youtube-only-stream-providers`에 따라 `!라이브`는 YouTube만 조회합니다. Chzzk·Twitch client/설정/DI와 명령 내 플랫폼 병합은 제거했습니다. YouTube 조회 실패는 기존 조회 실패 응답으로 전달합니다. 멤버 저장 데이터와 프로필의 정적 링크는 보존하며 제공자 지원을 뜻하지는 않습니다. Stream JSON의 Chzzk·Twitch 필드 10개(`chzzk_channel_id`, `chzzk_live_id`, `chzzk_live_url`, `is_integrated`, `is_chzzk_only`, `twitch_user_id`, `twitch_user_login`, `twitch_stream_id`, `twitch_live_url`, `is_twitch_only`)는 채우는 생산자가 없어 제거했습니다. Stream HTTP 응답과 alarm dispatch event payload에서 이 키가 빠집니다. 알림 템플릿 함수 `trustedlink`도 YouTube 링크와 치지직 링크를 ` | `로 묶은 동시 송출 값을 두 줄 링크로 나누던 분기를 지웠습니다. 그런 값을 만드는 생산자가 없으며, 들어오면 다른 신뢰하지 않는 값처럼 `mdsafe`로 보호해 일반 문자로 표시합니다. Twitch·치지직 단일 링크는 Holodex `/users/live` 응답의 `link`가 `Stream.GetYouTubeURL`을 거쳐 알림 URL이 될 수 있어 신뢰 출처로 남깁니다.
- 새로운 자동 재시도·DB 실패 시 원천 전환·부분 결과 fallback은 추가하지 않습니다. 예약 retry 의미는 cache-fill 개선에서 변경하지 않았습니다.
- `org=all`에서 일부 org만 실패하면 provider는 성공한 org의 stream과 `PartialStreamsError`를 함께 돌려주고 캐시하지 않습니다(stack audit B1, PLN-20260926-stack-audit-refactoring T09). Stream HTTP API(`/api/holo/streams/live`, `/api/holo/streams/upcoming`)는 이 오류를 다른 원천 실패와 같이 500으로 응답하며 부분 목록을 내보내지 않습니다. 이전에는 부분 목록을 200으로 응답하고 캐시했습니다. 부분 목록과 실패 org를 함께 돌려주는 응답 필드는 공개 계약 변경이라 별도 결정 전에는 추가하지 않습니다. 소비자(iris-console admin-web의 streams 화면)는 한 org 장애 동안 `org=all` 조회 실패를 받습니다.

## Source fallback retirement

- 영속 reply INSERT의 접수 결과 불명(`ErrReplyStagingFailed`)과 Iris 전송 결과 불명은 같은 추가 응답 억제 규칙을 따릅니다. 도움말·달력 이미지 실패의 대체 텍스트나 공통 오류 응답을 추가하지 않고 명령 결과 불명을 보존합니다.
- Reply 발송은 Iris `202` admission과 `requestId` 저장에서 끝나고 행은 lease 없는 `accepted`가 됩니다. handoff 확인은 bot plane의 reply handoff reconciler가 맡으며 `failed`는 같은 행의 `:r1`/`:r2` 재발급(세대 상한 2, 예산 소진 시 `dead`), `outcome_unknown`·`404`·지평 미확인은 자동 재발송 없는 `manual_review`로 정산합니다. `last_error` 어휘는 `handoff_failed`, `handoff_outcome_unknown`, `handoff_status_missing`, `handoff_unconfirmed`입니다. 발송 정산은 caller 취소와 분리된 기존 `settlement_timeout_ms` 예산을 사용하며, 적용되지 않은 정산은 결과 불명으로 집계합니다. Reply outbox attempt `success`는 admission 완료를 뜻하고 handoff 완료 지표는 없으며 reconciler 결과는 로그와 manual review backlog로 관측합니다. 엄격 shape CHECK(migration 332)는 lease 있는 `accepted`를 거부하고, 만료 lease reclaim은 `submitting`만 다룹니다. `bot_reply_outbox_accepted_reclaimed_total`은 삭제했습니다. 확인 간격·전이표·관측·적용 전제·rollback 조건은 [API runbook](../runbooks/hololive-api.md#3-durable-webhook-or-reply-backlog-grows)을 따릅니다.
- 멤버 조회 backend 오류는 미발견 응답으로 바꾸지 않습니다. 달력 cache miss의 공유 조회는 기존 bot 명령 시간 예산으로 제한하고, 각 대기 요청은 자신의 context 취소에 따라 반환합니다. 달력 항목 스냅샷(`bot:calendar:entries:v1:<YYYY-MM>`, TTL `BOT_CALENDAR_ENTRY_CACHE_TTL_SECONDS`)과 렌더한 PNG(`bot:calendar:image:v5:<YYYY-MM>`, 값 앞에 항목 해시, TTL 7일)는 Valkey에 두어 인스턴스와 재시작 사이에서 공유합니다. 같은 달 항목이 바뀌면 PNG 월 key를 덮어쓰고, 차단 사진 대체 이미지처럼 공유하면 안 되는 렌더 결과는 저장하지 않습니다. 이 cache는 정본이 아니므로 Valkey 오류와 손상 값은 miss로 보고 PostgreSQL 조회·렌더로 진행하며 `render_failed`로 세지 않고 `calendar_entry_cache_failed`·`calendar_image_cache_failed` Warn 로그로만 드러냅니다. 호출자 취소·기한 만료로 끝난 cache 연산은 Valkey 장애가 아니므로 이 경고를 남기지 않습니다. 공유 멤버 캐시의 epoch 구독·재조회 작업은 공유 Valkey client와 bot·admin pool을 닫기 전에 종료합니다.
- bot·worker의 공유 방 Catalog는 같은 인스턴스에서 겹친 전체 HTTP 조회와 per-room 저장 순회를 공유합니다. owner의 전체 예산은 기존 `QueryTimeout.Default` 5초이며 initiator/follower는 각자 취소할 수 있습니다. owner가 저장 중 만료돼도 살아 있는 waiter는 이미 확보한 방 정보를 사용합니다. 완료 뒤의 miss는 새 조회가 가능하고 negative cache는 없습니다. DB 조회 오류의 HTTP fallback 금지, 명시적 webhook metadata 우선, unknown의 plain 처리와 동일 metadata의 `Observe` 저장 재시도는 유지합니다.

`DEC-20260926-hololive-source-fallbacks-retirement`(PLN-20260926-stack-audit-refactoring T19)에 따라 계약 없는 원천·표시 폴백을 오류 반환 단일 경로로 바꿨습니다. T18(2026-09-26) 30일 로그에서 아래 경로의 fallback·fail-open 경고는 0건이었습니다.

- Holodex `GetChannelSchedule`은 YouTube·공식 일정으로 보충하거나 그 결과를 캐시하지 않습니다([공식 일정 계약](../contracts/schedule-hololive-tv-api.md)). YouTube scraper로 부분 Channel을 만들던 `GetChannel`은 그 뒤 호출자가 없어 삭제했습니다.
- 멤버 matcher의 부분 일치 결과는 roster(`MemberDataProvider.LoadAllMembers`로 적재한 멤버 데이터) 이름을 채널명 정본으로 씁니다. Holodex 조회 보강과 그 실패 시 후보명·알림 멤버명 캐시로 채우던 체인은 없습니다. 정확 일치 결과와 같은 이름을 씁니다.
- `/api/holo` rate limit은 fail-closed입니다. rate limit이 켜져 있는데 cache가 없거나 limiter 초기화에 실패하면 admin API가 기동하지 않고, 판정 실패는 503으로 거절하며 `hololive_admin_rate_limit_check_failures_total`로 셉니다. `hololive_admin_rate_limit_fail_open_total`은 삭제했습니다.
- `domain.MemberDataProvider`는 오류를 돌려주는 `LoadAllMembers` 하나로 전체 멤버를 적재합니다. 공식 일정 식별 색인·멤버 목록 응답·alarm 콜라보 표시명은 멤버 적재 실패를 빈 결과로 바꾸지 않고 오류로 드러냅니다.
- 알림 멤버 표시명 폴백만 예외 계약으로 남습니다([alarm 계약](../contracts/alarm.md)의 멤버 표시명 예외 계약).

## 도움말·달력 이미지의 텍스트 대체 예외 계약

`!도움말`과 `!달력`은 이미지 응답이 정상 경로이고, 이미지가 확정적으로 실패했을 때만 같은 내용을 텍스트로 보냅니다(2026-10-02 계약화).

| 항목 | 계약 |
|---|---|
| Trigger | 이미지 적재·렌더링 실패나 빈 결과(`render_failed`), 또는 이미지 응답을 reply outbox에 기록하기 전에 난 실패(`send_failed`: transport·outbox writer·reply identity가 없거나 저장 payload 인코딩이 실패한 조립·인코딩 결함)입니다. `send_failed`는 Iris 전달 실패를 뜻하지 않습니다. Iris 전달은 기록 뒤 reply outbox 발송이 맡으며 그 결과는 명령에 돌아오지 않습니다. outbox 기록 실패(`ErrReplyStagingFailed`)는 행이 커밋됐을 수 있으므로 `send_failed`가 아니라 `outcome_unknown`입니다. |
| 한도 | 명령 한 번에 텍스트 응답 한 번입니다. 이미지 재시도는 없습니다. 이미지 전송 결과가 불명(`IsReplyOutcomeUnknown`)이면 이미 전달됐을 수 있으므로 텍스트를 보내지 않습니다(`outcome_unknown`). |
| 종단 | 텍스트 전송도 실패하면 두 오류를 합쳐 명령 오류로 반환합니다(도움말). 이미지 provider·renderer·전송 callback이 설정되지 않았으면 텍스트로 바꾸지 않고 명령 의존성 오류를 반환합니다. 운영 조립은 이 의존성을 항상 연결합니다. |
| Telemetry | `hololive_bot_image_text_fallback_total{command,reason}`(`command`: `help`, `calendar`; `reason`: `render_failed`, `send_failed`, `outcome_unknown`)와 Warn 로그(`help_image_fallback`, `calendar image ... falling back to text`) |
| Owner | `hololive-api` bot plane의 command handlers(`internal/planes/bot/internal/command/handlers`) |
| 검토 조건 | `render_failed`가 0이 아니면 이미지 렌더러 결함으로 보고 원인을 고칩니다. 두 명령의 `render_failed`와 `send_failed`가 90일 동안 0이면 텍스트 대체를 지우고 이미지 실패를 명령 오류로 반환합니다. |

## PhotoSync 재시도 예외 계약

admin plane PhotoSync는 `PHOTO_SYNC_ENABLED`(admin plane 기본 true)가 켜져 있으면 기동 10초 뒤 한 번, 그 뒤 7일마다 `members.photo`를 Holodex 채널 사진으로 갱신합니다. 한 주기가 실패하면 아래 범위에서만 같은 주기를 다시 시도합니다(2026-11-11 계약화).

| 항목 | 계약 |
|---|---|
| Trigger | 한 주기의 실패입니다. 갱신 대상(`channel_id`가 있고 `photo`나 `photo_updated_at`이 NULL이거나 7일보다 오래된 멤버) 조회 실패, 또는 대상이 있을 때의 Holodex 채널 목록 조회 실패가 해당합니다. 채널별 `members.photo` 저장 실패는 실패 수로만 세고 재시도하지 않습니다. 호출자 취소로 끝난 시도도 해당하지 않습니다. |
| 한도 | 주기마다 최대 3번 시도합니다. 재시도 전 대기는 5초와 10초이며 각각 0~2초 jitter를 더합니다. 각 시도의 Holodex 호출은 Holodex client의 자체 재시도와 circuit breaker를 그대로 거칩니다. |
| 종단 | 어느 단계든 3번 모두 실패하면 그 주기를 포기합니다. 다음 시도는 다음 7일 주기이거나 재기동 뒤 첫 동기화입니다. |
| Telemetry | 재시도 직전 Warn 로그 `Photo sync failed, will retry`(`attempt`, `max_attempts`), 최종 실패 Error 로그 `Photo sync failed after all retries`, 완료 Info 로그 `Photo sync completed`(`success`, `failed`, `total`)입니다. 전용 metric은 없습니다. |
| Owner | `hololive-api` admin plane의 PhotoSync(`internal/planes/admin/internal/photosync`) |
| 제거 조건 | 90일 동안 `Photo sync failed, will retry` 뒤 같은 주기가 `Photo sync completed`로 끝난 기록이 없으면 재시도를 지웁니다. 지우면 Holodex 일시 장애 때 다음 시도가 7일 주기 뒤로 밀리므로, 그 동작 변경을 CHANGELOG에 남깁니다. |

## 주간 일정 포스터

admin plane은 `SCHEDULE_POSTER_CONFIG_FILE`이 있으면 X의 멤버 주간 일정 포스터를 수집하고 번역합니다. 파일이 없으면 수집·번역만 꺼지고, 관리 API(`/api/holo/schedule-posters*`)와 `!스케쥴` 표시는 그대로 동작합니다. 잘못된 설정 파일이나 Vertex client 생성 실패는 기동 오류입니다.

- 설정 파일(64KiB 이하, 모르는 키 거부): `{"poll_seconds":3600,"targets":[{"x_user_id":"<X 사용자 ID>","channel_id":"<YouTube 채널 ID>"}],"translation":{"project":"<GCP 프로젝트>","location":"global","model":"gemini-nano-banana-2.1","text_model":"gemini-4-argon","glossary":{"ホロドリ":"홀로도리"}}}`. `poll_seconds`는 600~86400이고 기본 3600, `location`·`model`·`text_model`은 생략하면 위 기본값입니다. `glossary`(선택, 200개 이하, 원문·번역 각 1~80자, 제어 문자 불가)는 멤버 이름 밖의 용어 표기입니다. Vertex 인증은 `GOOGLE_APPLICATION_CREDENTIALS`(서비스 계정 ADC)로 공급합니다. Quadlet은 `HOLOLIVE_SCHEDULE_POSTER_ENABLED=1`일 때 `optional/schedule-poster` drop-in으로 두 파일을 mount합니다.
- 수집: `poll_seconds`마다 대상별로 FxTwitter(`api.fxtwitter.com`)의 게시물 목록을 최대 5페이지 읽습니다. 응답은 시간순이 아니고 리포스트가 섞이므로 본인 원글만 보고, 지난 8일 안의 원글 가운데 사진과 일정 문구(`週間スケジュール|スケジュール|schedule|予定|weekly`)가 함께 있는 글을 검토 대기 후보(`schedule_posters`)로 넣습니다. 첫 사진 URL이 `https://pbs.twimg.com`이 아니면 저장하지 않고 Warn 로그 `Schedule poster photo skipped: unexpected image URL`(`post_id`, `image_url`)을 남깁니다(Iris Console은 이 호스트의 원본만 표시합니다). 본인 원글이 모두 8일보다 오래된 페이지에서 멈춥니다. 같은 tick에 8일 넘게 검토되지 않은 후보는 `expired`로, 주 종료일(없으면 생성일)이 60일 지난 검토 완료 행은 100건씩 지웁니다.
- 검토: 관리자가 Iris Console에서 원본을 보고 주 범위(7일 이하, 게시일과 7일 이내)를 입력해 확정하거나 거절합니다. 확정만 번역을 예약하며, 모든 변경은 `expectedRevision` 조건이고 충돌은 409 `revision_conflict`입니다. 확정하면 같은 멤버가 그 주 범위(마지막 날 제외)에 올린 다른 검토 대기 후보를 `expired`로 닫고, 이후 수집도 그 범위의 글을 후보로 넣지 않습니다. 마지막 날은 다음 주 포스터를 미리 올리는 경우가 있어 계속 받습니다.
- 번역: 1분마다 확정된 번역 대기 행을 5개까지 처리합니다. 원본(`pbs.twimg.com`, 200 응답, 8MiB 이하)을 받아 두 단계로 번역합니다. 원본 형식은 응답 헤더가 아니라 내용으로 판정하며 JPEG·PNG·WebP만 받습니다. 1단계는 텍스트 모델(`text_model`)이 Google 검색 grounding(웹 검색만)과 thinking level `HIGH`로 포스터의 일본어를 `원문 ⇒ 번역` 줄의 번역표로 만듭니다. 프롬프트에는 포스터 주인 멤버와 현역 Hololive 멤버의 일본어(로마자)→한국어 이름 대응표(`members` 표), 설정 `glossary`가 들어갑니다. 응답에서 이 형식의 줄만 받으며(80줄·줄당 200자 이하), 맞는 줄이 없거나 상한을 넘으면 실패입니다. 2단계는 이미지 모델(`model`)이 도구 없이 thinking level `HIGH`, 2K로 원본의 글자를 번역표대로 바꿔 그립니다. 결과는 JPEG(4MiB 이하)로 저장합니다. 멤버 목록을 읽지 못하면 그 tick은 번역하지 않고 행을 대기 상태로 둡니다. Info 로그 `Schedule poster text translated`에 번역표(`table`)와 실제 검색 횟수(`web_search_queries`)가 남습니다. 번역본은 관리자가 다시 검토하지 않고 바로 `!스케쥴`로 보입니다. 재번역을 예약해도 새 번역본이 저장될 때까지 이전 번역본을 계속 게시하고, 재번역이 실패해도 이전 번역본이 남습니다(migration 012). 원본은 저장하지 않으므로 X에서 원본이 지워진 뒤의 재번역은 `source_unavailable`로 끝나고 이전 번역본이 유지됩니다. 게시를 내리려면 거절합니다. Console의 번역본 조회와 `translatedAt`은 번역 성공 상태에서만 값을 줍니다.
- 표시: `!스케쥴 <멤버>`는 오늘(JST)이 주 범위에 드는 확정본 가운데 번역본이 있는 가장 최근 것을 이미지로 보냅니다. 없으면 안내 문구를, 조회 실패면 오류 문구를 보내고 발송 실패는 명령 오류로 올립니다. 요청은 라이브·방송 이력과 따로 사용자당 1분 3회, 방당 1분 12회로 제한하며, 초과하면 제한 안내를 보내고 Valkey 판정에 실패하면 명령을 실행하지 않습니다. `!일정`·`!스케줄`(Holodex 일정)과는 다른 명령이며 Iris `commandRoutePrefixes.hololive`에 `!스케쥴`이 있어야 전달됩니다.

## 일정 포스터 번역 재시도 예외 계약

| 항목 | 계약 |
|---|---|
| Trigger | 번역 대기 행 하나의 번역표 단계 실패(Vertex 텍스트 생성 실패, 형식에 맞는 줄이 없거나 상한 초과) 또는 이미지 단계 실패(Vertex 이미지 생성 실패, 응답에 이미지가 없음, 결과 이미지의 디코드(PNG·JPEG만 받습니다)·JPEG 변환 실패)입니다. Vertex가 요청 자체를 거절한 오류(HTTP 400·401·403·404)는 다시 보내도 같으므로 재시도하지 않고 바로 실패로 기록합니다. 원본 이미지 내려받기 실패는 재시도하지 않고 바로 `source_unavailable`로 기록합니다. 호출자 취소로 끝난 시도는 기록하지 않고 다음 tick이 다시 처리합니다. |
| 한도 | 행마다 단계별로 최대 3번 시도합니다. 번역표가 나온 뒤 이미지 단계를 다시 시도할 때는 번역표를 다시 만들지 않습니다. 재시도 전 대기는 5초와 10초이며 각각 0~2초 jitter를 더합니다. 시도 하나의 상한은 단계마다 3분이고, genai SDK 자체 재시도는 1회로 고정합니다. 행 하나는 최악 약 18분을 쓰며 그동안 같은 tick의 다음 행은 기다립니다. |
| 종단 | 3번 모두 실패하면 `translation_status=failed`, `translation_error=translation_failed`로 기록합니다. 다시 시도하려면 관리자가 Console에서 재번역을 예약합니다. |
| Telemetry | 재시도 직전 Warn 로그 `Schedule poster translation attempt failed, will retry`(`poster_id`, `stage`=`text`|`image`, `attempt`), 최종 실패 Warn 로그 `Schedule poster translation failed`(`poster_id`, `reason`), 성공 Info 로그 `Schedule poster translated`(`poster_id`, `bytes`)입니다. 전용 metric은 없습니다. |
| Owner | `hololive-api` admin plane의 일정 포스터 서비스(`internal/planes/admin/internal/scheduleposter`) |
| 제거 조건 | 90일 동안 재시도 뒤 같은 행이 `Schedule poster translated`로 끝난 기록이 없으면 재시도를 지웁니다. 지우면 일시 장애마다 관리자가 재번역을 눌러야 하므로 그 동작 변경을 CHANGELOG에 남깁니다. |

## Plane별 공유 자원

- `internal/app`의 `buildAPIInfrastructure`가 bot plane 설정으로 Valkey client 1개, `bot` PostgreSQL pool, 멤버 캐시 1개를 만들고(`BuildInfraModule`), admin plane용으로는 PostgreSQL pool(`admin`)만 따로 만듭니다. admin plane은 Valkey client와 멤버 캐시를 bot 것과 같은 인스턴스로 빌립니다. YouTube plane은 자기 `youtube_plane` pool을 만들고, 공유 Valkey client는 알림 행 commit 뒤 alarm-worker를 깨우는 신호에만 씁니다. 한 프로세스 안에 Valkey client 1개, 멤버 캐시 1개, PostgreSQL pool 3개(`bot`, `admin`, `youtube_plane`)가 있으며 `TestAPIInfrastructureSharesCacheButSeparatesRuntimePools`가 이 공유와 pool 분리를 검증합니다.
- plane에 넘긴 공유 자원에는 종료 권한이 없습니다. bot·admin·YouTube plane의 `CloseContext`가 각자 작업을 join하고(YouTube plane은 자기 pool도 닫음) 나면 process 조립이 공유 자원을 한 번 닫습니다. 순서는 공유 Holodex Service 정지 → 멤버 캐시(epoch 작업 정지와 대기) → `admin` pool → `bot` pool → Valkey client이며, 멤버 캐시의 epoch 작업이 Valkey client를 쓰기 때문입니다. bot·admin 가운데 자원 사용 작업이 join되지 않은 plane이 있으면 공유 자원을 닫지 않고 오류를 남깁니다.
- PostgreSQL pool을 plane별로 나누는 근거(장애 격리와 독립 drain)는 `DEC-20260825-hololive-api-dedicated-plane-pools`가 소유합니다. Valkey client와 멤버 캐시는 `DEC-20260626-hololive-api-three-runtime-consolidation`의 "shared Valkey client 1개" 조항대로 process에 하나만 둡니다. 2026-10-02에 plane별로 나눴던 Valkey client와 멤버 캐시는 v11.0.0(2026-10-19)에서 한 소유자로 다시 합쳤습니다.
- 멤버 캐시 비용은 시작 때 멤버 적재 1회, epoch 구독 연결 1개, 15초마다 epoch 조회 1회이며, 적재·재조회는 `bot` pool을 씁니다.

## Shorts observation processing

- 쇼츠 알림 초기화는 `SHORT` watermark와 저장된 canonical 영상이 소유합니다. 비어 있지 않은 유효 목록은 `PARTIAL / GAP_UNRESOLVED`여도 최초 기준 목록으로 저장하며 알리지 않습니다. 빈 부분 목록은 초기화하지 않고, 검증된 complete-empty 목록은 초기화합니다.
- 현재 writer는 빈 부분 목록만으로 초기화하지 않습니다. T18에서 구버전의 빈 부분 목록 초기화 잔여 행이 0건임을 확인한 뒤 해당 watermark를 미초기화로 되돌리던 분기와 보조 조회를 제거했습니다. 일반 영상으로 먼저 저장된 ID도 유효한 Shorts 기준 목록과 canonical 중복 판정에서 제외하지 않으며, 영상 종류를 강제로 변경하지 않습니다. 이미 저장된 목록이나 complete-empty 기준은 유지합니다.
- 초기화 이후 새 canonical video ID만 기존 `NEW_SHORT` outbox로 전달합니다. 이미 저장된 쇼츠는 알림 이력이 없어도 자동 backfill하지 않습니다. 기존 영상·watermark·전송 이력을 지우거나 가짜 `SENT`를 만들지 않습니다.
- `shorts_list`와 `video_list` claim은 같은 채널·kind의 더 앞선 `(scheduled_for, id)` 관측이 replay epoch 안에서 유효하고 `PENDING` 또는 `PROCESSING`이면 후속 관측을 선택하지 않습니다. 대기 중인 후속 관측의 attempt는 증가하지 않습니다. 다른 채널과 다른 kind는 이 순서 제약의 대상이 아니며, 기존 retry/lease recovery/dead-letter 정책은 유지합니다.
- 활성 backlog를 한 번 materialize하여 채널·kind별 선행 목록을 선택한 뒤 due 순서로 필요한 queue 행만 잠급니다. 완료 이력을 후보별로 재조회하지 않으며 custom/generic plan에서 전체 backlog 순회와 실제 claim 후보 조회를 따로 제한합니다.
- 관측 순서 제약을 적용하는 첫 배포에서는 기존 YouTube consumer를 drain한 뒤 교체해야 합니다. 이미 구버전에서 claim된 작업까지 새 claim SQL이 재정렬하지는 않습니다. source replay epoch 변경이나 과거 관측 일괄 replay는 배포 절차에 포함하지 않습니다.
- 부분 목록은 삭제·비공개 근거가 아니며 `earliest_complete_effective_at`을 채우지 않습니다. 최초 목록 이전의 관측이나 수집 범위 밖의 영상까지 복구한다는 보장은 하지 않습니다.

## 일반 영상·최초공개 신규성

- `video_list`는 항목별 공개 근거(Publication)를 싣는 generation 2(`VideoListPublicationContractGeneration`)만 받으며, 기준 시각 `earliest_baseline_effective_at`과 항목별 `novelty_pending`으로 신규성을 판정합니다. generation 1 관측은 지원 계약이 아니므로 consume 때 `unsupported_contract`로 DEAD_LETTER가 되며 canonical·replay에 반영하지 않습니다.
- 첫 유효한 비어 있지 않은 목록은 PARTIAL이어도 조용히 기준을 세웁니다. 빈 부분 목록은 기준이 아니며 complete-empty는 기준입니다. 기존 complete 기준은 보존하고 year-1 first-positive clock을 신뢰 가능한 과거 기준으로 추정하지 않습니다.
- 기준 이후 처음 발견한 영상은 신뢰 가능한 게시 시각이 기준 이후일 때만 알립니다. 과거에 게시됐지만 처음 목록에 나타난 영상은 알리지 않습니다. 게시 근거가 없으면 pending으로 저장하며 후속 증거가 확인되면 한 번 알립니다. 이미 알려진 canonical ID·다른 채널/Shorts에서 먼저 저장된 ID는 자동 backfill하지 않습니다.
- 새 미래 최초공개는 검증된 예정 시각으로 신규성만 닫고 발견 시 `NEW_VIDEO`를 만들지 않습니다. 같은 관측으로 확정한 live 세션(`is_premiere=true`)의 분 전·시작 알림이 공개 알림을 소유합니다(`contracts/alarm.md`). 기준 목록에 있던 최초공개와 공개 전환도 알리지 않습니다. 부분 목록은 끝까지 COMPLETE 부재 근거와 구분합니다.
- 요청 상한 안에서 증거를 확보하지 못하거나 목록 범위를 벗어난 영상의 알림 복구를 보장하지 않습니다. 이 정책은 알림 누락을 감수하고 과거 영상의 오알림을 막습니다.

## Collection projection heartbeat와 eligibility

- migration 261부터 `youtube_collection_projection_generations.valid_until`만 projection 만료의 정본입니다. target의 `valid_until`은 삭제합니다. API의 bot LiveQuery·수집 진단, collector acquire·Publish membership은 모두 유효한 CURRENT header를 요구하며, acquire의 bundle 조회 statement도 만료를 다시 검사합니다.
- API는 기존 projection guard를 입력 Build **이전**부터 한 번 배타 잠금합니다. READ COMMITTED에서 대기한 writer는 앞선 commit 이후의 입력을 읽으므로 오래된 Build의 역순 활성화를 막습니다. 조회·검증·저장 실패는 transaction을 rollback하며 validity를 연장하지 않습니다.
- header의 `validity_refreshed_at`은 마지막으로 수락한 supplied refresh 시각입니다. 그보다 최신이거나 PostgreSQL 마이크로초 정밀도에서 같은 시각이면 `valid_until`을 해당 시각+현재 TTL로 대체하여 설정 하향·상향을 모두 반영합니다. 더 과거 시각의 호출만 기존 만료를 보존합니다. migration 이전 시각은 NULL로 남기고 첫 성공 refresh에서 수립하며, 과거 TTL을 추정하지 않습니다. API 재시작 후에도 이 기준은 DB에 유지됩니다.
- `TestCollectionRealPolicyGuardCancellationAndExpiry`와 `TestCollectionRealPolicySharedGuardLoad`는 실제 PolicyBuilder/rosterReader의 10,000 target·영상 1,000개·현재 검토 500개·과거 영수증 15,000개에 독립 collector 공유 guard 조회 4개를 병행합니다. 실제 lock 대기, reader/writer 취소, 대기 중 header 만료 거부와 회복을 검증하고 30회 refresh의 guard/transaction p95·p99를 기록합니다. 이는 해당 DB 경로의 격리 부하이며 외부 provider부터 intent까지의 전체 지연이나 운영 지속 부하를 뜻하지 않습니다.
- `LiveCheckVideos` SQL은 구조적 LIVE/지난 일정·출처 미상 UPCOMING과 현재 원본에 적용되는 검토 영수증(`youtube_live_review_current_receipt`, migration 262) 제외를 cap+1 이전에 적용하고, 같은 statement의 DB 시각과 필요한 사실만 반환합니다. API가 미래/NULL 근거를 거부하고 밀리초 단위 기존 예산으로 positive/availability deadline을 계산합니다. notification/operational/live 입력은 READ COMMITTED의 별도 statement이며 하나의 공통 snapshot이라고 보장하지 않습니다. 지연된 refresh 호출 시각은 기존 header validity를 줄이지 않습니다.
- 구조 identity(subject/kind/priority/cadence/enabled)가 같으면 generation/hash와 연속 `member_since_generation`·`created_at`을 유지합니다. 실제 `not_before` 변경 row만 UPDATE하고 같은 transaction에서 header의 양수 `eligibility_version`을 한 번 올립니다(새 generation 기본값 1). 변경 없는 heartbeat는 header만 쓰며 target/reason 행을 갱신하지 않습니다. 이유만 바뀌면 기존처럼 이유를 교체하되 eligibility version은 바꾸지 않습니다.
- collector는 현재 header·target·lease를 같은 후보 statement에서 확인하는 관계형 조회를 유지합니다. 전체 target/lease를 캐시해 AP에서 정렬하는 안은 전송량 회귀로 채택하지 않았으며 target별 eligibility version도 추가하지 않습니다. 기존 상한 10,000 targets/50,000 reasons/1,000 live 입력, membership·lease/fence·DB slot·원자적 Publish와 기존 bounded retention의 CURRENT/STAGING·lease 보호, collector의 reasons/canonical 접근 금지는 유지합니다.
- IDLE 대상의 관측 due는 scheduler와 같은 `LEAST(next_due_at, scheduled_for + 현재 bundle cadence)`이며 `eligible_at`보다 앞당기지 않습니다. cadence 연장 때 이미 도래한 slot을 늦추지 않고 ACTIVE 만료·DEFERRED 재시도·projection 만료의 의미는 유지합니다.
- header validity 구조는 epoch-3 baseline(`001_schema_epoch3_baseline.sql`)에 포함되어 있으며 별도 선행 migration이 없습니다. 이 구조를 모르는 이전 API·collector binary와 혼합 운용하지 않고, 롤백 경계는 [migration 규약](../../../hololive/hololive-api/scripts/migrations/CONVENTIONS.md)의 epoch-3 적용과 복구 하한을 따릅니다.

## Live canonical 저장과 잠금 순서

- `youtube_live_sessions`·`youtube_live_reconciliation_heads`·`youtube_live_pending_ends`·`youtube_video_availability`의 writer는 `internal/youtube/sourceobservation` 하나입니다. worker·collector의 참조는 읽기 SQL입니다.
- 세션의 status·관측 시각·메타데이터 정책(ENDED 유지, LIVE→UPCOMING 후퇴 방지, 필드별 관측 시각 우선순위, 저장된 채널·`ended_at` 유지)은 `live.Reduce`·`live.FinalizeDue`와 `schedule.Reduce`가 정합니다. 저장은 잠금 안에서 읽은 행과 결과를 비교해 바뀐 행만 씁니다. 읽은 행이 있으면 그 행 전체가 그대로일 때만 적용되는 `UPDATE`(`repository_live_session_update.sql`), 없으면 `INSERT … ON CONFLICT DO NOTHING`(`repository_live_session_insert.sql`)이며 SQL은 값을 병합하지 않습니다. 0행 적용은 `LiveSessionConflictError`로 보고하고 트랜잭션을 되돌리며, YouTube plane은 이를 40001과 같은 재시도 대상으로 분류합니다. 최초공개 분류는 `is_premiere`가 NULL일 때만 채우는 별도 SQL을 유지합니다.
- `0048` head 저장은 18개 mutable 필드 전체의 `IS DISTINCT FROM`으로 같은 근거의 tuple rewrite를 피하며 `updated_at`·review snapshot을 유지합니다. 실제 사실 시계·ignored-absence 변화는 계속 저장합니다.
- `youtube_live_runtime_state`는 원본 트리거가 아니라 writer가 같은 READ COMMITTED 트랜잭션 끝에서 `refresh_youtube_live_runtime_state(text[])`를 한 번 호출해 갱신합니다(migration 328). 관측 writer는 `ReconcileResult.LiveRuntimeVideoIDs`(finalizer는 decision의 세션·pending key)를 넘기며 빈 key는 DB를 호출하지 않습니다. 함수는 READ COMMITTED가 아니면 `live runtime refresh requires read committed transaction`으로 거절하고, 영상 key를 1024개 bucket으로 접은 advisory 잠금을 bucket 순으로 잡은 뒤(같은 영상은 커밋까지 직렬화, 전체 재계산도 잠금 표를 고갈시키지 않음) 문장별 새 snapshot으로 원본을 다시 읽어 행을 지우거나 바뀐 행만 upsert합니다.
- 전역 잠금은 같은 advisory key의 두 모드입니다. 관측 writer는 `lock_youtube_live_runtime_state_shared()`, 증거 retention·수동 영수증(`record_youtube_live_review`)·terminal head 복구·라이브 원본을 바꾸는 data migration은 `lock_youtube_live_runtime_state()`(exclusive)를 원본 잠금보다 먼저 잡습니다. 관측 writer끼리는 원본 행 잠금과 영상 잠금으로만 직렬화되며, 그 사이의 40001·40P01·55P03과 `LiveSessionConflictError`는 YouTube plane의 관측 재시도로 처리합니다.
- 아래 표는 경로별로 잡는 잠금을 앞에서 뒤 순서로 적습니다. "갱신"은 트랜잭션의 마지막 공유 쓰기인 `refresh_youtube_live_runtime_state`이며 영상 잠금을 잡은 뒤에는 다른 행 잠금을 기다리지 않습니다.

| 경로 | 트랜잭션 | 전역 잠금 | 잠금·쓰기 순서 |
|---|---|---|---|
| 관측 소비: `live_snapshot` (`reconcileLive`) | `Repository.Finalize` | `finalizeTx` 첫 문장, shared | 전역 → queue 행 `FOR UPDATE`(claim lock) → advisory `live_subject_head`(subject key) → `youtube_live_sessions` `FOR UPDATE`(payload 영상, 채널의 UPCOMING·LIVE·후보 남은 ENDED; video_id 순) → heads `FOR UPDATE`(video_id 순) → pending ends `FOR UPDATE`(동결 ENDED 제외) → 부재 이력·slot 읽기 → 세션 A → head A → 세션 B → head B(video_id 순) → pending upsert → pending 삭제 → absence slot insert → application insert → queue 완료 → 갱신 |
| 관측 소비: `schedule_snapshot` (`reconcileSchedule`) | `Repository.Finalize` | `finalizeTx` 첫 문장, shared | 전역 → queue 행 → advisory `live_subject_head`(`schedule:` group key) → 요청 영상 세션 `FOR UPDATE`(video_id 순) → 세션 없는 영상의 head `FOR UPDATE` → schedule item upsert → 세션 저장 → application → queue 완료 → 갱신 |
| live check: `video_live_check` (`reconcileVideoLiveCheck`) | `Repository.Finalize` | `finalizeTx` 첫 문장, shared | 전역 → queue 행 → 요청 영상 1개의 세션 → head → pending `FOR UPDATE`(동결 ENDED 제외) → availability upsert → ENDED면 시각 확정 행으로, 아니면 reducer 결과를 세션 → head → pending 순으로 저장 → application → queue 완료 → 갱신 |
| live check: `channel_live_check` | `Repository.Finalize` | `finalizeTx` 첫 문장, shared | 전역 → queue 행 → `youtube_channel_live_checks` upsert. live 표 행은 잠그지 않고 갱신 key도 없음 |
| finalizer (`FinalizeNextDueLiveEnd`) | 단독 | `finalizeNextDueLiveEndTx` 첫 문장, shared | 전역 → due head 1건 조회(잠금 없음, 트랜잭션 시각) → 세션 → head → pending `FOR UPDATE` → due 재확인 → `FinalizeDue` 결과를 세션 → head → pending 순으로 저장 → 갱신 |
| 최초공개 (`mergeContentPremieres`) | `Repository.Finalize`(`video_list`) | `finalizeTx` 첫 문장, shared | 전역 → queue 행 → advisory `content_subject_head` → content 행 잠금·저장 → advisory `live_subject_head`(subject key) → 확정 최초공개 영상 세션 `FOR UPDATE`(video_id 순) → 분류 upsert → 충돌 감사 insert → queue 완료 → 갱신 |
| 시각 확정 (`reconcileEndedVideoTimes`) | `video_live_check`와 같은 트랜잭션 | `finalizeTx` 첫 문장, shared | (video live check의 세션 → head → pending(동결 ENDED 제외) 잠금 뒤) `youtube_live_time_finalizations` `FOR UPDATE` → 한 문장에서 진행 행 → 세션 시각 → head `ended_at` 순으로 저장 → (video live check와 함께) 갱신 |
| 수동 검토 영수증 (`record_youtube_live_review`) | 운영자 READ COMMITTED 트랜잭션 | DB 함수, exclusive | 전역 → 세션 → head → pending → availability `FOR UPDATE` → snapshot CAS → 영수증·사실 insert → 갱신(해당 영상) |
| maintenance: terminal head 복구 (`reconcile-terminal-live-heads.sql`) | 단독 | 스크립트 첫 잠금, exclusive | 전역 → 세션 → head `FOR UPDATE`(video_id 순) → head 갱신 → 갱신(복구 영상) |
| maintenance: 원본 retention (`delete_source_observation_retention_batch`) | 단독 | DB 함수 첫 문장, exclusive | 전역 → retention 커서 singleton `FOR UPDATE` → 원본 후보 잠금·삭제(FK `SET NULL`·`CASCADE`). 수명 모델 입력이 아니므로 갱신 없음 |
| maintenance: queue·collision·payload·absence slot·checkpoint retention | 단계별 단독 | 없음 | 각 표의 제한된 DELETE. 라이브 원본 표를 쓰지 않음 |
| claim (`ClaimBatch`) | 단독 | 없음 | due queue 행 `FOR UPDATE SKIP LOCKED`(replay epoch 이전·시도 소진 후보와 선점 후보) → 같은 트랜잭션에서 종료 후보 DEAD_LETTER 집합 갱신 |

- migration 328은 기존 guard를 잡은 뒤 316·325의 원본 문장 트리거, `refresh_youtube_live_runtime_state_statement()`, `guard_youtube_live_runtime_state()`, `youtube_live_runtime_state_guard` 표를 제거하고, 잠금 함수 본문을 advisory로 바꾸며 기록 함수를 READ COMMITTED 요구로 다시 정의합니다. runtime 역할은 수명 모델 표 SELECT만 유지하고 refresh·shared 잠금 EXECUTE를 받습니다. 기록 함수 실행 권한이 있는 역할은 refresh EXECUTE를 받고 scraper는 shared 잠금 권한이 없습니다. 마지막에 기존 모델·세션·head의 전체 key로 재계산해 drift 0에서 시작합니다. 328 이후 신설하는 기록 역할에는 기존 의존 권한에 더해 `refresh_youtube_live_runtime_state(text[])` 실행 권한도 부여합니다.
- writer가 명시적으로 갱신하는 이 구조는 epoch-3 baseline(`001_schema_epoch3_baseline.sql`)에 포함되어 있으며 별도 선행 migration이 없습니다. 원본 트리거가 없으므로 명시적 갱신을 모르는 이전 API 이미지로 되돌리면 수명 모델이 갱신되지 않습니다. 이미지만 되돌리는 rollback은 지원하지 않으며 복구는 forward-only입니다. 정합성은 [API runbook](../runbooks/hololive-api.md#db-poolyoutube-plane-db-슬롯-지표)의 `live_runtime_state_drift.sql` 점검으로 확인합니다.
- 수동 검토 영수증은 READ COMMITTED 트랜잭션(psql 기본)에서 기록합니다. 기존 `BEGIN ISOLATION LEVEL SERIALIZABLE` 절차는 `live review requires read committed transaction`으로 거절됩니다.

## Observation storage and retention

- 채널 수치 통계·구독자 수 명령·통계 알림은 제거합니다. 채널 profile/photo, 방송·일정과 알림 구독은 유지합니다.
- 각 successful collection slot의 관측은 독립 저장합니다. payload만 kind/schema/canonical profile과 전체 32바이트 SHA-256으로 공유하며, JSONB의 PostgreSQL LZ4 압축을 사용합니다. identity와 외부 hex hash 계약은 바꾸지 않습니다.
- claim은 같은 SQL에서 payload를 조회합니다. 참조 누락·kind/schema 불일치·hash 손상은 오류이며 구 저장 경로로 되돌아가지 않습니다. GC는 참조가 없는 payload만 잠금 후 새 snapshot으로 확인하여 제한된 수만 삭제합니다.
- claim SQL은 due 행 중 replay epoch 이전 자동 claim과 시도 소진 행을 다른 선점 후보와 함께 잠그기만 합니다. DEAD_LETTER 여부와 사유(`replay_epoch_expired`가 `attempts_exhausted`보다 우선)는 Go 재시도 정책이 정하고, 같은 claim 트랜잭션의 집합 UPDATE 한 번으로 claim 트랜잭션 시각·NULL 상세와 함께 기록합니다. claim 시점 DEAD_LETTER는 지표를 남기지 않습니다.
- application은 entity별 멱등·CANONICALIZED 결과를 한 INSERT로 저장하며 원본의 `ON DELETE CASCADE` 수명을 따릅니다. 고아 유예·별도 감사 retention은 없습니다. provider/kind/subject/hash/effective clock은 원본에서 읽고 감사 행에 중복 저장하지 않습니다.
- 원본·live/content absence slot의 scope/evidence SHA-256은 32바이트 `bytea`로 저장합니다. publisher/claim/reducer 경계에서는 기존 소문자 hex 표현과 동일한 identity·dedup 비교를 유지합니다.
- 원본 retention은 큐·종료 근거, CURRENT/STAGING generation, ACTIVE 수집 lease, LIVE/UPCOMING 세션별 최신 application 근거와 pending-end 근거를 보호합니다.
- migration 315의 원본 정리는 보호 판정 전에 최대 1,000개 ID를 materialize합니다. kind별 인덱스 후보 반환은 최대 1,000개(정책 최대 16개), 최초 보호 검사는 합계 최대 1,000개이며 삭제·최종 보호 재검사는 `BatchSize`(최대 1,000) 이내입니다. 이 상한은 물리 I/O·보호 함수 내부의 자식 조회량·FK 쓰기량·실행 시간의 상한이 아닙니다.
- `(received_at,id)` 진행 커서와 고정된 순회 상한을 DB singleton에 보존합니다. 새 유입이 계속되어도 순회 끝에서 앞쪽을 재방문하고, 잠긴 후보는 건너뛴 뒤 다음 순회에서 다시 봅니다. 모든 kind가 같은 시간/ID 순서를 공유하며 보호 해제·늦게 삽입된 앞쪽 원본도 재방문합니다. 커서와 원본/application 삭제는 같은 transaction으로 확정·롤백하므로 재시작 때 처음부터 훑지 않습니다.
- DB 함수가 exclusive live 전역 잠금 → 커서 → 원본 잠금을 소유합니다. 전부 보호된 창도 성공한 tick이며 payload·absence·checkpoint 정리는 계속 실행됩니다. 앞 단계의 별도 commit 수량과 `BacklogKnown=false`의 미확정 의미는 유지합니다. runtime·scraper는 커서 표에 직접 접근하지 않습니다.
- publish·claim·application SQL이 요구하는 provenance·어휘 사전과 bytea 키는 epoch-3 baseline(`001_schema_epoch3_baseline.sql`)에 포함되어 있으며 별도 선행 migration이 없습니다. 이 구조를 모르는 구 binary나 이전 migration runner를 같은 DB에 섞어 실행하지 않습니다. 감사 표의 물리 공간 회수(`VACUUM FULL`)에는 별도 점검 창·백업·용량 확인이 필요하며 로컬 검증은 운영 DDL·배포 승인이 아닙니다.
- RETIRED projection 기본 보존은 2일이며 reasons+targets 합계를 batch 상한 이하로 제한하고 CURRENT/STAGING, lease 참조 generation을 보호합니다. 호출 timeout은 8초입니다.
- 운영 cutover와 복구 경계는 [API runbook](../runbooks/hololive-api.md#youtube-관측-저장-구조-전환)을 따릅니다. 소스 기본값 변경은 운영 TTL 적용 완료를 뜻하지 않습니다.

## Startup requirements

- Iris URL/cert/token configuration
- PostgreSQL and Valkey availability
- `ALARM_INTERNAL_URL` and API key configuration for the alarm-worker client
- Internal HTTP/H3 client options (alarm-worker client) are loaded once into `BotPlaneConfig.InternalH3` and `AdminPlaneConfig.InternalH3` and passed explicitly; each plane owns timeout and transport cleanup. Iris URL-file dynamic reload remains on its existing path.
- The process lifecycle uses the same shared-go `bootstrap` and `lifecycle.Run` assembly as youtube-collector and alarm-worker. It is not an operator-selectable mode and does not change ports, routes, config keys, or dependency readiness requirements.

## Shutdown behavior

- 통합 `Runtime.Run`(shared-go `lifecycle.Run`)이 process signal의 유일한 소유자입니다. 조립은 process 공유 자원(`buildAPIInfrastructure`) → bot → admin → YouTube plane 순서이며, 뒤 plane은 앞에서 만든 in-process 인스턴스를 빌립니다. YouTube plane은 worker profile의 `source_observation` executor가 꺼져 있어도 항상 만들고 시작하며, 이때는 영속 큐 표본 수집만 돌립니다. 시작은 YouTube → admin → bot 순서이고, 종료는 runtime context를 취소한 뒤 bot → admin → YouTube 순서로 drain합니다.
- Stop HTTP/H3 ingress and scheduler workers within one shared 10-second plane-drain budget. Drain plus plane cleanup is a hard 30-second cap: as with the former Fx stop timeout, a plane that ignores cancellation is abandoned at the deadline and the bootstrap `Close` does not re-join unfinished cleanup. Only the telemetry flush (at most 5 seconds) and logger close follow, inside the unit's 45-second `StopTimeout`.
- Plane `CloseContext` receives the remaining process-stop context. Each plane joins its background tasks before releasing the resources it owns (the YouTube plane also closes its `youtube_plane` pool); bot durable samplers are included. The shared Valkey client, member cache and bot·admin pools are released once afterwards by the process assembly (see "Plane별 공유 자원"). A join timeout preserves live resources and the error; the process then exits without re-joining or starting concurrent cleanup, and cleanup that has not started by the stop deadline is not started.
- YouTube claim release waits for the claim producer to finish registering its returned batch. Registration and fenced release share the existing settlement budget; if registration times out, a later `CloseContext` can finish that release. An attempted release failure is retained without automatic retry, and DB cleanup still waits for all workers to join.
- Bot readiness cancellation is normal only when its error matches the runtime's actual shutdown cancellation. An independent readiness deadline or a joined operational error remains fatal.
- A runtime fatal, plane-drain failure, or process-stop timeout remains process-fatal. Cleanup is attempted once and no legacy lifecycle fallback is selected.
- Do not drain or mutate dispatch queues during shutdown.
- Preserve delivery/outbox state in PostgreSQL.

## Observability

- Logs: `sudo journalctl -u hololive-api.service -f`
- Health: `https://127.0.0.1:30001/health`, `https://127.0.0.1:30006/health` through container `./bin/healthcheck`
- Ready: `https://127.0.0.1:30001/internal/ready` through container `./bin/healthcheck --api-key-env API_SECRET_KEY`
- Metrics: bot plane이 `HOLOLIVE_METRICS_ADDR`에 평문 HTTP/1.1 `/metrics` 리스너를 엽니다(값이 비면 열지 않음). admin plane은 metrics 리스너를 두지 않습니다. 운영 주소·인증과 조회 명령은 [API runbook](../runbooks/hololive-api.md#go-runtime-metrics-prometheus)을 따릅니다.

## Related documents

- Project Map: `../PROJECT_MAP.md`
- Contract Map: `../CONTRACT_MAP.md`
- Process trust domain: `../architecture/hololive-api-trust-domain.md`
- Runbook: `../runbooks/hololive-api.md`
