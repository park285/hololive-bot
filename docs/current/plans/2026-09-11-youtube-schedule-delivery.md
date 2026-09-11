# YouTube 일정 수집 수정본의 리뷰와 발행

**Decisions:** `DEC-20260911-youtube-restricted-schedule-isolation` (governing), `DEC-20260902-pre-push-phase-content-contract` (constraint), `DEC-20260829-hololive-live-start-evidence-admission` (constraint)

## Execution capsule

**Goal:** 일정 수집 수정본을 리뷰한 뒤 main에 반영·커밋·푸시하고 승인된 collector 네 slot에 배포한다.
**Context:** 구현 계획 PLN-20260911-youtube-restricted-schedule-isolation은 로컬 검증을 완료했다. source 기준은 1c081388d, meta 기준은 fcfa9f49d이며 두 origin/main과 일치한다.
**Constraints:** 사용자 승인은 수정본 리뷰·main 반영·commit/push 및 a/b/c/d collector의 build/transfer/restart/검증을 포함한다. 다른 작업·운영 DB 직접 수정·강제 재전송·새 의존성·비밀 변경은 제외한다.
**Evidence:** 실제 입력에서 정상 일정 2개와 LIVE 1개가 복구됐다. 리뷰에서 제한 행과 정상 행의 중복 ID가 정상 행을 숨기는 경우를 재현해 엄격히 거부하도록 보완했다.
**Success:** 리뷰 회귀와 필수 publish gate 통과, source와 meta의 원격 main 확인, 네 slot의 동일 revision·readiness와 새 수집 확인, 문제 채널의 수집 갱신 및 미확인 상태 보존.
**Output:** 발행 커밋·배포 artifact/rollback 메타데이터와 이 계획의 실행 근거.

## 실행 경계

기존 task worktree `.tmp/youtube-schedule-recovery-20260911/stack`와 그 안의 hololive-bot을 사용한다. source branch는 `fix/youtube-schedule-isolation-20260911`이다. 다른 repository checkout은 검증용 detached snapshot으로 유지한다. 원본 meta checkout의 infrastructure-remediation 변경은 발행 범위에 넣지 않는다.

main 반영은 fast-forward 또는 검토된 정상 merge로 수행한다. ref/index lock, 활성 Git process, reflog와 remote SHA를 확인하고 push 동안 기존 Git writer guard를 유지한다. source 원격 반영을 확인한 뒤 meta pointer를 커밋한다. published history를 재작성하지 않는다.

배포 대상은 Osaka osaka1의 host-native a, 서울 iris-seoul의 Compose b, 중앙 hololive-osaka의 Compose c, Osaka2 osaka2의 host-native d다. 빌드·테스트는 kapu에서만 실행하고 Go binary/Node helper를 같은 revision으로 교체한다. 네 slot을 하나씩 처리하며 새 revision과 건강 상태를 확인한 다음 다음 slot으로 진행한다. 기존 release/image와 배포 트리를 rollback 대상으로 보존한다. API/worker·DB schema/generation 전환은 필요하지 않다.

### T01 리뷰와 회귀 확인

원천 분류, RPC 범위, PARTIAL 발행과 종료 판정, 타입·identity·중복 검증을 리뷰하고 발견한 문제를 수정한다. AC01/V01을 충족한다.

### T02 메인 반영과 발행

T01 이후 source 변경만 stage/commit하고 main에 반영하여 push한다. hook의 canonical phased gate를 모두 통과시킨다. source SHA가 원격 main에 존재함을 확인한 후 관련 meta plan/catalog/pointer만 commit/push한다. AC02/V02를 충족한다.

### T03 순차 배포와 운영 확인

T02 이후 검증된 source revision으로 로컬 artifact를 준비하고 rollback 메타데이터를 확보한다. 각 slot을 canonical deploy runbook으로 순차 교체한 뒤 실제 상태를 확인한다. AC03/V03을 충족한다.

### AC01 리뷰 결과

정상 LIVE/정확한 일정/다른 채널의 중복 행이 접근 제한 필터에 숨겨지지 않는다. 원천 정보가 모순되면 parser_drift로 보존한다. 유효 정상 방송, 제한 영상의 미확인 사유, metadata 독립성과 기존 negative-evidence 경계가 유지된다.

### AC02 발행 결과

hololive-bot source와 관련 iris-stack 기록이 원격 main에 반영된다. source commit과 meta pointer가 일치하며 unrelated infrastructure-remediation 파일을 변경·포함하지 않는다. commit/push hook과 모든 필수 gate를 유지한다.

### AC03 운영 결과

collector a/b/c/d가 모두 검증된 revision의 binary/helper를 실행하고 readiness 및 배포 이후 수집을 확인한다. 문제 채널의 live/metadata job이 새로 완료되고 정상 방송 상태가 갱신된다. 멤버십 시각 부재를 임의 시각·종료·발송 성공으로 바꾸지 않는다.

### V01 리뷰 검증

새 중복 충돌 regression의 수정 전 실패/수정 후 통과, helper 전체 npm test와 typecheck를 확인한다. 기존 Go collector/reducer test·race·NilAway·lint 근거를 유지하고 변경 또는 gate 요구에 따라 재실행한다.

### V02 발행 검증

source pre-push hook의 reusable/freshness/ambient 및 meta의 CI/DB/retry/projection/reissue/worker/decision catalog gate를 통과한다. push 후 ls-remote와 source/meta Git tree를 대조한다.

### V03 배포 검증

`test-three-runtime-topology.sh`, `test-compose-services.sh`, `ap-deploy-version_test.sh`, `ap-host-native-deploy_test.sh`, `systemd-compose-up_test.sh`를 먼저 실행한다. source revision과 호스트별 architecture(native a/d는 amd64, Compose b/c는 arm64), Node version/helper hash, 각 runtime readiness, change_started_at 이후 collection freshness, guarded DB의 문제 채널 job/observation 상태를 확인한다. 모든 테스트·probe는 task 종료 시 멈추며 signal-resistant probe는 bounded transient cgroup에서 실행한다.

## 실행 근거

재개 시 중단된 배포·빌드·push process는 없었고, source/main과 remote/main은 1c081388d, meta/main과 remote/main은 fcfa9f49d였다. 원본 meta의 infrastructure-remediation 변경 네 파일과 untracked 기록은 별도로 보존한다.

리뷰 재현에서 동일 video ID의 LIVE 또는 정확한 UPCOMING 시각이 접근 제한 행과 함께 들어오면 이전 필터가 모두 제거했고, 먼저 나온 다른 채널의 행도 Map 덮어쓰기로 숨길 수 있었다. 새 regression이 Missing expected rejection으로 실패함을 확인했다. helper에서 필터 전 channel identity와 상충한 정상 관측을 검증하도록 보완했다.

보완 후 helper 전체 149개 테스트와 typecheck가 통과했다. 실제 호스트 조회에서 native a/d는 x86_64와 Node v24.20.0, 중앙 c와 서울 b는 aarch64임을 확인했다. 기존 artifact architecture를 유지하며 toolchain/runtime 업그레이드는 하지 않는다.

## 인계

배포·Git 발행 권한은 이 세션에서 승인되었으며 사용자에게 재확인하지 않는다. 필수 gate 실패는 원인을 해결한 뒤 해당 경로를 다시 검증한다. 외부 부작용이 미확정이면 해당 slot에서 멈추고 상태를 조회한다. 수동 메시지 재전송은 수행하지 않는다.
