# 멤버 주간 일정 포스터 반자동 등록 (`!일정` 보강)

## 목표

X에 올라오는 멤버 주간 일정 포스터를 1시간마다 찾아 후보로 만들고 Iris Console 앱으로 푸시한다. 반자동 구조이다. 관리자가 앱에서 원본 포스터를 보고 일정 포스터가 맞으면 확정하고, 아니면 거절한다. 확정하면 그 포스터를 이미지 생성 모델(나노바나나 2.6)로 한국어 이미지로 번역하고, 번역이 끝난 이미지가 그 주의 일정으로 `!일정 <멤버>` 응답에 붙는다. 번역은 확정한 포스터에만 하므로 거절할 후보에 비용을 쓰지 않는다. 첫 대상은 소라시나 소피아이다(채널 `UCROQtXcp2loQEmvpe5rhJzQ`, X `2054825907916054528`). 정확도가 충분히 확인되면 나중에 자동 확정으로 바꿀 수 있다.

## 확인된 사실 (2026-11-11)

### 출처
- 소피아의 포스터는 X에만 있다. 운영 `youtube_community_posts`의 소피아 행은 9/26 4건뿐이고 일정 키워드가 없다.
- 이번 주 포스터 게시물은 `2119784604752343207`이다. 본문에 `週間スケジュール`와 날짜 범위가 있고, 사진은 `pbs.twimg.com` 원본 1920×1080(약 390KB)이며 인증 없이 받을 수 있다. 게시 46시간 만에 첫 페이지 밖으로 밀려났다.

### FxTwitter
- `GET https://api.fxtwitter.com/2/profile/id:<x_user_id>/statuses`는 인증 없이 20건씩 돌려준다(`count=100`도 20건). `cursor.bottom`으로 페이지를 넘긴다.
- 응답에는 리포스트(`reposted_by`)와 본인 답글(`replying_to`)이 섞인다.
- 공식 문서상 v2 제한은 IP당 분당 1000회이고, SLA와 봇 사용 약관은 없다.

### 운영 호스트(hololive-seoul)
- `api.fxtwitter.com`과 `pbs.twimg.com` 모두 200으로 응답한다.
- hololive-api 컨테이너의 출구는 firewalld 정책 `podman-egress`(`egress-zones: ANY`, ACCEPT)를 따른다.
- 운영 `pg_default_acl`은 `hololive_migrator`가 만든 테이블에 `hololive_runtime=arwd`를 부여하므로 GRANT 문이 필요 없다.

### LLM
- 이 계획을 쓸 때 운영 설정은 `LLM_PROVIDER=gemini`, `GEMINI_MODEL=gemini-4-argon`이었고 `llm.Client`는 텍스트 전용이었다. 둘 다 llm plane과 함께 삭제했다.
- Gemini Interactions API의 이미지 입력은 `input` 배열에 `{"type":"image","data":<base64>,"mime_type":"image/jpeg"}`를 넣는 방식이다. 인라인 요청 전체는 20MB 이하여야 한다.
- hololive의 기존 `GEMINI_API_KEY`(`AQ.` 키)는 만료되었다. Gemini API와 Vertex express 모두 401(`ACCESS_TOKEN_TYPE_UNSUPPORTED`)을 반환한다.
- ChatBotGo의 Vertex ADC로는 정상 동작한다. 서비스 계정 파일은 `/etc/stack-secrets/chatbotgo/vertex/adc.json`이고, 설정은 `GOOGLE_CLOUD_PROJECT`, `location=global`이다. `POST https://aiplatform.googleapis.com/v1/projects/<project>/locations/global/publishers/google/models/gemini-4-argon:generateContent`에 이미지(`inlineData`), `responseSchema`, `thinkingLevel: LOW`를 함께 보내 200을 받았다(2017 토큰). 결과는 포스터 7행과 일치했다. 다만 `熱千めら`를 `熱干めら`로 읽은 글자 오인식이 1건 있었다.
- ChatBotGo는 `google.golang.org/genai` v1.77.0(`genai.BackendVertexAI`)을 쓴다. hololive-api에는 Google SDK와 oauth2 의존성이 없다.
- 위 텍스트 추출 시험은 글자 오인식이 남았고, 추출한 표를 관리자가 고쳐야 했다. 그래서 2026-11-11에 사용자가 텍스트 추출 대신 나노바나나 2.6으로 이미지를 한국어로 번역하는 방식을 택했다. 원본의 배치·사진·색을 유지한 채 글자만 한국어로 바꾼 이미지를 만드는 것이 목표이다.
- 2026-11-11 공식 문서(Gemini Enterprise Agent Platform 모델 표)에는 "Nano Banana 2.6"이 없고 최신은 `gemini-nano-banana-2.1`(GA 2026-10-06, `global`, 이미지 입출력, 1K/2K/4K, 출력 2K 이미지 약 $0.05)이다. 모델 ID는 설정 파일의 `translation.model`로 바꿀 수 있고 기본값은 2.1이다.
- 2026-11-11에 llm plane과 기존 LLM 기능(행사 알림, 멤버 뉴스)을 모두 삭제했다(`feat/remove-llm-features`). migration 009가 그 DROP을 맡으므로 이 기능은 010을 쓴다. `schedulerkit`·`executionlock`도 함께 삭제했다.

