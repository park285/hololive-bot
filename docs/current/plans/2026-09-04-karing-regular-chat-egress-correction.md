# Hololive 일반채팅 Karing egress 범위 정정 계획

## Execution capsule
**Goal:** Hololive의 Karing 지원 알림을 확인된 일반채팅에만 보내고 오픈채팅의 기존 Markdown 알림을 보존합니다.
**Context:** 기존 Karing 구현은 opt-in flag로 전체 방에 적용됐고, 첫 정합화 변경은 방 유형을 구분하지 않은 채 alarm-worker Markdown 경로까지 제거하여 사용자 의도보다 범위를 넓혔습니다.
**Constraints:** 기존 Karing template, YouTube lifecycle fencing, replica 1, `BOT_MARKDOWN_REPLIES`, grouped short-link와 `/l/*` 호환성을 유지하며 결과 불명확 알림을 재발송하지 않습니다.
**Evidence:** `kakaoroom.Catalog`가 오픈채팅 여부를 소유하고 기존 `IrisMessageSender`는 오픈채팅만 `SendMarkdown`으로 보냈으며, alarm dispatch와 YouTube outbox에는 재사용 가능한 Karing builder가 있습니다.
**Success:** 확인된 일반채팅의 지원 알림만 Karing을 사용하고 오픈채팅은 Markdown, 미확인 방과 비지원 유형은 기존 일반 텍스트를 사용하며 모든 로컬 검증이 통과합니다.
**Output:** 방 유형 판정, sender·alarm dispatch·YouTube outbox routing, 설정·short-link·계약 문서를 정정하고 추적 가능한 테스트 근거를 남깁니다.

## Plan controls

**Decisions:** `DEC-20260904-hololive-karing-regular-chat-egress` (governing), `DEC-20260831-hololive-youtube-egress-lifecycle-transition-ownership` (constraint), `DEC-20260730-hololive-alarm-egress-lease-removed` (constraint), `DEC-20260517-iris-karing-runtime-capability` (context)

- 기준 source revision은 `hololive-bot@69b2a76b5473a568eda2672e17e6068888a85fdf`이며 현재 변경은 커밋되지 않은 첫 구현을 정정합니다.
- 일반채팅 여부는 `kakaoroom.Catalog`가 positive fact로 확인해야 합니다. 조회 실패나 미확인 방을 일반채팅으로 추정해 Karing을 보내지 않습니다.
- 오픈채팅은 기존 `BOT_MARKDOWN_REPLIES`가 활성화된 경우 `SendMarkdown`, 비활성화된 경우 `kakaoformat.Render` 일반 텍스트를 사용합니다.
- Karing-compatible 유형은 YouTube target을 가진 일반 broadcast/video/Shorts/community입니다. Twitch/Chzzk-only, celebration, delivery digest, YouTube milestone과 generic notification delivery는 기존 message path를 사용합니다.
- Karing template mapping `1 -> 133266`, `2 -> 133223`, `3 -> 133222`, `4 -> 133267`, 최대 네 항목 split과 deterministic `clientRequestId`를 유지합니다.
- `YOUTUBE_OUTBOX_KARING_ENABLED`와 `ALARM_DISPATCH_KARING_ENABLED`는 방 유형 기반 routing과 모순되므로 퇴역시킵니다. `ALARM_SHORT_LINK_BASE_URL`과 grouped short-link producer는 기존 message path를 위해 유지합니다.
- 새 dependency, schema, protocol, template 등록, text fallback 또는 운영 side effect를 추가하지 않습니다. Fallback delta: none.
- 로컬 코드·테스트·문서 변경만 현재 요청 범위입니다. Commit, push, secret 변경, 배포·재시작과 실제 Kakao 발송은 별도 승인이 필요합니다.

## Chosen boundary

방 유형과 content compatibility를 모두 만족해야 Karing을 선택합니다. `kakaoroom.Catalog`가 일반채팅으로 확인한 방에서만 지원되는 YouTube 알림을 Karing으로 보내며, 오픈채팅은 기존 Markdown/message 경로로 내려갑니다. 미확인 방은 안전하게 일반 텍스트를 사용합니다. 이는 Karing 실패 뒤 다른 형식으로 보내는 fallback이 아니라 provider 호출 전에 결정되는 정상 routing입니다.

