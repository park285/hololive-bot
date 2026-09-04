# Hololive 알림 Karing egress 정합화 계획

## Execution capsule
**Goal:** 일반 채팅에서 Markdown에 의존하던 Hololive 알림을 지원되는 Karing content-list 경로로 일원화하고, 실제 Kakao handoff가 확인된 뒤에만 발송 성공을 기록합니다.
**Context:** alarm-worker에는 Karing 구현이 이미 있지만 두 opt-in 환경변수 때문에 비활성화되어 있고, 현재 sender는 Iris의 `202 Accepted`를 최종 전달 성공으로 취급합니다. 일반 텍스트 알림에는 open-chat Markdown 분기도 남아 있습니다.
**Constraints:** 기존 Karing template ID와 변수 계약, YouTube lifecycle fencing, replica 1, 기존 `/l/*` 링크 호환성을 유지합니다. Karing 실패를 일반 텍스트로 fallback하거나 outcome unknown을 재발송하지 않습니다.
**Evidence:** Karing 계약은 YouTube content-list template 1~4개를 정의하고 Iris live send는 `requestId`를 반환한 뒤 `/reply-status/{requestId}`의 `handoff_completed`를 요구합니다. Twitch/Chzzk-only와 축하·digest·milestone·generic 알림은 현 template으로 표현할 수 없습니다.
**Success:** YouTube 주소가 있는 일반 방송·영상·Shorts·community 알림은 항상 Karing을 사용하고, 나머지는 Markdown이 아닌 일반 텍스트를 사용합니다. 세 퇴역 환경변수와 새 short-link 생성은 사라지며 전달 실패·불명확 상태는 성공으로 기록되지 않습니다.
**Output:** alarm-worker sender·routing·설정 정리, 전달 확인과 회귀 검증, 계약·runbook 정합화, 운영 승인 경계와 무중단이 아닌 명시적 maintenance rollout 절차를 남깁니다.

## Plan controls

**Decisions:** `DEC-20260904-hololive-karing-canonical-notification-egress` (governing), `DEC-20260831-hololive-youtube-egress-lifecycle-transition-ownership` (constraint), `DEC-20260730-hololive-alarm-egress-lease-removed` (constraint), `DEC-20260517-iris-karing-runtime-capability` (context)

- 기준 source revision은 `hololive-bot@69b2a76b5473a568eda2672e17e6068888a85fdf`입니다. 실행 직전에 branch, HEAD, worktree, active Git writer와 lock을 다시 확인하고 기준이 달라졌으면 연결 경로와 계약을 재검토합니다.
- 깨진 불변조건은 `Iris가 Karing 요청을 queue에 수락한 사실과 Kakao로 handoff한 사실을 구분해야 한다`입니다. 현재 `SendKaringContentList`는 `202 Accepted` 응답 직후 성공을 반환하여 alarm dispatch와 YouTube outbox가 너무 일찍 `sent`를 기록할 수 있습니다.
- Karing template mapping은 `1 -> 133266`, `2 -> 133223`, `3 -> 133222`, `4 -> 133267`을 유지합니다. Template 변수, 최대 네 항목, chunk 순서와 deterministic `clientRequestId`를 바꾸지 않습니다.
- Karing-compatible은 YouTube target을 가진 일반 방송·영상·Shorts·community 알림입니다. Twitch/Chzzk-only 알림과 celebration, delivery digest, YouTube milestone, generic notification delivery는 일반 텍스트로 보냅니다. 통합 알림은 YouTube target이 있으면 Karing-compatible입니다.
- `BOT_MARKDOWN_REPLIES`는 `hololive-api` 명령 응답의 별도 계약이므로 전역에서 제거하지 않습니다. Alarm-worker만 room lookup과 Markdown sender 의존성을 제거합니다.
- 기존 `/l/<videoID>` provider, listener와 중앙·Seoul ingress는 발송된 링크 호환성을 위해 유지합니다. Alarm-worker의 새 short-link 생성만 종료합니다.
- Alarm-worker replica는 1을 유지합니다. Group partition이 `clientRequestId`에 관여하는 기존 scale-out 제약은 이 작업에서 변경하지 않습니다.
- 새 dependency, schema, protocol generation, template 등록, retry provider 또는 text fallback을 추가하지 않습니다. Fallback delta: none.
- 로컬 code/test/document 변경은 현재 요청 범위입니다. Commit, push, static-secret master 수정·sync, artifact publication, production stop/restart/deploy, DB write, 실제 Kakao test-room 발송은 각각 exact target에 대한 별도 현재 승인이 필요합니다.

