# 라이브 확인 슬롯·DB 시계 수정 검증

- Plan: `PLN-20260927-live-check-slot-isolation`
- Governing: `DEC-20260927-live-check-slot-isolation`
- Constraints: `DEC-20260926-hololive-live-absence-evidence`, `DEC-20260814-hololive-youtube-three-provider-convergence-v2`
- Opus 5.5 구현 agent 2명이 job 분리와 DB clock 수정을 각각 담당했다. 모든 빌드·테스트·smoke·gate는 통합 담당자가 foreground에서 수행했다.
- 로컬 수정·testcontainers·build-only만 수행했다. 커밋·푸시·운영 DB 변경·migration 적용·배포·fleet 반영·secret 조회는 수행하지 않았다.

## T01 / AC01 — 독립 슬롯 진행

`youtubejs_channel_live`는 live_snapshot만, 새 exact-subject `youtubejs_channel_live_check`는 channel_live_check만 발행한다. registry·scheduler·job contract·publish 검증·API collection demand metrics를 함께 전환했다. 공유 runner의 independentFailures/independentResult와 obsolete fixture를 제거했다. 새 production 파일이 없어 기존 AP manifest가 그대로 빌드 의존성을 포함한다. 여섯 executable youtubejs job의 수요·due·완료 시각을 분리하며 물리 요청 수는 바꾸지 않는다.

수정 전 적대적 리뷰에서 실제 acquisition SQL은 300초 된 DEFERRED scheduled_for를 유지했고, observed/received만 최신인 확인도 실제 LiveQuery가 incomplete로 판정했다. 단순 freshness 완화 대신 check의 슬롯 소유권을 분리했다.

`TestChannelLiveCheckSlotAdvancesWhileSnapshotRetries`는 외부 YouTube 응답만 fake로 두고 **실제 runner → collection executor → joblease → publisher → API consumer → canonical DB**를 실행한다. 테스트 cadence는 1초(운영 기본 2분은 유지)다. 관측된 결과:

| round | snapshot | check scheduled_for (+09:00) | check terminal | canonical observation | received age |
|---|---|---|---|---:|---:|
| 1 | DEFERRED, 02:34:37.362472, fence 1 | 02:34:37.370486 | IDLE/completed | 1 | 21.508ms |
| 2 | 같은 슬롯, fence 2 | 02:34:38.370486 | IDLE/completed | 2 | 12.005ms |
| 3 | 같은 슬롯, fence 3 | 02:34:39.370486 | IDLE/completed | 3 | 12.492ms |

세 observation key는 서로 다르며 canonical의 CHANNEL_PAGE·scheduled_for·effective_at·observation_id가 매번 해당 새 슬롯으로 바뀌었다. snapshot은 완료되지 않았고 live_snapshot 관측을 만들지 않았다. snapshot job lease로 channel_live_check를 발행하면 ErrTargetDisabled로 거부하는 회귀도 통과했다. 이로써 snapshot 재시도가 성공한 확인을 같은 key에 고정시키던 경로가 제거됐다.

## T02 / AC02 — DB statement 시계

`runtime/queries/stale_live_videos.sql`은 MATERIALIZED clock의 `statement_timestamp()`를 두 positive freshness 비교에 사용한다. StaleLiveVideoQuery.AsOf와 전달·검증 caller를 제거했다. projection validity와 activation의 외부 시계는 기존 계약대로 유지한다.

수정 전 동일 DB에서 시각 캡처 → positive 커밋 → stale SQL 실행 시 캡처 시각으로는 `[fresh000001]`, 조회 시점 DB 시각으로는 `[]`가 나왔다. 호스트 skew 없이 재현되는 race였다.

`TestProjectionRefreshKeepsPositiveCommittedAfterRefreshClock`는 실제 Refresher/PolicyBuilder/rosterReader로 다음을 관측했다:

- captured: `2026-09-26T17:34:46.282246Z`
- 이후 DB positive commit 시각: `2026-09-26T17:34:46.282667Z` (421µs 뒤)
- 그 캡처 시각으로 Refresh해도 **video_target=none**.