### hololive-api admin plane
- `/api/holo` 아래 route는 `internal/planes/admin/internal/httpapi/routes.go`와 `registration.go`에 등록한다. 인증은 `X-API-Key`이다.
- 비밀번호 재확인, Origin 검사, mutation ID 단회 사용은 Console gateway가 계약의 `mutation: true`를 보고 적용한다.
- 낙관적 동시성 선례는 두 가지다. X 세션은 `expectedRevision`으로 충돌을 처리하고, 방 설정은 `409 + code`를 쓴다.
- 여러 plane이 함께 쓰는 저장소는 `hololive-shared/pkg/service/xspaces`처럼 shared에 둔다.

### Iris Console gateway
- push monitor는 30초마다 관측한다. X 스페이스 인증 만료(`X-spaces`) 선례가 있다(`internal/push/monitor.go`, `store.go`).
- 관측 키는 8개가 상한이고 지금 7개를 쓴다. 이름과 값은 각 32자 이하이다.
- 알림 payload는 `{kind, body}`이다.
- 계약 generation이 바뀌면 gateway와 APK를 함께 배포해야 한다. 세대가 다르면 409 `CLIENT_GENERATION_MISMATCH`이다.

### Iris Console Android 앱
- 외부 이미지는 `ExternalImage.kt`가 허용 호스트 목록으로 직접 로드한다. 지금 `pbs.twimg.com`은 목록에 없다.
- 화면은 `ConsoleRoute`, `ReadKey.hololive`, `rememberHoloMutationRunner`, `rememberDraft` 패턴을 쓴다.
- 푸시는 `PushPayload.parse`가 `kind`별로 `PushChannel`과 `NotificationDestination`을 고른다.

## 설계

### 흐름

```
admin plane (1시간) ─ FxTwitter 조회 ─ 후보 필터 ─ pending_review 저장(원본 이미지 URL만)
                                                                    │
Console push monitor (30초) ─ GET /api/holo/schedule-posters/summary ┘ ─ 새 후보 → FCM "schedule-poster"
                                                                    │
Android 목록 → 상세(원본 포스터) → 확정(주 범위 입력)/거절 (mutation: reauth)
                                                                    │
admin plane ─ revision 조건부 UPDATE ─ confirmed + translation_status=pending
                                                                    │
admin plane 번역 대기열(1분) ─ 원본 이미지 → 나노바나나 2.6 → 번역 이미지 저장(succeeded)
                                                                    │
bot plane `!일정` ─ 확정 + 번역 성공인 이번 주 포스터의 번역 이미지를 덧붙임
```

### 저장 (hololive-bot)