## Chosen boundary

Alarm-worker의 rich notification egress는 Karing을 canonical path로 사용하되, 현재 승인된 YouTube content-list template이 완전하게 표현할 수 있는 알림에만 적용합니다. 알려진 비호환 유형은 `kakaoformat.Render`를 거친 일반 텍스트로 명시적으로 라우팅하며 Markdown은 사용하지 않습니다. 지원 유형에서 Karing 준비나 전달이 실패하면 텍스트로 우회하지 않습니다.

Iris의 `202 Accepted`는 접수 증거일 뿐 성공 증거가 아닙니다. Sender는 반환된 `requestId`를 조회하여 `handoff_completed`를 관측한 뒤에만 confirmed success를 반환합니다. `failed`는 known failure, `outcome_unknown`·알 수 없는 상태·응답 훼손·확인 deadline 소진은 outcome unknown으로 보존합니다. Provider 호출 뒤 outcome unknown이 된 작업은 같은 알림을 다시 post하거나 다른 형식으로 fallback하지 않습니다.

두 Karing opt-in 환경변수는 canonical routing과 모순되므로 제거합니다. `ALARM_SHORT_LINK_BASE_URL`도 producer와 함께 퇴역시키되 이미 배포된 URL을 처리하는 provider 경로는 호환성 계약으로 유지합니다. 새 binary는 세 퇴역 key가 존재하면 무시하지 않고 startup에서 fail closed하여 stale runtime 구성을 드러냅니다.

## Current evidence

- `hololive/hololive-alarm-worker/internal/app/workerapp/build_egress.go`는 `ALARM_DISPATCH_KARING_ENABLED`와 `YOUTUBE_OUTBOX_KARING_ENABLED` 기본값을 false로 읽고 alarm-worker의 plain sender에 open-room Markdown을 연결합니다.
- `hololive/hololive-alarm-worker/internal/egress/iris_sender.go`는 실제 SDK와 겹치는 interface/`any` adapter를 두고 `SendMarkdown`을 제공하며, Karing 응답의 `requestId`와 delivery status를 확인하지 않습니다.
- `hololive/hololive-alarm-worker/internal/service/dispatchrun/alarm_dispatch_runner.go`와 `alarm_dispatch_group.go`는 flag에 따라 text/Karing grouping을 나눕니다. Karing chunk는 최대 네 항목이고 request identity는 grouping에 의존합니다.
- `hololive/hololive-alarm-worker/internal/egress/youtubedispatch/send_engine_karing.go`는 outcome unknown에서 `SENDING`을 보존하는 기존 lifecycle 경계를 갖습니다. 새 sender 오류는 이 경계를 약화하지 않아야 합니다.
- `docs/current/contracts/karing-kakaolink.md`의 성공 설명 일부는 Iris의 현재 `202 + reply-status` 계약과 어긋납니다.
- `ALARM_SHORT_LINK_BASE_URL`의 alarm-worker consumer와 renderer는 새 grouped text URL을 만들지만 `hololive-api`의 `/l/*` provider와 ingress는 과거 링크를 계속 처리해야 합니다.
- 2026-09-04 기준 `hololive-osaka`의 alarm-worker runtime에서 두 Karing flag는 disabled이고 short-link base URL은 configured입니다. 따라서 새 fail-closed binary를 배포하기 전에 승인된 maintenance window에서 static secret을 먼저 정리해야 합니다.

## Ordered tasks

### T01 — Pin canonical routing and delivery outcomes