DB 상대 fixture의 fresh → 만료 → fresh 회복 → 만료 재진입 → ENDED 전이도 통과했다. generation은 1 → 2 → 3 → 4 → 5였고 안쪽 freshness 경계에서는 generation 1을 유지했다. 진짜 stale·missing head·NULL·미래 positive/seen은 계속 선택한다. DB 시계를 실제 사용하는 테스트이므로 예산 안쪽 fixture에는 10초 여유를 두며, 정확한 동시 경계라고 주장하지 않는다.

## T03 / V01 / V02 — 실제 검증

공통 Go 환경: `GOEXPERIMENT=jsonv2 GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off`.

- 위 slot smoke: `go test -mod=readonly -race -count=1 -run '^TestChannelLiveCheckSlotAdvancesWhileSnapshotRetries$' -v ./hololive/hololive-youtube-collector/internal/runtime/collectorruntime` → PASS, 10.899초.
- clock/transitions smoke: `go test -mod=readonly -race -count=1 -run '^TestProjectionRefresh(KeepsPositiveCommittedAfterRefreshClock|TracksStaleLiveVideoTransitions)$' -v ./hololive/hololive-api/internal/planes/youtube/runtime` → PASS, 6.330초.
- 영향 7패키지 `go test -mod=readonly -race -count=1`: collector youtubejscollector/collectorruntime/joblease, shared sourceobservation service/contracts, API youtube runtime/targetprojection → 모두 PASS, 52.745초.
- affected pinned NilAway → PASS. 발견한 test complexity는 fixture와 의미별 assertion 분리로 해소했고 억제 주석/threshold 변경은 하지 않았다. incidental job 개수 고정 assertion은 삭제했다.
- 최종 `COMPOSE_ENV_FILE=$PWD/deploy/compose/build-only.env.sample ./build-all.sh --build-only --no-bump` → **exit 0, 665.632초**. `[LOCAL CI] Passed`, `[DONE] Image build complete`. 전체 workspace vet·staticcheck·golangci-lint·pinned NilAway·Go build·일반 테스트·race(-p 4)·SQL ownership·hard structure·AP manifest·performance budget 포함. 선택적 RUN_INTEGRATION_TESTS는 켜지 않았으며 위 DB smoke는 별도로 실행했다.
- 메타 workspace의 check-stack-db-access-policy / check-stack-retry-contract / check-stack-projection-tables / check-stack-worker-contract → 각각 exit 0.
- 종결 전 selected plans gate: strict_validation=passed, gate_passed=true.
- 사용자 지정 보호 파일 5개(admin-dashboard README와 performance-resolution, member-info cleanup plan/review, go.work.sum)의 빌드 전후 SHA-256 동일. 관련 없는 사용자 변경을 되돌리지 않았다.

## 전환 조건과 남는 한계

계약 §3.4, collector 서비스와 runbook을 갱신했다. 공유-job collector drain → API의 분리 job 계약 → 새 collector 순서이며 혼합 실행을 지원하는 shim은 없다. 기존 공유 버전을 운영한 환경은 최초 독립 슬롯과 마지막 기존 슬롯의 key 충돌 가능성을 확인하고 다음 새 슬롯까지 관측한다. 이 변경에는 추가 schema migration이 없다. 운영 적용은 별도 승인 사항이다.

Fallback delta: 새 fallback·자동 retry·freshness 완화 없음. 기존 snapshot retry/채널 UNKNOWN/수명·pending 계약 유지.

조건부 cross-channel ENDED 진단 누락은 producer 도달성이 입증되지 않은 별도 위험이며 이번 두 확정 이슈 수정 범위가 아니다. 전체 catalog의 사전 고지된 범위 밖 결함은 임의 수정하거나 재확인하지 않았다. 이 후속 계획의 V02는 명시된 영향 gate와 selected gate이며 전체 catalog 통과를 주장하지 않는다. 이전 terminal 계획은 재개·수정하지 않는다.