- migration `010_member_schedule_posters.sql`로 새 테이블을 만든다(멱등, manifest 등록, golden snapshot 갱신). GRANT는 쓰지 않는다.
  - 식별: `post_id text PK`, `channel_id varchar(64)`, `x_user_id text`, `post_url text`, `image_url text`, `posted_at timestamptz`, `text_excerpt text`(본문 앞 500자)
  - 검토: `review_status text CHECK (pending_review|confirmed|rejected|expired)`
  - 번역: `translation_status text CHECK (not_requested|pending|succeeded|failed)`, `translation_error text`, `translated_image bytea`(JPEG, 4MiB 이하), `translated_mime text`, `translated_at`. 확정 전에는 `not_requested`이고, `review_status=confirmed`일 때만 `pending` 이후 값을 가질 수 있게 CHECK로 묶는다
  - 확정: `week_start date`, `week_end date`(관리자가 확정 때 입력), `reviewed_by text`, `reviewed_at timestamptz`
  - 기타: `revision bigint`, `created_at`, `updated_at`
  - 인덱스: 기본 키만 둔다. 60일 보존으로 행이 수백을 넘지 않고, migration 규약상 index는 `CONCURRENTLY` 단일 문장 파일로만 만들 수 있다.
- 번역 이미지는 Iris 발송과 앱 표시를 위해 DB에 보관한다. 크기 상한은 구현 때 모델 출력 크기를 보고 정한다(원본 약 390KB).
- 저장소 패키지는 `hololive-shared/pkg/service/scheduleposter`에 둔다. admin plane이 후보를 넣고, 확정·거절과 번역 결과를 기록하며, bot plane이 번역까지 끝난 확정본을 읽는다. `xspaces`와 같은 구조이다.
- 보존: 수집 tick마다 두 가지를 처리한다. 8일이 지난 `pending_review`는 `expired`로 바꾸고, `week_end` 또는 `created_at`이 60일 지난 행은 100건씩 지운다.

### 수집과 번역 (admin plane)

- llm plane을 삭제했으므로 admin plane에 둔다. admin plane의 PhotoSync가 같은 방식(주기 실행, 외부 조회, DB 갱신)의 선례이고, hololive-api는 중앙에서 한 인스턴스만 돈다.

- **설정**: `SCHEDULE_POSTER_CONFIG_FILE`에 `{"poll_seconds":3600,"targets":[{"x_user_id","channel_id"}]}` 형식으로 둔다. 로더 규칙은 worker의 `xspaces/config.go`를 따르고, 경로가 비어 있으면 기능을 끈다.
- **주기**: PhotoSync처럼 admin plane runtime이 1시간마다 돌린다. 시작·종료 join은 admin plane 수명에 묶는다.
- **조회**: 대상별로 마지막 성공 조회 시각을 저장한다. 평소에는 `since=<마지막 성공 시각>`으로 첫 페이지만 요청한다. 새 글이 없으면 204가 오는 것을 확인했다. 처음 실행하거나 마지막 성공이 1일보다 오래되었으면 `cursor.bottom`으로 페이지를 넘긴다. 멈춤 판단은 글 하나 단위가 아니라 페이지 단위로, 본인 원글만 보고 한다. 페이지 안 본인 원글이 모두 8일보다 오래되었거나 5페이지에 이르면 멈춘다. 응답은 시간순이 아니고 오래된 리포스트가 포스터 바로 앞에 끼어 있었기 때문이다. 이 페이지 구성을 fixture로 둔다.
- **후보 필터**: 본인 원글(리포스트·답글 제외)이면서 사진이 있고, 본문이 `週間スケジュール|スケジュール|schedule|予定|weekly`에 맞는 글이다. 사람이 확정하므로 넓게 잡는다. 확정하고 거절한 기록이 쌓이면 그것으로 조건을 조정한다. 사진이 여러 장이면 첫 장을 쓴다.
- **HTTP**: shared-go `netguard`+`httputil`로 만든다.
  - 허용 호스트는 `api.fxtwitter.com`과 `pbs.twimg.com`이고, https/443만 쓰며 redirect는 2회 이하이다.
  - 응답은 JSON 2MiB, 이미지 8MiB로 제한하고 이미지는 jpeg/png/webp만 받는다.
  - User-Agent에는 서비스 이름만 쓴다.