현재 sender와 두 dispatch caller의 경계를 characterization test로 고정합니다.

- Iris Karing live response의 `requestId`가 없으면 성공하지 않습니다.
- `queued`, `preparing`, `prepared`, `sending`은 확인 중 상태이고 `handoff_completed`만 confirmed success입니다.
- `failed`는 known-not-delivered이고 `outcome_unknown`, 알 수 없는 상태, malformed snapshot과 context 소진은 outcome unknown입니다.
- Status poll 중 transient query 오류는 caller context 안에서만 재조회할 수 있으며 Karing post 자체는 다시 호출하지 않습니다.
- 지원되는 YouTube-addressable 유형은 Karing, 명시된 비호환 유형과 non-YouTube-only 유형은 plain text입니다.
- Karing 준비·post·handoff 실패는 plain text나 Markdown으로 fallback하지 않습니다.

테스트는 실제 Kakao나 Iris runtime에 접근하지 않는 fake client와 clock/poll seam을 사용합니다. 기존 `hololive-api`의 reply handoff 상태 해석을 근거로 삼되 alarm-worker 요구만을 위한 작은 local owner를 만들고 범용 shared abstraction이나 공개 API를 추가하지 않습니다.

### T02 — Confirm Karing handoff in the alarm-worker sender

`hololive/hololive-alarm-worker/internal/egress/iris_sender.go`를 실제 사용 계약에 맞게 정리합니다.

- Constructor의 `any` type switch와 중복 legacy adapter를 제거하고 `SendMessage`, `SendKaringContentList`, `GetReplyStatus`만 포함한 typed client seam을 받습니다.
- Alarm-worker의 `SendMarkdown`, `WithMarkdownReplies`, room-chat lookup과 `kakaoroom` 의존성을 제거합니다. `SendMessage`는 항상 기존 `kakaoformat.Render` plain-text 경로를 사용합니다.
- Karing post가 반환한 accepted payload와 `requestId`를 검증하고, caller context와 bounded poll interval 안에서 reply status를 조회합니다.
- 오직 `handoff_completed`에서 nil을 반환합니다. Known failure와 outcome unknown을 `errors.Is`로 구분할 수 있는 alarm-worker-local typed/sentinel error로 반환합니다.
- Poll error, cancellation 또는 timeout 뒤 post를 반복하지 않습니다. Raw request ID, payload, secret 또는 room message를 새 log에 남기지 않습니다.

Go 파일 수정 직전에 Modern Go Guidelines CLI를 대상 경로 기준으로 실행하고 전체 출력을 적용합니다.

### T03 — Make Karing canonical and preserve lifecycle safety

`workerapp`, `dispatchrun`과 `youtubedispatch`의 routing을 하나의 명시적 정책으로 수렴시킵니다.

- `ALARM_DISPATCH_KARING_ENABLED`, `YOUTUBE_OUTBOX_KARING_ENABLED`, `Runner.karingEnabled`와 flag별 grouping branch를 제거합니다.
- 일반 broadcast/video/Shorts/community 중 YouTube target이 있는 알림은 기존 content-list builder와 최대 네 항목 split을 항상 사용합니다.
- Twitch/Chzzk-only, celebration, delivery digest, YouTube milestone과 generic delivery는 plain text를 유지합니다. 알 수 없는 kind나 불완전한 Karing 입력을 비호환 plain-text 유형으로 오인하지 않고 fail closed합니다.
- Integrated content에서 YouTube target을 canonical Karing link로 선택하고 기존 item order, template map, community argument와 image validation을 유지합니다.
- Alarm dispatch는 confirmed handoff 뒤에만 dispatched로 완료합니다. Known failure는 기존 bounded failure policy로 분류하고 outcome unknown은 retry 판단보다 먼저 분리하여 quarantine/preserved-unknown으로 종결하며 재post하지 않습니다.
- YouTube outbox는 `DEC-20260831-hololive-youtube-egress-lifecycle-transition-ownership`의 `SENDING` 보존, stale sweeper quarantine, provider non-reexecution 규칙을 유지합니다. Sender 확인 context 소진을 일반 retryable deadline으로 낮추지 않습니다.
- Supported Karing 경로의 post 또는 확인 실패에 text/Markdown fallback을 추가하지 않습니다.

