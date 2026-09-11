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

### 발행과 배포 결과

[PR #491](https://github.com/park285/hololive-bot/pull/491)의 [CI](https://github.com/park285/hololive-bot/actions/runs/34568558326)는 fast-gate를 포함한 12개 check가 모두 성공했다. 로컬 full pre-push의 일반·race test, lint·NilAway·build·freshness·ambient도 통과했다. 리뷰 commit `bf7d9cb20bd15744c40d25122034d362c1fb7cfa`와 동일한 tree를 가진 main commit `214ff8ef73ab767b9092d7f18362829fa6527169`가 2026-09-11 06:16:24 UTC에 반영됐다. 최초 직접 push는 필수 원격 check 부재로 거부됐으며 PR 절차로 해소했다. Meta main `9a17c10f1`의 source pointer도 해당 main SHA와 일치한다.

다음 시각은 UTC이며 모두 동일한 source revision `214ff8ef73ab767b9092d7f18362829fa6527169`를 실행한다.

| Slot | 실행 방식 | 새 기동 시각 | 보존한 rollback |
|---|---|---|---|
| a / osaka1 | native amd64 | 2026-09-11 06:22:03 | release `20260907-session-27eddada-osaka-r2` |
| b / iris-seoul | Compose arm64 | 2026-09-11 06:23:12.982468046 | image `rollback-20260911T062302Z`, `backups/seoul-collector-20260911T062302Z` |
| c / hololive-osaka | Compose arm64 | 2026-09-11 06:28:27.846144243 | image `rollback-youtube-schedule-20260911-214ff8ef73ab`, 기존 `/opt/hololive-bot/compose/current` |
| d / osaka2 | native amd64 | 2026-09-11 06:30:19 | release `20260907-session-27eddada-osaka2` |

Native의 새 release는 각각 `20260911-youtube-schedule-214ff8ef73ab-osaka`와 `20260911-youtube-schedule-214ff8ef73ab-osaka2`다. 두 호스트의 실행 파일 SHA-256은 로컬 artifact와 같은 `b9095533397e7ce8e5c51880ac76a7d4f5681f2d3066ec5e3f80f539d1bb83c8`이다. 중앙·서울 ARM64 image archive SHA-256은 `a252ea233e38f59f31c103c5278c4f271f8076bb6f7422cef2d4907fc18811f8`, archive의 configuration digest는 `42acbc59955672fb44a6760dc775e8cb66e051c83e1f9d8d51c3b09f23c58355`다. 중앙과 로컬의 image ID는 `da02f7aec3f0e22cfa710ca899fcde81d813bc974226ce90b6805f1183ccf80a`이고 서울 engine은 configuration digest를 image ID로 보고한다. 중앙 전달 파일의 전체 해시, loaded image의 architecture·revision과 실제 container image ID를 대조했다.

네 slot의 Node는 v24.20.0이다. 실제 helper의 `fetch-channel.mjs` SHA-256은 `3949c27c771c4ccc34d303b83381e605ab6ab1c43c2db044156ca9b9a400d9b5`, `live-metadata.mjs`는 `d51fa8f862c9df6aae2f814f2daba28de40a9b742bc91fbfa730b88ddf65bdd4`로 로컬과 일치했다. 모두 READY, helper ok, first_success=true, handoff PROCESSED를 확인했다. 중앙의 실제 prod/admin-security/live-compat/admin-web overlay 네 개를 유지하고 c만 `--no-build --no-deps --force-recreate`로 교체했다. wrapper와 Compose 파일 해시가 보존됐고 전후 container ID 차이는 c 한 개뿐이었다.

### 운영 수집 근거와 남은 한계

`transaction_read_only=on`, statement timeout 10초의 guarded query로 확인했다. 06:30:19 이후 새 관측은 a 175건, b 123건, c 114건, d 174건이며 각 slot의 마지막 observed_at은 06:34:24~25였다. 문제 채널 `UCKSpM183c85d5V2cW5qaUjA`의 live job은 06:33:41.584437, metadata job은 06:25:19.263974에 완료됐고 두 job 모두 IDLE이며 retry_not_before가 비어 있었다. 보존된 마지막 실패 시각은 모두 마지막 slot 배포 이전이다.

최신 live 관측은 PARTIAL로 정상 sessions 29개를 발행했다. `Spraq2szAMA`와 `dAOcenyS3n8`의 일정은 각각 2026-12-09 03:00 UTC, 2026-09-13 03:00 UTC이며 projection의 last_seen_at이 새 수집으로 갱신됐다. 시각이 가려진 `ijnXjcoquTY`는 제외하고 기존 상태·last_seen_at(2026-09-10 09:57:38.011899 UTC)을 보존했다. 기존 예정 시각을 새 확인 결과로 재사용하지 않았다.

과거 누락 `yK62q7V_JvE`는 첫 배포 이전인 06:05:35 UTC에 종료됐다. 06:31의 공개 player 조회는 실제 startTimestamp 03:02:15와 endTimestamp 06:05:35를 반환했고 새 collector도 ENDED fact를 발행했다. 그러나 기존 projection은 실제 LIVE 시작을 관측하지 못했다. 기존 `CanEnd`의 LastLivePositiveAt/SeenAt 필수 계약 때문에 이 과거 row는 UPCOMING으로 남는다. 이 한 건의 과거 상태 자동 정리와 지나간 알림 복구는 검증된 수집 복구 결과에 포함되지 않는다. 시작 관측을 소급 생성하거나 운영 DB를 직접 수정하지 않았다.

처음의 과거 전체 관측 조회는 10초 timeout으로 종료됐다. 운영 index `(observation_kind, subject_key, scheduled_for DESC, id DESC)`와 최근 시각을 지정한 bounded query로 필요한 최신 PARTIAL 근거를 확인했다. 실제 YouTube 최종 probe는 45초 transient cgroup에서 정상 종료됐다.

Fallback delta: 확인된 paid-access 시각 부재를 PARTIAL로 격리하는 예외 1개다. 추가 provider·추가 retry·시각 추정·과거 LIVE 관측 조작·수동 재전송은 없다. consumer와 공용 payload generation은 유지했다.

## 인계

배포·Git 발행 권한은 이 세션에서 승인되었으며 네 collector의 배포와 운영 검증을 완료했다. 실행 중인 code revision은 `214ff8ef73ab767b9092d7f18362829fa6527169`이며 이후 문서 마무리 commit은 실행 파일 변경을 포함하지 않는다. 숨겨진 멤버십 시각과 위 과거 상태 한 건의 한계를 유지한다. 수동 메시지 재전송은 수행하지 않았다.