Karing sender는 Iris의 `202 Accepted`와 non-empty `requestId`를 검증한 뒤 `/reply-status/{requestId}`의 `handoff_completed`를 확인해야 성공합니다. `failed`는 known failure이고 `outcome_unknown`, 알 수 없는 상태, malformed snapshot과 확인 deadline 소진은 outcome unknown입니다. Accepted request는 다시 post하지 않습니다.

기존 grouped message가 사용하는 `ALARM_SHORT_LINK_BASE_URL`과 `/l/*` producer/provider는 유지합니다. 두 Karing opt-in flag만 제거하고 빈 값으로 남아 있어도 alarm-worker startup에서 거부합니다.

## Ordered tasks

### T01 — Restore positive room-kind classification

`kakaoroom.Catalog`에 미확인과 일반채팅을 구분하는 positive regular-chat query를 추가하고 기존 `OpenChat` 계약을 유지합니다. Cache, DB와 Iris room list의 기존 lookup 순서를 재사용하며 새 fallback이나 별도 source of truth를 만들지 않습니다.

### T02 — Preserve open-chat Markdown and confirm Karing handoff

`IrisMessageSender`의 typed client seam과 Karing status 확인 개선은 유지하되 `SendMarkdown`, `WithMarkdownReplies`, `WithRoomChat`을 복구합니다. 일반 message는 확인된 오픈채팅에서만 Markdown을 사용하고 나머지는 `kakaoformat.Render`를 사용합니다. Sender는 regular-chat 판정을 downstream routing에 제공합니다.

### T03 — Scope alarm dispatch Karing to regular chats

Alarm dispatch grouping과 path 선택에 방 유형을 포함합니다. 확인된 일반채팅의 지원 YouTube 알림만 Karing grouping/split을 사용하고 오픈채팅·미확인 방·비지원 유형은 기존 text/Markdown grouping과 renderer를 사용합니다. Karing failure의 no-fallback, confirmed-only completion과 outcome-unknown quarantine은 유지합니다.

### T04 — Scope YouTube outbox Karing to regular chats

YouTube outbox sender capability가 room eligibility를 함께 노출하도록 정리합니다. Send engine은 일반채팅에서만 Karing lifecycle을 시작하고 오픈채팅·미확인 방에서는 기존 rendered message와 deterministic message `clientRequestId` 경로를 사용합니다. Outcome unknown에서는 기존 `SENDING` 보존 규칙을 유지합니다.

### T05 — Retain short links and align configuration and docs

Grouped short-link renderer, `ALARM_SHORT_LINK_BASE_URL` settings와 validation/tests를 복구하고 Karing 전역 충돌 검사만 제거합니다. Startup retired-env guard와 compose에서는 두 Karing flag만 제거합니다. Karing/short-link 계약, alarm-worker runbook, architecture map과 change record를 room-specific matrix에 맞춥니다.

### T06 — Validate the corrected boundary

V01-V05를 final inputs에서 실행합니다. Dependency, lockfile, schema, protocol, template ID와 Iris client 변경이 없어야 하며 운영 배포나 실제 메시지 전송은 수행하지 않습니다.

## Acceptance criteria

### AC01 — Karing is regular-chat only

방 유형이 일반채팅으로 확인된 지원 YouTube 알림만 Karing content-list를 사용합니다. 오픈채팅과 미확인 방에서 Karing post가 발생하지 않습니다.

### AC02 — Open-chat Markdown behavior is preserved

오픈채팅은 `BOT_MARKDOWN_REPLIES=true`일 때 기존 `SendMarkdown` 경로를 사용하고 false일 때 일반 텍스트를 사용합니다. 일반채팅에는 Markdown을 보내지 않습니다.

### AC03 — Routing remains explicit and fail closed

비지원 유형은 provider 호출 전에 기존 message path로 분류됩니다. Karing build·admission·handoff 실패 뒤 Markdown이나 text로 fallback하지 않으며 미확인 방을 일반채팅으로 추정하지 않습니다.