- **번역 시점**: 수집 단계에서는 번역하지 않는다. Console에서 확정한 행만 `translation_status=pending`이 되고 번역 대기열이 처리한다.
- **번역**: `google.golang.org/genai`의 Vertex backend로 나노바나나 2.6에 원본 이미지(확정 시점에 다시 받음)와 번역 지시를 보낸다. 지시에는 배치·사진·색 유지, 일본어 글자만 한국어로 교체, 멤버 이름은 한국어 표기 사용, 시각은 원문 그대로를 넣는다. 응답이 이미지가 아니거나 형식·크기가 허용 범위를 벗어나면 실패로 본다. 확정 1건당 최대 3회 시도하고, 모두 실패하면 `translation_status=failed`로 둔다. 이때 `!일정`에는 붙지 않고, 관리자가 앱에서 재번역을 요청할 수 있다. 모델 장애는 그 포스터만 막고 `!일정`의 Holodex 일정은 그대로 나간다.
- **번역 대기열**: 1시간 tick과 별도로 1분마다 `translation_status=pending` 행을 5개까지 처리한다. 대상은 새 확정과 재번역 요청 모두이므로, 확정 뒤 1분 안에 번역이 시작된다. 재번역 예약은 이전 번역본을 지운다.
- **주 범위**: 관리자가 확정할 때 원본을 보고 `weekStart`·`weekEnd`를 입력한다. 앱은 `posted_at`(JST) 기준 이번 주 월~일을 기본값으로 채운다. 기간이 7일을 넘거나 `posted_at`과 7일 넘게 떨어진 범위는 서버가 거절한다.
- **번역 품질 위험**: 이미지 생성 모델은 글자를 잘못 그리거나 없는 내용을 만들 수 있고, 번역본은 확정 뒤 사람이 다시 보지 않고 게시된다. 그래서 구현 전에 소피아 포스터 몇 장으로 시각·이름·콜라보 표기가 정확한지 먼저 시험한다(작업 순서 0). 게시된 번역본이 틀렸으면 관리자가 앱에서 재번역하거나 확정을 취소(거절)할 수 있다.

### 관리 API (admin plane)

| 메서드 | 경로 | 용도 |
|---|---|---|
| GET | `/api/holo/schedule-posters/summary` | `{pendingCount, latestCandidateId}`. push monitor용. 집계 대상은 `pending_review`이다. `latestCandidateId`는 확정·거절 여부와 무관하게 가장 늦게 만든 후보이므로 뒤로 가지 않는다 |
| GET | `/api/holo/schedule-posters?status=` | 목록. `pending_review`(검토 대기), `confirmed`(번역 상태 포함) |
| GET | `/api/holo/schedule-posters/{id}` | 상세(원본 이미지 URL, 원 게시물, 검토·번역 상태, revision) |
| GET | `/api/holo/schedule-posters/{id}/translated` | 번역 이미지 `{id, mimeType, data(base64)}`. gateway가 JSON만 중계하므로 base64로 담는다. 번역 성공 행만 200 |
| POST | `/api/holo/schedule-posters/{id}/confirm` | mutation. `{expectedRevision, weekStart, weekEnd}`. `pending_review` 행을 확정하고 번역을 예약한다 |
| POST | `/api/holo/schedule-posters/{id}/reject` | mutation. `{expectedRevision}`. 검토 대기 행의 거절과, 이미 게시된 확정본의 취소에 함께 쓴다 |
| POST | `/api/holo/schedule-posters/{id}/translate` | mutation. 확정 행의 재번역 예약(`translation_status=pending`) |

- 충돌은 방 설정 선례처럼 `409 {"code":"revision_conflict"}`, 허용하지 않는 상태 변경은 `409 invalid_state`, 주 범위 규칙 위반은 `409 invalid_week`로 돌려준다(gateway가 중계하는 오류 상태에 422가 없다).
- producer 계약은 `docs/current/contracts/admin-api.openapi.json`과 `internal/planes/admin/runtime/openapi_contract_test.go`에 추가한다. `CONTRACT_MAP.md`에도 행을 추가한다.

