# 알람 발송: 오래된 선점 조회가 완료 행을 되살리는 경합

동기화 커밋: ee8a7688136cffb44d827a0ebf2e6de0e0fa9278 (claim_due 상태 재검사 반영분은 아직 커밋 전 작업 트리 기준)

epoch 4 기준선은 delivery/send-unit의 PK·FK, 요청 ID와 unit key의 유일성, 상태 제약을 보존한다. `ClaimDue`의 최종 UPDATE는 대상 delivery의 최신 상태가 `pending`·`retry`인지 다시 검사한다. 이 검사로 다중 소비자 사이의 오래된 스냅샷 경합(H1)을 막는다. 기본 설정은 네 속성을 모두 검사하며 2,506 → 1,729개 생성, 641개 고유, 대기 0으로 통과한다.

**H1 (해소됨).** 한 소비자가 `ClaimDue`의 조회 스냅샷을 얻고 잠시 멈춘 사이 다른 소비자가 같은 delivery를 선점하고 성공 정산할 수 있다. 재검사 전에는 앞의 소비자가 재개하면서 `sent_at`이 있는 행을 다시 `leased`로 바꾸고 발송 진입까지 할 수 있었다. 배포 구성은 활성 `alarm_dispatch` worker를 하나만 허용하므로 운영에서는 도달하지 않았다. 다만 SQL 자체는 단일 소비자를 강제하지 않는다. 지금은 위 Go 통합 테스트가 일회용 PostgreSQL 19에서 이 경합을 재현하고, 수정 전 SQL에서는 실패하는 것을 확인했다.

| 설정 | 속성 | 검증하는 약속과 피해 |
|---|---|---|
| 기본 | `SendNeedsLiveLease` | 발송 진입 시 `leased`인 행의 현재 소유자이고 미만료여야 한다. 아니면 권한을 잃은 작업자가 외부 발송을 시작한다. |
| 기본 | `SettlementNeedsSendingOwner` | 성공 정산은 현재 `sending` 행의 소유자만 적용한다. 아니면 다른 작업자의 시도를 자신의 결과로 확정한다. |
| 기본 | `SettledStaysSent` | 같은 성공 세대에서 한번 `sent_at`이 기록된 행은 선점으로 `sent`에서 벗어나지 않아야 한다. 아니면 전달 기록과 다음 작업의 판단이 어긋난다. |
| 기본 | `AtMostOneTransportCall` | 오류나 재발급 없이 한 고정 요청을 보낼 때 서로 다른 소비자가 같은 요청으로 두 번 전송 함수를 호출하지 않아야 한다. 아니면 이미 수락됐을 수 있는 요청을 또 제출한다. |

## 코드와 단계

`D`는 `hololive/hololive-alarm-worker/internal/service/alarm/dispatchoutbox/`, `R`은 `hololive/hololive-alarm-worker/internal/egress/alarmdispatch/`다. `D/repository_integration_test.go`의 `TestPgxRepositoryClaimDue_ConcurrentWorkersClaimDisjointRows`와 `TestPgxRepositoryClaimDue_ConcurrentWorkersKeepOneCanonicalGroupAtomic`은 중복 없는 선점을 명시한다. `D/claim_due_stale_snapshot_test.go`의 `TestClaimDueStaleSnapshotDoesNotReleaseSettledDelivery`는 B의 문장 스냅샷 뒤·시계 샘플 앞에서 B를 멈추고, A의 선점과 `sent` 정산이 커밋된 뒤 B를 재개해 완료 행이 그대로 남는지 확인한다.

