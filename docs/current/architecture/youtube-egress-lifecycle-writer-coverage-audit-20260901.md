# YouTube egress lifecycle writer와 historical coverage 감사

- 감사일: 2026-09-01 KST
- 기준 커밋: `340109e73fe81e3b3b7fd7bc9fff21fb14b9af64`
- 적용 계획: [`../plans/2026-08-31-youtube-egress-lifecycle-implementation.md`](../plans/2026-08-31-youtube-egress-lifecycle-implementation.md)
- 적용 결정: `DEC-20260831-hololive-youtube-egress-lifecycle-transition-ownership`

## 판정

현재 delivery와 post-fanout outbox lifecycle writer는 alarm-worker와 YouTube plane의 poller batch repository에 분산되어 있습니다. Poller가 `FAILED` row를 다시 `PENDING`으로 만들거나 post-level tracking만으로 delivery를 `SENT`로 바꾸는 경로가 있으므로 fixed-high-water capture 전 제거해야 합니다.

Repository evidence만으로는 이미 cleanup된 historical terminal delivery의 시작 경계를 증명할 수 없습니다. 또한 source observation replay는 observation ID를 받지만 event age 하한을 검사하지 않고, production retention은 runtime configuration에 따라 비활성화될 수 있습니다. 따라서 `legacy_coverage_start_at`은 이 감사에서 `unknown`이며 다음 작업은 차단됩니다.

- production ledger completion marker 기록
- 새 transition writer production cutover
- ledger-guarded cleanup 활성화
- governing decision의 `verified` 전환

Additive schema, compatibility writer, bounded backfill binary, typed policy와 cutover code는 이 차단 상태를 fail closed로 보존하는 조건에서 구현할 수 있습니다.

## Writer inventory

| 단계 | 현재 writer와 query | runtime owner | 현재 의미 | 목표 |
|---|---|---|---|---|
| pre-fanout claim | `youtubedispatch/queries/dispatcher_claim_0050_01.sql` | alarm-worker | `PENDING` outbox lock 획득 | `OutboxFanoutService` 전용 |
| pre-fanout release | `dispatcher_claim_release_0019_01.sql` | alarm-worker | outbox lock 해제 | `OutboxFanoutService` 전용 |
| pre-fanout terminal | `status_updater_0066_01.sql`, `0118_02.sql`, `0205_05.sql`, `0226_06.sql` | alarm-worker | no-target 성공과 fanout 전 실패 | child 부재 CAS가 있는 fanout command |
| pre-fanout retry | `status_updater_0245_07.sql`, `0269_08.sql` | alarm-worker | outbox retry due와 attempt 변경 | typed fanout failure command |
| delivery materialization | shared `outbox/store/delivery_repository_0102_02.sql` | alarm-worker | `(outbox_id, room_id)` child 생성 | alarm-worker internal store |
| delivery claim | shared `delivery_repository_0119_03.sql` | alarm-worker | `PENDING` delivery lock 획득 | version 증가 `ClaimPending` |
| begin send | shared `delivery_repository_lock_0065_01.sql` | alarm-worker | `PENDING -> SENDING` | operation-level fenced `BeginSending` |
| delivery success | shared `delivery_repository_0188_04.sql`, `delivery_repository_lock_0142_02.sql` | alarm-worker | delivery `SENT` | delivery/tracking/ledger terminal transaction |
| delivery retry/failure | shared `delivery_repository_0231_05.sql`, `0258_06.sql`, `0282_07.sql`, `delivery_repository_lock_0190_03.sql`, `0224_04.sql` | alarm-worker | SQL이 retry 소진과 상태를 재판정 | typed owner/follower group command |
| stale send quarantine | shared `delivery_repository_lock_0255_05.sql` | alarm-worker | stale `SENDING -> QUARANTINED` | group과 ledger의 atomic quarantine |
| post tracking | shared `delivery_repository_tracking_0066_01.sql`과 tracking repository | alarm-worker | exact claim으로 tracking `SENT` | terminal envelope 안의 typed requirement |
| unsafe sent recovery | `dispatcher_claim_gate_0230_01.sql` | alarm-worker | ID-only delivery `SENT` 복구 | 제거, exact envelope read-back으로 대체 |
| aggregate | shared `delivery_repository_aggregate_sync.sql` | alarm-worker | child 상태로 outbox 계산 | 유일한 post-fanout projector와 `terminal_at` |
| worker revive | `dispatcher_claim_revive_0141_02.sql`, `0156_03.sql` | alarm-worker | outbox 단위 `FAILED -> PENDING` | ledger-aware logical group revive |
| terminal cleanup | `dispatcher_claim_release_0042_02.sql` | alarm-worker | `COALESCE(sent_at, created_at)` 기준 삭제 | completed ledger와 `terminal_at` 필수 |
| orphan cleanup | `dispatcher_claim_release_0072_03.sql` | alarm-worker | child 없는 stale `PENDING` 삭제 | fanout claim과 freshness fence 유지 |
| producer rearm | `batchrepo/queries/repository_batch_writes_0244_06.sql` | hololive-api YouTube plane | Community/Shorts `FAILED -> PENDING` | existing lifecycle field 변경 제거 |
| producer delivery rearm | `repository_batch_delivery_state_0198_02.sql` | hololive-api YouTube plane | child `FAILED -> PENDING` | 제거 |
| producer sent inference | `repository_batch_completed_finalize_0088_01.sql`, `0111_02.sql` | hololive-api YouTube plane | post-level tracking으로 outbox/delivery `SENT` | 제거 |