### T04 — Retire stale flags and new short-link production

Alarm-worker 구성의 단일 source of truth를 canonical routing과 맞춥니다.

- `YOUTUBE_OUTBOX_KARING_ENABLED`, `ALARM_DISPATCH_KARING_ENABLED`, `ALARM_SHORT_LINK_BASE_URL` parsing, settings field, compose forwarding, examples와 producer-side tests를 제거합니다.
- Alarm-worker startup에만 세 exact key의 presence를 검사하는 retired-env guard를 둡니다. 값이 빈 문자열이어도 stale key presence를 오류로 처리하며 다른 binaries의 환경계약을 넓히지 않습니다.
- `alarm_dispatch_shortlink.go`, renderer short-link injection과 새 grouped notification URL 생성 경로를 제거합니다. 일반 텍스트가 필요한 유형은 canonical full URL을 렌더링합니다.
- `hololive-api` `/l/:videoID` route, scraper 차단, `hololive-shared/pkg/service/shortlink`, port/listener, 중앙·Seoul ingress와 public smoke는 수정하지 않습니다.
- Runtime file master에서 현재 존재하는 `ALARM_SHORT_LINK_BASE_URL` 삭제와 host sync는 local code와 분리된 `stack-platform-ops` 승인 작업으로 남깁니다. Secret 값은 출력하거나 diff에 기록하지 않습니다.

### T05 — Align contracts, runbooks and change record

구현의 canonical behavior와 운영 절차를 문서 정본에 반영합니다.

- `docs/current/contracts/karing-kakaolink.md`를 Iris `202 Accepted -> requestId -> reply-status -> handoff_completed` 계약과 일치시키고, read/render 성공은 보장하지 않는다는 경계를 유지합니다.
- `docs/current/runbooks/alarm-worker.md`에서 두 Karing flag, Markdown alarm lane과 short-link consumer 활성화 절차를 제거하고 compatible/plain-text matrix와 확인 실패 관측법을 기록합니다.
- `docs/current/contracts/shortlink.md`와 admin-dashboard runbook은 producer retired, provider retained 상태로 갱신합니다. 기존 `/l/*` 제거 또는 status/ID contract 변경을 암시하지 않습니다.
- 관련 architecture message-style/egress 문서와 `CHANGELOG.md`를 갱신합니다. Contract map/manifest가 현재 파일 경로를 이미 가리키면 불필요한 churn을 만들지 않고, parser gate가 요구할 때만 함께 수정합니다.
- 문서는 Karing 지원 유형, plain-text 예외, no fallback, outcome unknown, replica 1과 별도 운영 승인 경계를 동일하게 설명해야 합니다.

### T06 — Validate locally and prepare the gated rollout

V01-V06을 final inputs에서 한 번 실행합니다. 실패가 변경 범위와 무관하면 원인을 보고하고 broad suite나 unrelated fix로 확장하지 않습니다. Final diff에 dependency, schema, protocol, template ID, Iris/iris-client-go 변경이 없어야 합니다.

운영 전환은 별도 승인 뒤 다음 순서로만 수행합니다.

1. Iris Karing readiness와 1~4 item dry-run을 확인합니다. Dry-run 결과가 template/argument mismatch이면 전환을 중단합니다.
2. Exact revision으로 arm64 alarm-worker artifact를 로컬 build하고 image identity를 기록합니다. Remote host에서는 build하지 않습니다.
3. 승인된 maintenance window에서 alarm-worker를 중지합니다. Old binary가 plain-text로 발송하는 전환 중간 상태를 허용하지 않습니다.
4. `stack-platform-ops` 승인 범위로 static-secret master의 세 retired key를 삭제하고 `hololive-osaka`에 sync합니다. Exact key absence만 확인하고 값을 출력하지 않습니다.
5. `hololive-bot-ops` 승인 범위로 새 artifact를 no-build deploy하고 alarm-worker replica 1, readiness, image identity와 bounded logs를 확인합니다.
6. 사용자가 지정하고 실제 발송을 승인한 test room에서 Karing 한 건을 보내 `handoff_completed`와 단일 lifecycle completion을 확인합니다. 승인이나 안전한 room이 없으면 live smoke는 미완료로 남깁니다.