### `!일정` 표시 (bot plane)

- 오늘(JST)이 `week_start`~`week_end` 안에 들고 번역이 성공한 확정본 중 가장 최근 것을 고른다. 확정했지만 번역 중이거나 실패한 포스터는 붙이지 않는다.
- Holodex 일정 텍스트 뒤에 번역 이미지만 bot의 기존 이미지 발송 경로(`SendImage`)로 보낸다. 안내 문구·템플릿은 두지 않는다.
- Holodex 조회가 실패해도 확정본이 있으면 포스터만 보낸다. 포스터 조회나 이미지 발송이 실패하면 로그만 남기고 기존 동작을 유지한다. Iris 전달 결과 불명은 중복 발송을 막기 위해 명령 오류로 올린다.

### Iris Console

- **gateway**
  - push monitor에 관측 키 `Poster`를 추가한다. 값은 `pendingCount>0`일 때 `p:<latestCandidateId>`, 아니면 `none`이다. 이로써 키는 상한인 8개가 된다.
  - 변경 규칙은 값이 `p:`로 시작하면서 이전 값과 다를 때만 알리는 것이다. 후보 A와 B가 대기 중일 때 B를 처리해도 값이 A로 되돌아가지 않으므로 다시 알리지 않는다.
  - 앱 이미지 로더는 긴 변을 1280px로 줄인다. 1920px 포스터에서 작은 콜라보 글자가 읽히는지 화면에서 확인하고, 읽히지 않으면 상세 화면에서만 확대 디코딩을 허용한다.
  - 번역 이미지는 admin API를 gateway가 중계해 앱에 보낸다. 외부 호스트 직접 로드가 아니므로 `ExternalImage` 허용 목록과 별개이다.
  - payload는 `{kind:"schedule-poster", body:"새 일정 포스터 후보 N건"}`이다.
  - 계약은 producer 반영 뒤 `contracts/hololive`를 재생성한다. generation이 바뀐다.
- **Android 앱**
  - route는 `hololive/schedule-posters`(목록)와 `hololive/schedule-posters/candidate`(상세)이다.
  - `HololiveOperation`에 확정, 거절, 재번역을 추가한다.
  - `PushChannel`과 `NotificationDestination`은 목록의 끝에 추가한다. 순서가 PendingIntent requestCode이기 때문이다.
  - `ExternalImage`에 `pbs.twimg.com` 정확 호스트를 추가한다. 기존 2MiB 본문 상한은 포스터(약 390KB)에 충분하다.
  - 검토 대기 상세 화면은 원본 포스터를 보여 주고, 주 범위를 `rememberDraft`로 입력받아 확정하거나 거절한다. 확정은 기존 mutation 경계(비밀번호 재확인)를 따른다.
  - 확정 목록과 상세 화면은 번역 상태(대기·성공·실패)와 번역 이미지를 보여 주고, 재번역과 확정 취소를 제공한다. 번역은 확정 뒤 1분 안에 시작되므로 상세 화면은 번역 중일 때 상태를 다시 읽는다.

## 작업 순서