| 모델 단계 | 실제로 끊을 수 있는 지점 |
|---|---|
| `ClaimSnapshot` | `D/repository_claim.go`의 `PgxRepository.ClaimDue`에서 실행한 `D/queries/claim_due.sql`의 READ COMMITTED 문장 스냅샷 획득. `D/queries/delivery_ready_predicate.sql`로 판정할 한 행의 `retry` 상태를 본다. `D/queries/delivery_ready_snapshot.sql`의 별도 모니터링 조회를 선점 권한으로 쓰는 모델이 아니다. |
| `SampleClaimClock` | 같은 문장의 `clock` CTE가 `clock_timestamp()`를 한 번 평가한다. 읽을 버전을 결정하는 스냅샷과 DB 시각 샘플은 다르다. |
| `LockSendUnit`, `SkipLocked` | 같은 문장의 `locked_units`가 `FOR UPDATE OF u SKIP LOCKED`로 **단위 행**을 잠그거나 건너뛴다. A가 이미 커밋했다면 건너뛸 잠금이 없다. |
| `ClaimDue`, `SkipSettledClaim` | `picked`가 스냅샷에서 본 멤버 ID로 `updated`가 delivery의 상태·소유자·만료를 쓴다. 밖에서 보이는 효과는 한 문장의 커밋이다. 외부 UPDATE는 `d.status IN ('pending','retry')`를 다시 검사한다. READ COMMITTED에서 스냅샷 뒤에 바뀐 행은 최신 버전으로 이 조건을 재평가하므로, 이미 선점·정산된 행은 건너뛴다. due 시각은 재검사하지 않는다. 한 단위의 멤버는 due가 달라도 함께 선점한다는 기존 계약(`TestAlarmDispatchClaimTakesWholeUnitAcrossMemberDueSkew`) 때문이다. |
| `BeginSendRequest` | `D/consumer.go`의 `MarkSending` → `D/send_request.go`의 `BeginSendRequest` → `D/queries/send_request_begin.sql`. 단위와 멤버를 잠근 뒤 DB 시각으로 전원을 검사하고 `sending`으로 바꾼다. 모델에서는 요청 ID·멤버·attempt가 고정되어 그 비교는 항상 참이다. |
| `SendPinnedRequest` | `R/alarm_dispatch_runner.go`의 `dispatchPreparedMessageGroup` → `sendPreparedRequest` → `sender.SendPreparedMessage`. 발송 진입이나 그 뒤의 DB 정산과는 별개인 외부 호출 지점이다. 취소되지 않고 성공으로 돌아오는 경로만 택한다. |
| `MarkSent` | `D/consumer.go`의 `MarkDispatched` → `D/repository_transitions.go`의 `PgxRepository.MarkSent` → `D/queries/mark_sent.sql`. 현재 `sending`이고 소유자·attempt·멤버가 일치해야 한다. 이미 성공한 외부 호출의 정산이므로 만료 여부는 검사하지 않는다. |
| `RecoverExpiredLeased`, `QuarantineStaleSending`, `ClockTick` | `D/consumer.go`의 `maybeRecover`, `D/queries/{recover_expired_leased,quarantine_stale_sending}.sql`: `leased`는 `expires_at < now`에 retry로, 오래된 `sending`은 결과 불명으로 격리한다. 단위 잠금이 없는 격리와 단위 잠금이 필요한 회수를 구분한다. 시계는 이산적으로 진행한다. |

## 실행과 음성 대조

```sh
m=verification/tla/AlarmDispatchClaim/AlarmDispatchClaim
TLC_WORKERS=2 bash /home/kapu/work/iris-stack/tools/lint/verify-tools.sh tlc "$m.tla" "$m.cfg"
```

고정 `tla2tools` 1.7.4 배포본(TLC 배너 2.19)에서 종료 0, 1,729개 생성, 641개 고유, 대기 0이다. 아래 음성 대조는 원본을 그대로 두고 복사본에서 한 가지 조건만 되돌린 결과다.

| 속성 | 잘못된 변형 | 관찰한 결과 |
|---|---|---|
| `SendNeedsLiveLease` | `send_request_begin.sql`에서 전원 검사 중 `other.lease_valid`만 제거 | 종료 12. 선점 시계 샘플 뒤 시간이 지나 만료 시각에 도달했는데 `sending`에 진입했다. |
| `SettlementNeedsSendingOwner` | `mark_sent.sql` UPDATE에서 `checked.owner_matches` 제거 | 종료 12. B가 다시 선점하고 `sending`에 진입한 뒤 A의 옛 요청이 B 대신 정산됐다. |
| `SettledStaysSent` | `ClaimDue`의 상태 재검사 제거 | 종료 12. 1,440개 생성 시점에 A의 정산 뒤 B가 `sent` 행을 다시 `leased`로 바꿨다. |
| `AtMostOneTransportCall` | 같은 변형에서 외부 호출 수만 검사 | 종료 12. 1,815개 생성 시점에 B가 같은 고정 요청으로 두 번째 호출에 도달했다. |

## 범위와 해석

단위 1개, 멤버 1개, 서로 다른 소비자 2명이며 각자 한 번만 `ClaimDue`를 호출한다. 요청·방·attempt·멤버십은 고정하고 `next_attempt_at`은 처음부터 due다. DB 시각 0~2, TTL과 격리 간격은 각각 1틱이다. 같은 요청과 소유자의 읽기만 수행하는 준비 단계는 발송 진입 직전 성공하는 경우로 투영했다. 단위 행을 선점한 구간을 별도 모델링하며, 나머지 쓰기는 최종 조건 검사·갱신 효과로 표현한다. 읽기 fence 미스·시간 초과·프로세스 중단의 무변화는 대기로도 표현할 수 있다. 이 모델은 모든 PostgreSQL 실행 계획이나 고립 수준의 시뮬레이터가 아니다.

`sent`는 Iris 요청 수락 정산이지 방 도착 확인이 아니다. `sending` 도중 중단하면 격리할 수 있으므로 무조건적인 최소 한 번 전달도 약속하지 않는다. 여러 멤버의 부분 성공, 재발급·handoff, 사용자에게 보인 메시지 수와 공급자 멱등성은 검증하지 않았다. 특히 `internal/egress/youtubedispatch/store/queries/{transition_claim_pending,delivery_ready_predicate}.sql`은 다른 `youtube_notification_delivery` 테이블의 `row_version`·상태 판정·delivery 자체 행 잠금을 사용하는 별도 경로다. 이 발견이나 통과 결과를 그 코드에 일반화하면 안 된다.