### AC04 — Accepted and delivered remain distinct

Karing `202 Accepted`만으로 성공 처리하지 않고 exact `requestId`의 `handoff_completed` 뒤에만 sent/dispatched로 전이합니다. Outcome unknown은 재post하지 않고 alarm quarantine과 YouTube `SENDING` 보존 경계를 유지합니다.

### AC05 — Only obsolete flags are retired

두 Karing opt-in flag는 active code와 compose에서 사라지고 startup에서 presence를 거부합니다. `ALARM_SHORT_LINK_BASE_URL`, grouped short-link producer와 기존 `/l/*` provider/listener/ingress는 유지됩니다.

### AC06 — Contracts and tests describe one room matrix

코드, 테스트, Karing·short-link 계약과 alarm-worker runbook이 일반채팅 Karing, 오픈채팅 Markdown, 미확인·비지원 일반 텍스트를 동일하게 설명합니다.

## Validation

### V01 — Room catalog and sender contract

From `hololive-bot`:

```bash
(cd hololive/hololive-shared && go test ./pkg/service/kakaoroom -count=1)
(cd hololive/hololive-alarm-worker && go test ./internal/egress -count=1)
```

Known regular, open and unknown room tests와 Markdown/plain/Karing handoff 상태 테스트가 통과해야 합니다.

### V02 — Alarm dispatch room routing

From `hololive-bot/hololive/hololive-alarm-worker`:

```bash
go test ./internal/service/dispatchrun -count=1
```

같은 지원 payload가 일반채팅에서는 Karing, 오픈채팅과 미확인 방에서는 message path를 사용하며 grouped short-link와 failure semantics가 통과해야 합니다.

### V03 — YouTube lifecycle and wiring

From `hololive-bot/hololive/hololive-alarm-worker`:

```bash
go test ./internal/egress/youtubedispatch ./internal/app/workerapp -count=1
```

Room eligibility가 Karing capability 선택 전에 적용되고 open/unknown은 기존 message lifecycle, Karing outcome unknown은 existing `SENDING` 보존으로 이어져야 합니다.

### V04 — Configuration and build

From `hololive-bot`:

```bash
(cd hololive/hololive-shared && go test ./pkg/config/settings ./pkg/config/settings/alarmworker -count=1)
(cd hololive/hololive-alarm-worker && go build ./cmd/alarm-worker)
```

두 Karing flag presence는 거부하고 `ALARM_SHORT_LINK_BASE_URL`은 정상 로드되어야 하며 build 결과 외 tracked artifact가 생기지 않아야 합니다.

### V05 — Architecture, contract and catalog boundary

From `hololive-bot`, then `iris-stack`:

```bash
bash scripts/architecture/check-contract-map.sh
bash scripts/architecture/ci-notification-egress-gate.sh
git diff --check
bash ../tools/checks/check-decision-catalog.sh check --submodules
```

모든 명령이 exit 0이어야 하고 gate는 alarm-worker Markdown을 금지하지 않고 일반채팅 전용 Karing의 room selector와 두 퇴역 flag만 검증해야 합니다.

## Failure behavior and approval boundaries

- Room lookup 실패나 미확인은 Karing eligibility가 아니며 기존 일반 텍스트 경로를 사용합니다.
- Karing payload validation이나 handoff 확인 실패는 같은 알림을 Markdown/text로 재전송하지 않습니다.
- 오픈채팅 Markdown 오류는 기존 message failure lifecycle을 따르며 Karing으로 우회하지 않습니다.
- 두 Karing flag가 runtime에 남아 있으면 startup이 실패하지만 `ALARM_SHORT_LINK_BASE_URL`은 유효한 기존 설정입니다.
- Local implementation은 commit, push, secret write, production deploy/restart, DB write와 Kakao 발송을 승인하지 않습니다.

## Handoff

이 계획은 `executing-plans`로 실행합니다. 최종 handoff에는 방별 routing matrix, Karing post call count, open-chat Markdown 보존, unknown-room 동작, short-link 유지, 검증 결과와 `Fallback delta: none`을 보고합니다.