0. [ ] **선행 확인**: 나노바나나 2.6의 Vertex 모델 ID·입출력 형식·단가를 공식 문서로 확인하고, ChatBotGo ADC로 소피아 포스터 1장을 번역해 품질과 비용을 본다. 이미지 생성 비용은 앞서 승인한 텍스트 추출 비용과 다르므로 결과와 예상 월 비용을 보고하고 진행 여부를 확인한다.
1. [ ] **선행 (stack-platform-ops)**: ChatBotGo의 Vertex 서비스 계정 파일을 hololive-api에도 배치한다(stack-secrets manifest, `/run/hololive-bot/vertex/adc.json` 읽기 전용 mount, `GOOGLE_CLOUD_PROJECT`, `GOOGLE_CLOUD_LOCATION=global`). 만료된 `GEMINI_API_KEY`는 운영 env에서 제거한다(코드는 이미 읽지 않는다). 모델 호출은 `google.golang.org/genai`(ChatBotGo와 같은 버전)의 Vertex backend를 쓰는 것으로 정했다(2026-11-11 승인).
2. [ ] hololive-bot worktree(`tools/ops/stack-worktree.sh add <branch> hololive-bot`)를 만든다. `feat/remove-llm-features`가 병합된 main을 base로 한다.
   1. genai Vertex client와 번역 호출, 응답 검증 테스트.
   2. migration 010, shared `scheduleposter` 저장소, dbtest.
   3. 설정 로더, FxTwitter와 이미지 client, admin plane 수집기와 번역 대기열(fixture 테스트: 필터, 페이지 중단, 크기와 호스트 제한, 확정 전 행은 번역하지 않음).
   4. admin API, producer 계약과 계약 테스트.
   5. bot 이미지 표시와 안내 템플릿, `!일정` 테스트(번역 성공 확정본 있음, 확정했지만 번역 전·실패, 없음, Holodex 실패, 이미지 발송 실패).
   6. 문서: 서비스 문서, `CONTRACT_MAP.md`, 설정 예시.
3. [ ] iris-console 확장을 hololive-bot 작업과 연계해 같은 작업 단위로 진행한다. `tools/ops/stack-worktree.sh add <branch> hololive-bot iris-console`로 같은 브랜치 이름의 worktree를 함께 만든다.
   1. hololive admin 계약(2-4)이 확정되면 바로 `contracts/hololive`를 재생성하고 gateway route를 붙인다. 번역 이미지(`/translated`)는 JSON이 아닌 바이트 응답이므로 gateway가 이진 응답을 중계할 수 있는지 먼저 확인하고, 없으면 그 경로를 이 작업에서 추가한다.
   2. push 관측 키 `Poster`, Android 목록·상세 화면, 원본 이미지용 `ExternalImage` 호스트(`pbs.twimg.com`)를 추가한다.
   3. gateway `go test`, Android 단위 테스트와 화면 렌더링을 확인한다. hololive-bot 쪽 dbtest·계약 테스트와 함께 실제 흐름 검증을 한 번에 한다.
4. [ ] 배포 순서를 지킨다.
   1. hololive-api: migration 010, 설정 파일 배치, 재배포. 엔드포인트 추가만 하는 변경이라 기존 Console은 영향받지 않는다.
   2. Console gateway와 APK를 함께 배포한다(generation 일치). hololive-api 배포 직후 같은 릴리스 창에서 진행해, 후보 알림이 먼저 생겼는데 앱에 화면이 없는 기간을 두지 않는다.

## 검증

- **hololive-bot**: `./build-all.sh --build-only --no-bump`, 영향받은 모듈의 `go test -race`, NilAway, golangci-lint, dbtest, `scripts/ci/pre-push-gate.sh`.
- **iris-console**: 계약 생성 테스트, gateway `go test`, Android 단위 테스트와 화면 렌더링 확인.
- **실제 흐름**: 소피아 게시물로 후보 생성, 푸시 수신, 앱에서 확정, 번역 완료, `!일정 소피아`의 번역 이미지 표시까지 한 번 끝까지 확인한다. 같은 후보를 두 번 확정할 때 409가 나는지도 확인한다.

## 승인 상태

- 2026-11-11 사용자가 FxTwitter 런타임 의존, Gemini 비용, Holodex 실패 시 포스터 단독 표시, `google.golang.org/genai` 추가를 승인했다. 이미지 생성 모델의 비용은 선행 확인 뒤 따로 확인한다.
- 운영 반영(migration, 설정 파일, hololive-api 재배포, Console과 APK 배포)은 각 ops 절차에서 대상별로 진행한다.

## 남은 문제

- 나노바나나 2.6의 모델 ID·단가·출력 품질은 아직 확인하지 않았다(작업 순서 0).
- 반자동 검토는 잘못 잡힌 후보는 거르지만, 놓친 포스터는 잡지 못한다. 키워드 필터는 소피아 한 명의 포스터로만 확인했고, 멤버 전체 패턴 조사는 진행하지 않았다.