`hololive-shared/pkg/service/youtube/outbox/store`의 production importer는 alarm-worker의 `youtubedispatch`뿐입니다. Poller batch repository는 이 package를 import하지 않고 별도 SQL로 lifecycle을 변경합니다. Tracking observation repository는 post-level tracking을 소유하며 room-level delivery 성공 증거를 소유하지 않습니다.

## Replay와 retention 경계

| 경로 | repository evidence | `replay_floor_at` 판정 |
|---|---|---|
| primary delivery claim | `dispatchstate.DefaultConfig().ClaimFreshnessWindow = 2h`; claim query가 outbox `created_at` 하한을 적용 | 현재 outbox 생성 시각 기준 `now-2h` |
| worker revive | `ReviveFreshnessWindow = 1h`, interval `5m`; current code가 outbox `created_at`을 검사 | 현재 outbox 생성 시각 기준 `now-1h` |
| terminal full-row cleanup | `CleanupAfter = 7d`; current query는 `COALESCE(sent_at, created_at)` 사용 | retained row의 가장 이른 완전 경계를 보장하지 못함 |
| source observation automatic replay | replay queue가 observation ID로 원본 evidence를 다시 처리하며 event age predicate가 없음 | unbounded |
| operator/manual source replay | `RequestReplay`가 positive observation ID와 audit fields만 검증하며 event age predicate가 없음 | unbounded |
| poller rediscovery rearm | Community/Shorts existing `FAILED` outbox를 현재 시각의 due로 재활성화 | source history에 따라 unbounded |
| tracking-based repair | post-level `SENT`를 room delivery `SENT`로 투영 | logical room evidence가 아니므로 floor로 사용할 수 없음 |
| big-bang cutover 또는 historical repair | repository에 승인된 bounded command가 없음 | unknown, 실행 차단 |

`source_observation` retention은 기본적으로 비활성화될 수 있고 manual replay는 retained observation의 event age를 제한하지 않습니다. 오래된 observation이 cleanup 뒤 새 outbox를 만들면 outbox `created_at`은 새 persist 시각이 될 수 있으므로 alarm-worker의 2시간 claim freshness만으로 logical event replay를 제한할 수 없습니다.

## Characterization evidence

다음 기존 테스트가 교체 전 보존할 동작을 고정하며 2026-09-01 기준으로 통과했습니다.

| 불변조건 | test evidence |
|---|---|
| `locked_at`의 microsecond 차이도 stale token으로 거부 | `TestMarkSendingBatchIfLockedRejectsStaleRelockWithinOneMillisecond` |
| `SENDING`은 primary `PENDING` claim 대상이 아님 | `TestFetchAndLockDoesNotReclaimSendingRows`, `TestDispatcherAggregateSyncQuarantinesStaleSendingDelivery` |
| outcome unknown은 write, release, fallback, resend 없음 | `TestDispatchDeliveryRows_PerRoomOutcomeUnknownHoldsClaim`, `TestDispatchGroupedClaimedRows_OutcomeUnknownHoldsWithoutFallback` |
| stale `SENDING`은 `QUARANTINED`로 수렴 | `TestQuarantineStaleSendingMarksTerminalAndAggregateFailed` |
| stale failure writer는 `SENT`를 덮어쓰지 않음 | `TestLegacyMarkFailedMethodsDoNotOverwriteSentRows` |
| delivery success와 tracking은 한 transaction | `TestMarkSentBatchIfLockedPersistsTrackingAfterSendingGate` |
| claim defer는 attempt를 소비하지 않음 | `TestDispatchDeliveryRowsContendingWorkerDefersRoomWithoutConsumingAttempt` |
| later room success는 already-sent tracking을 수용 | `TestDispatchDeliveryRowsSendsAlreadySentPostToRoomWithoutSentRow` |
| revive는 `SENT`와 `QUARANTINED` evidence를 보존 | `TestReviveStaleFailedOutbox_RevivesFreshNeverSentAndPreservesDelivered`, `TestReviveStaleFailedOutbox_MixedFailedAndQuarantinedResetsFailedOnly` |

실행한 기준선:

```text
go -C hololive/hololive-alarm-worker test ./internal/egress/youtubedispatch/...
go -C hololive/hololive-shared test ./pkg/service/youtube/poller/runtime/batchrepo/... ./pkg/service/youtube/tracking/observation/...
```

두 명령 모두 통과했습니다.

## T01 종료 조건

- 모든 현재 lifecycle writer와 runtime owner를 위 표로 분류했습니다.
- Unbounded source/manual replay를 확인했으므로 production completion과 cutover를 명시적으로 차단했습니다.
- 삭제된 historical row를 추정하지 않으며 `legacy_coverage_start_at`을 비워 둡니다.
- T04 compatibility 단계 뒤 production read-only audit와 승인된 historical coverage 판정이 없으면 T05 completion marker, T07 activation, T08 cleanup activation으로 진행하지 않습니다.