중간 실패 시 worker는 중지 상태를 유지합니다. 새 binary에 stale env를 남기거나 old binary를 다시 시작해 Markdown/plain routing으로 되돌리는 것을 자동 rollback으로 사용하지 않습니다. Rollback이 필요하면 prior artifact, secret contract와 사용자-visible egress 차이를 제시하고 별도 승인을 받습니다.

## Acceptance criteria

### AC01 — Supported notifications use one Karing path

YouTube target이 있는 일반 broadcast/video/Shorts/community와 integrated notification은 flag 없이 기존 1~4 content-list template을 사용합니다. Item order, chunk identity, template ID와 arguments는 기존 계약과 같습니다.

### AC02 — Alarm-worker no longer emits Markdown

Alarm-worker에는 `SendMarkdown`, open-room lookup 또는 Markdown enable option이 없습니다. 명시된 비호환 유형은 `kakaoformat.Render` plain text를 사용하고 Karing failure는 다른 형식으로 fallback하지 않습니다. `hololive-api`의 독립적인 `BOT_MARKDOWN_REPLIES` 계약은 유지됩니다.

### AC03 — Accepted and delivered are distinct

Karing `202 Accepted`만으로 alarm dispatch 또는 YouTube delivery가 성공 처리되지 않습니다. Exact `requestId`의 `handoff_completed`를 관측한 뒤에만 confirmed success와 sent/dispatched transition이 발생합니다.

### AC04 — Ambiguous outcomes cannot cause a second post

Known failure와 outcome unknown이 구분됩니다. Status poll failure, unknown state, malformed snapshot, cancellation 또는 deadline 소진 뒤 text fallback, Karing repost, claim release나 일반 retry 경로가 실행되지 않으며 YouTube `SENDING` 보존 규칙과 alarm quarantine 경계가 유지됩니다.

### AC05 — Retired configuration cannot silently persist

두 Karing flag와 `ALARM_SHORT_LINK_BASE_URL`은 active code, compose, example과 producer docs에서 사라지고 alarm-worker는 세 key의 presence를 startup에서 거부합니다. 새 short link는 생성되지 않지만 기존 `/l/*` provider/listener/ingress와 URL behavior는 유지됩니다.

### AC06 — Contracts and operational state are aligned

Karing contract, alarm-worker runbook, short-link contract와 change record가 같은 routing matrix와 `202 + reply-status` 성공 의미를 설명합니다. Local checks가 통과하고 production completion은 승인된 secret cleanup, exact artifact deploy, readiness와 authorized live smoke 근거가 있을 때만 주장합니다.

## Validation

### V01 — Sender handoff contract

From `hololive-bot/hololive/hololive-alarm-worker`:

```bash
go test ./internal/egress -count=1
```

Fake client tests는 accepted-without-request-ID, 모든 in-flight 상태, `handoff_completed`, `failed`, `outcome_unknown`, unknown/malformed status, transient query 오류, cancellation과 deadline을 포함해야 합니다. 각 case에서 Karing post call count는 정확히 1이어야 합니다.

### V02 — Alarm dispatch routing and failure safety

From `hololive-bot/hololive/hololive-alarm-worker`:

```bash
go test ./internal/service/dispatchrun -count=1
```

지원 유형의 1~4 item template mapping과 split, integrated YouTube selection, non-YouTube-only/plain-text 예외, no fallback, confirmed-only completion과 outcome-unknown non-repost가 통과해야 합니다.

### V03 — YouTube lifecycle and application wiring

From `hololive-bot/hololive/hololive-alarm-worker`:

