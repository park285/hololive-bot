# Hololive Karing Redrain Path Stability Correction

## Execution capsule

**Goal:** room 판정이 재드레인 사이에 바뀌어도 ambiguous 발송을 다른 text/Karing payload로 재전송하지 않게 합니다.
**Context:** drain 내부 snapshot은 경로를 고정하지만 선택 결과를 영속하지 않으므로 unknown→regular 또는 regular→unknown 변화 뒤 기존 retry가 다른 payload와 `clientRequestId`를 사용할 수 있습니다.
**Constraints:** schema·data·dependency·fallback을 추가하지 않고, 429/502/503 미접수 확정 재시도와 room 판정에 의존하지 않는 text 경로를 보존하며 `outcome_unknown`은 fail-closed합니다.
**Evidence:** alarm dispatch grouping·client request ID·failure routing의 연결 경로, cross-redrain 특성화 테스트, 표적 Go 테스트와 notification egress gate입니다.
**Success:** room-scoped text/Karing의 ambiguous TransportError·deadline은 quarantine되고 재드레인되지 않으며, 미접수 확정 실패와 고정 text 경로의 안전한 retry는 유지됩니다.
**Output:** group의 route-stability 표식, failure routing 보정, 회귀 테스트와 versioned validation evidence입니다.

## Plan controls

**Decisions:** `DEC-20260904-hololive-karing-regular-chat-egress` (governing), `DEC-20260731-reply-outcome-unknown-fail-closed` (constraint), `DEC-20260831-hololive-youtube-egress-lifecycle-transition-ownership` (constraint)

## Ordered tasks

### T01 Characterize cross-redrain path changes

같은 persisted send unit이 첫 drain의 text와 다음 drain의 Karing에서 서로 다른 payload·ID를 만드는 연결 경로와 기존 ambiguous retry 허용을 고정합니다.

### T02 Carry room-scoped route provenance through groups

envelope의 경로 판정이 room facts에 의존하는지 함께 계산하고 group 병합과 Karing split에 보존해 failure owner까지 전달합니다.

### T03 Fail closed on ambiguous room-scoped sends

room-scoped text/Karing의 TransportError·deadline은 quarantine하고, 미접수 확정 429/502/503 및 intrinsic text의 기존 안전한 retry는 유지합니다.

### T04 Validate the corrected failure boundary

dispatchrun과 worker wiring의 표적 테스트, notification egress architecture gate와 diff check를 실행하고 evidence·PLN을 정합합니다.

## Acceptance criteria

### AC01 Dynamic egress paths cannot retry ambiguous effects

unknown→regular 또는 반대 변화가 가능한 알림은 ambiguous send 뒤 retry queue에 들어가지 않습니다.

### AC02 Confirmed non-admission retries are preserved

429, 502와 503은 text/Karing 경로와 무관하게 기존 bounded retry를 유지합니다.

### AC03 Intrinsic text retry remains stable

celebration, digest, milestone과 Twitch/Chzzk-only처럼 room type에 의존하지 않는 text 경로는 동일 payload·ID 재생산이 가능한 기존 retry를 유지합니다.

### AC04 No broader contract or operational change is introduced

DB/schema/data, dependency, retry 횟수, fallback, deploy, restart, commit, push와 release는 변경하거나 실행하지 않습니다.

## Validation

### V01 Focused dispatch tests pass

room-scoped text와 Karing ambiguity, intrinsic text retry 및 미접수 확정 retry 회귀 테스트와 전체 dispatchrun package test가 통과합니다.

### V02 Wiring and architecture checks pass

alarm-worker workerapp test와 `scripts/architecture/ci-notification-egress-gate.sh`가 통과합니다.

### V03 Final diff and lifecycle checks pass

Hololive와 meta `git diff --check`, decision catalog와 PLN post-validation이 통과합니다.

## Failure behavior and stop rules

- 안전한 자동 retry에 durable route 증명이 필요하면 schema를 추가하지 않고 room-scoped ambiguous 결과를 quarantine합니다.
- 미접수 확정 HTTP 상태까지 차단하거나 intrinsic text 경로를 바꾸지 않습니다.
- 다른 세션이 관련 파일을 다시 수정하면 중단하고 소유권을 재확인합니다.
- Fallback delta: none.