```bash
go test ./internal/egress/youtubedispatch ./internal/app/workerapp -count=1
```

Karing confirmed success, known failure와 outcome unknown이 existing typed lifecycle 결과로 보존되어야 합니다. Wiring test는 두 opt-in flag나 Markdown room lookup 없이 Karing sender를 구성해야 합니다.

### V04 — Settings and build boundary

From `hololive-bot`:

```bash
(cd hololive/hololive-shared && go test ./pkg/config/settings ./pkg/config/settings/alarmworker -count=1)
(cd hololive/hololive-alarm-worker && go build ./cmd/alarm-worker)
```

두 명령은 exit 0이어야 합니다. Alarm-worker runtime loader test는 세 retired key의 빈 값 presence까지 거부해야 합니다. Build 결과 외 tracked generated artifact가 생기면 제거하고, dependency/lockfile/schema/protocol/template diff는 없어야 합니다.

### V05 — Architecture, documentation and catalog boundary

From `hololive-bot`, then `iris-stack`:

```bash
bash scripts/architecture/check-contract-map.sh
bash scripts/architecture/ci-notification-egress-gate.sh
git diff --check
bash ../tools/checks/check-decision-catalog.sh check --submodules
```

모든 command가 exit 0이어야 하며 Karing/short-link 문서가 contract map과 catalog에서 유효해야 합니다. Gate가 stale Markdown/flag/short-link producer reference를 찾으면 active owner를 수정하고 allowlist로 우회하지 않습니다.

### V06 — Approved runtime convergence

별도 승인을 받은 뒤 `hololive-bot-ops`와 `stack-platform-ops`로 다음을 확인합니다.

- Runtime file에 세 retired key가 없고 raw value는 출력되지 않습니다.
- Alarm-worker replica는 1이며 exact approved arm64 image가 ready입니다.
- Startup log에 retired-env, template mismatch, repeated Karing post 또는 outcome-unknown retry가 없습니다.
- Authorized test-room 한 건은 Iris status가 `handoff_completed`이고 해당 alarm/outbox lifecycle은 한 번만 terminal success가 됩니다.

승인된 live smoke나 안전한 eligible notification이 없으면 V06은 inconclusive이며 production outcome을 verified로 올리지 않습니다.

## Failure behavior and approval boundaries

- Karing payload validation 또는 template preparation이 실패하면 supported notification을 plain text로 보내지 않습니다. 오류는 caller lifecycle에 전달합니다.
- Iris 접수 뒤 status가 확정되지 않으면 provider effect는 unknown입니다. Alarm dispatch는 quarantine/preserved unknown으로 종결하고 YouTube delivery는 `SENDING`을 보존하여 stale sweeper가 처리하게 하며 자동 post를 반복하지 않습니다.
- 명시된 non-Karing 유형은 사전에 plain-text로 분류된 정상 경로입니다. 이는 Karing failure fallback이 아닙니다.
- 새 binary와 static secret은 함께 정합화해야 합니다. Retired key가 남은 runtime은 startup failure가 정상이며 key를 무시하거나 guard를 완화해 진행하지 않습니다.
- Local implementation은 commit, push, static-secret write/sync, production stop/restart/deploy, DB write, Kakao 발송 또는 rollback을 승인하지 않습니다.
- Iris dry-run, template contract, local tests 또는 architecture gate가 실패하면 rollout을 시작하지 않습니다. Production 중간 단계가 실패하면 alarm-worker를 중지 상태로 두고 exact recovery 선택에 대한 승인을 요청합니다.

## Handoff

이 계획은 `executing-plans`로 실행합니다. T01-T05와 V01-V05를 로컬에서 먼저 완료하고 T06/V06은 static-secret cleanup, maintenance stop/deploy/restart와 test-room 발송에 대한 별도 승인 뒤에만 진행합니다. 최종 handoff에는 지원/예외 routing matrix, Karing post call count 근거, outcome-unknown 처리, short-link provider 보존, runtime 미완료 항목과 `Fallback delta: none`을 보고합니다.
