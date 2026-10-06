# alarm-worker egress 수평확장 결정서

작성일: 2026-07-30 KST
대상 저장소: `park285/hololive-bot`
대상 런타임: `hololive-alarm-worker`
상태: 확정된 결정 기록. replica 판단의 기준 문서.

---

## 이 문서의 목적

proactive notification egress의 배타성을 무엇이 소유하는지, 그리고 `hololive-alarm-worker`를 replica>1로 올리려 할 때 무엇이 게이트인지를 코드 위치와 함께 고정합니다. 향후 "alarm-worker를 수평확장하자"는 제안은 D-002의 게이트 목록으로 판단합니다.

---

## D-001. broad Valkey egress lease를 삭제하고 두 production profile을 열거한다

### 결정

`notification:egress-owner:alarm-worker` lease와 `ALARM_WORKER_EGRESS_LEASE_ENABLED` 플래그를 삭제합니다. production validator는 단일 profile 강제 대신 두 profile을 열거합니다.

```text
{NOTIFICATION_SCHEDULER_ROLE=worker, NOTIFICATION_EGRESS_ROLE=owner}  현행 단일 인스턴스
{NOTIFICATION_SCHEDULER_ROLE=off,    NOTIFICATION_EGRESS_ROLE=owner}  미래 egress 전용 인스턴스
```

`NOTIFICATION_EGRESS_ROLE=owner`는 계속 강제됩니다. 완화된 축은 scheduler role 하나뿐입니다.

### 이유

**1. 배제 효과 0.** replica=1에서 이 lease는 아무것도 배제하지 못했습니다. compose의 `container_name: hololive-alarm-worker`와 고정 호스트 포트(`127.0.0.1:30007:30007`, 동일 포트의 udp, `127.0.0.1:30097:30097`, `127.0.0.1:30067:30067`)가 이미 두 번째 인스턴스의 기동 자체를 막습니다. lease가 배제할 대상이 애초에 존재할 수 없었습니다.

**2. 가용성 마이너스.** 삭제 전 `acquireLease`는 `leasepkg.ErrHeld`인 경우에만 "다른 소유자가 있다"로 처리해 재시도 루프로 보냈고, 그 밖의 오류(Valkey 순단, dial 실패)는 그대로 상위 `startWithLease`로 전파되어 러너 기동을 막았습니다. 또한 `handleLeaseRenewLoopResult`가 renew 실패를 러너 그룹 종료로 바꾸었으므로, 정상 동작 중인 egress 러너 그룹이 Valkey 장애만으로 통째로 멈췄습니다. 배제 이득 없이 Valkey 장애를 egress 장애로 증폭하는 구조였습니다.

### 삭제 후 배타성 소유자

| 층위 | 소유 장치 |
|---|---|
| 행 단위 | PG `FOR UPDATE SKIP LOCKED` row-claim (`ClaimDue`) |
| 인스턴스 단위 | compose 단일 인스턴스 (`container_name` + 고정 호스트 포트) |

### profile 열거의 범위 — 동시 운영 승인이 아니다

두 profile을 열거한 것은 **egress 담당 인스턴스를 교체할 수 있게** 한 것이지, 두 인스턴스를 동시에 띄우는 것을 승인한 것이 아닙니다.

lease 삭제와 scheduler role 완화의 결과로, **validator는 더 이상 여러 alarm-worker 인스턴스가 동시에 `egress=owner`인 것을 막지 않습니다.** 이전에는 Valkey lease가 그 역할을 명목상 맡고 있었습니다. `{scheduler=worker, egress=owner}`와 `{scheduler=off, egress=owner}`를 **함께** 배포하면 그것은 이름만 다른 replica>1이며, D-002의 게이트 목록이 그대로 적용됩니다.

이 조합을 막는 것은 validator가 아니라 compose 단일 인스턴스 구성(`container_name` + 고정 호스트 포트)과 deploy 계층 게이트뿐입니다. 현행 배포는 단일 인스턴스이므로 이 경계가 실제로 성립합니다.

### deploy 계층 단언

deploy 계층에서는 `scripts/architecture/ci-notification-egress-gate.sh`가 alarm-worker compose 블록에 대해 다음을 단언합니다.

- `NOTIFICATION_SCHEDULER_ROLE: "worker"` 리터럴을 고정할 것. validator가 `off`를 허용하게 되었으므로, 이 리터럴이 유일 인스턴스를 감지기 없이 기동시키는 오배포를 막는 deploy 계층 방어선입니다.
- `container_name`과 고정 loopback 호스트 포트를 유지하고 `replicas`를 선언하지 않을 것. 이 셋이 단일 인스턴스를 보장합니다.

### 코드 위치

```text
hololive/hololive-alarm-worker/internal/service/workerruntime/runtime_alarm_worker.go
  NewNotificationEgressRunner(runners, logger) — leaseCache/leaseEnabled 인자와 lease 분기 제거

hololive/hololive-alarm-worker/internal/egress/lease.go
  삭제됨. NotificationEgressLeaseKey / AcquireNotificationEgressLease / RenewLoop / Release

hololive/hololive-shared/pkg/config/settings/runtime_role_validation.go
  validateProductionAlarmWorkerOwnership — egress role은 owner 강제
  requireNotificationRoleEnv(notificationSchedulerRoleEnv, worker, off) — scheduler role 두 값 열거

hololive/hololive-alarm-worker/internal/service/alarm/dispatchoutbox/repository_claim.go
  PgxRepository.ClaimDue — 행 단위 배타성의 실제 소유자

deploy/compose/docker-compose.prod.yml
  hololive-alarm-worker 서비스 블록: container_name, ports, NOTIFICATION_* env

scripts/architecture/ci-notification-egress-gate.sh
  lease env 부재 단언 + NOTIFICATION_SCHEDULER_ROLE 리터럴 단언
```

---

## D-002. replica는 1을 유지한다 — replica>1의 게이트 목록

### 결정

`hololive-alarm-worker`는 replica=1로 유지합니다. 아래 (a)~(i) 게이트가 모두 해소되기 전에는 replica를 올리지 않습니다.

이 게이트들은 확장 시점에 해소할 목록이지 현행 배포의 결함 목록이 아닙니다. 단일 인스턴스에서는 아래 불변식이 인스턴스 경계 자체로 성립합니다.

### 이유

지켜야 하는 불변식은 행 단위 배타성이 아니라 **"한 알림 묶음은 한 메시지로, 같은 `ClientRequestID`로"**입니다. 이 묶음의 저장 단위는 send unit입니다. dispatchoutbox가 delivery를 적재할 때(`dispatch_group.go`의 `assignSendUnits`) 같은 `dispatch_group_key`(room과 source 식별자, 또는 알림 유형·단계·`MinutesUntil`)의 delivery를 dedupe key 순으로 최대 10개씩 묶어 `alarm_dispatch_send_units`에 저장하고, 그 unit의 `client_request_id`(`hololive-alarm:<hash>`)도 이때 정합니다. send unit은 한 적재 batch 안에서만 만들어지므로 같은 room·같은 group key라도 다른 batch로 적재되거나 10개를 넘으면 unit(메시지)이 여러 개가 됩니다. 이것은 인스턴스 수와 무관한 적재 규칙입니다.

`ClaimDue`는 send unit 단위로 배타적입니다. `repository_claim_0053_02.sql`이 due delivery가 있는 send unit 행을 `FOR UPDATE OF u SKIP LOCKED`로 잠그고, 잠근 unit의 due delivery를 같은 statement에서 모두 `leased`로 전이합니다. 배치 상한은 unit 경계에서 자르며 첫 unit은 상한을 넘어도 통째로 가져갑니다(`ordinal = 1`). `ClaimDue`는 명시적 트랜잭션 없는 단일 auto-commit statement라 반환 시점에 row lock은 이미 풀려 있고, 이후 배타성은 `leased` 상태와 `locked_by` fence가 맡습니다.

alarm-worker는 claim한 봉투를 send unit별로 묶고(`alarmDispatchRegroupKey`), 저장된 send-unit `client_request_id`를 그대로 씁니다(`alarmDispatchClientRequestID`). 그룹 구성(envelope `DispatchOutboxID`와 범위)에서 ID를 파생하던 경로는 지웠습니다(stack-audit 2026-09-26 T17). 저장 ID가 비었거나 한 그룹에 서로 다른 ID가 섞이면 보내지 않고 발송 전 실패로 드러냅니다. karing 경로는 `DEC-20260926-hololive-karing-egress-disposition`에 따라 삭제했습니다(stack-audit 2026-09-26 T19). 모든 방은 message path를 씁니다.

따라서 동시 claim이 한 send unit을 쪼개 서로 다른 `ClientRequestID`를 만드는 경로는 코드와 아래 통합 테스트 기준으로 없습니다. 같은 unit을 다시 보내는 경우(재시도, `RecoverExpiredLeased`의 lease 만료 회수)는 같은 저장 ID를 쓰므로 Iris admission 중복 제거에 기댑니다. replica=1을 유지하는 이유는 이 재발송 구간의 cross-instance 경합이 테스트로 고정되지 않았고 (a)~(i) 게이트가 남아 있기 때문입니다.

### 근거 테스트

두 테스트 모두 `hololive/hololive-alarm-worker/internal/service/alarm/dispatchoutbox/repository_integration_test.go`에 있으며 `//go:build integration` 태그가 붙어 있습니다.

- `TestPgxRepositoryClaimDue_ConcurrentWorkersKeepOneCanonicalGroupAtomic` — 같은 room, 같은 minute bucket의 3행(한 send unit)을 worker-1과 worker-2가 각각 `limit=2`로 동시에 claim합니다. 단언은 합집합=3행, 교집합=∅, 행을 가져간 워커는 정확히 1개(한 send unit은 한 워커가 소유).
- `TestPgxRepositoryClaimDue_ConcurrentWorkersClaimDisjointRows` — 서로 다른 room 10행에서 행 단위 배타성(합집합=전체, 교집합=∅)을 고정합니다.

alarm-worker 쪽은 `hololive/hololive-alarm-worker/internal/service/dispatchrun/alarm_dispatch_retry_regroup_test.go`의 `TestAlarmDispatchClientRequestIDRequiresPersistedSendUnitIdentity`와 `TestAlarmDispatchRunnerRefusesGroupWithoutPersistedSendUnitIdentity`가 저장 ID만 쓰고 없으면 보내지 않는 계약을 고정합니다.

### 게이트 목록

**(a) send unit 재발송 구간의 cross-instance 경합.** 동시 claim에서 한 send unit이 한 워커에만 가는 것은 위 통합 테스트가 고정합니다. claim의 row lock은 statement가 끝나면 풀리므로, 그 뒤의 재시도·`RecoverExpiredLeased`(lease 만료 회수)·`QuarantineStaleSending` 경로에서 두 인스턴스가 같은 send unit을 다루는 경우는 테스트로 고정되지 않았습니다. 같은 unit의 재발송은 같은 저장 `ClientRequestID`를 쓰므로 Iris admission 중복 제거가 전제이며, replica>1 전에 이 전제를 경합 테스트로 확인해야 합니다.

**(b) YouTube dispatcher 내부 background loop 조율.** `Dispatcher.Start`가 `aggregateSyncLoop`, `telemetryLoop`, `cleanupLoop`, `reviveLoop`를 인스턴스마다 무조건 기동합니다. 인스턴스가 늘면 이 루프들이 그대로 중복 수행됩니다.

**(c) compose `container_name`과 고정 호스트 포트.** 현재 단일 인스턴스를 강제하는 바로 그 장치가 replica>1을 물리적으로 막습니다. 스케일 배선 자체가 선행 작업입니다.

**(d) dedup `LocalFallback` fail-open.** `LocalFallback.TryClaimOnOutage`는 공유 백엔드 오류 시 프로세스 로컬 맵으로 claim을 시도하고 그 결과를 그대로 반환합니다. 인스턴스가 늘수록 조율 백엔드 장애 구간에서 중복이 통과할 확률이 커집니다.

**(e) 분절·재발송 관측 counter 부재.** `alarm_dispatch_metrics.go`의 메트릭에는 한 알림 묶음이 여러 send unit으로 나뉜 경우나 같은 send unit의 인스턴스 간 재발송을 세는 지표가 없습니다. 이런 일이 실제로 일어났는지 운영에서 알 방법이 없으면 게이트 해소 여부를 측정할 수 없고, 전환 판단도 할 수 없습니다.

**(f) `dispatchrun` 쪽 cross-instance 테스트 부재.** `dispatchrun` 테스트는 한 runner 안에서 저장 ID만 쓰는 계약(위 근거 테스트)과 ambiguous 실패의 저장 ID 재시도를 고정합니다. 두 runner가 같은 send unit의 재발송을 경합하는 시나리오를 구성하는 테스트는 `hololive/hololive-alarm-worker/internal/service/dispatchrun`에 없습니다.

**(g) 근거 테스트가 기본 pre-push 게이트에서 실행되지 않는다.** `scripts/ci/local-ci.sh`의 `RUN_INTEGRATION_TESTS` 기본값이 `false`이고 `scripts/ci/pre-push-gate.sh`는 `local-ci.sh` 호출 시 `RUN_RACE_TESTS`만 전달할 뿐 `RUN_INTEGRATION_TESTS`를 설정하지 않습니다. 따라서 위 두 통합 테스트는 `RUN_INTEGRATION_TESTS=true`와 PostgreSQL이 있을 때만 돕니다. 평시 무조건 도는 것은 `check_integration_tag_compilation`의 `go vet -tags=integration` 컴파일 검증뿐입니다. 또한 race 스텝은 `-tags=integration` 없이 `go test -race`를 돌리므로 이 동시성 테스트들은 상시 race 대상이 아닙니다. 이는 이번 변경이 만든 문제가 아니라 기존 CI 인프라의 성질이며 이번 범위에서 고치지 않습니다. 다만 "이 테스트들이 replica>1을 자동으로 막아 주지는 않는다"는 사실을 기록해 둡니다.

**(h) 세 dispatcher 경로의 claim 락 커버리지가 고르지 않다.** egress 배타성이 이제 전적으로 PG 레벨 claim 락에 의존하므로, 그 락이 세 경로 모두에서 성립하는지가 확장의 전제입니다. 확인된 것과 확인되지 않은 것은 다음과 같습니다.

| 경로 | claim 락 | 동시성 테스트로 확인된 것 |
|---|---|---|
| alarm dispatch outbox (`dispatchoutbox`) | `repository_claim_0053_02.sql`의 send unit `FOR UPDATE OF u SKIP LOCKED` | 행 단위 배타성과 동시 claim의 send unit 원자성. 재발송 구간 경합은 미확인 — (a) 참조 |
| generic notification delivery outbox (`alarm-worker/internal/egress/notificationdelivery`) | `outbox_claim_ready.sql`의 방별 선행 항목 선택과 `FOR UPDATE` + `locked_by`/`lock_expires_at` lease | 방별 순서, lease 만료 회수, stale worker fence, 정산 경합은 해당 패키지의 DB 회귀로 검증한다. 여러 프로세스에서 같은 방의 발송과 독립 maintenance를 함께 수행하는 조건은 확장 전 별도 검증 대상이다. |
| YouTube delivery lifecycle (`alarm-worker/internal/egress/youtubedispatch/store`) | `transition_claim_pending.sql`의 `FOR UPDATE OF delivery SKIP LOCKED`와 `row_version` fence | 별개 `Dispatcher` 인스턴스 2개가 하나의 DB를 공유해 같은 delivery row를 경합해도 post당 1회만 전송이 시작됨 — `TestDispatchDeliveryRowsConcurrentExecutionsStartCommunityShortsDeliveryOncePerPost`. community post와 short kind에 한함 |

"락이 있다"와 "중복 전송이 불가능하다"는 다른 명제입니다. `dispatchoutbox`의 claim 락은 동시 claim에서 send unit을 쪼개지 않지만, 락이 풀린 뒤의 재발송 구간은 (a)처럼 따로 확인해야 합니다. 세 경로 중 cross-instance 중복 전송 불가가 테스트로 고정된 것은 YouTube outbox 경로뿐이며, 그것도 두 kind에 한정됩니다. egress 전용 인스턴스를 실제로 띄우기 전에 나머지 두 경로를 각각 확인해야 합니다.

**(i) 구독 변경과 cache rebuild의 프로세스 간 직렬화.** 현재 하나의 `AlarmService`가 같은 `cacheMutationMu`로 구독 변경과 전체 rebuild를 보호한다. 구독 변경은 commit 전에 positive set을 무효화하며 checker의 DB read-through는 set을 쓰지 않는다. replica>1에서는 다른 프로세스가 snapshot 이후에 해지를 commit한 뒤 오래된 rebuild가 SADD하는 경합이 가능하므로, 변경과 rebuild 전체를 함께 보호하는 프로세스 간 fence가 선행되어야 한다. SCAN 페이지 처리나 resolver별 singleflight는 이 fence를 대체하지 않는다.

### 코드 위치

```text
hololive/hololive-alarm-worker/internal/service/alarm/dispatchoutbox/repository_claim.go
  PgxRepository.ClaimDue — pool.Query 단일 auto-commit statement

hololive/hololive-alarm-worker/internal/egress/notificationdelivery/store.go
  Store.fetchReadyAndLock — 방별 첫 due 항목의 단일 statement claim
hololive/hololive-alarm-worker/internal/egress/notificationdelivery/queries/outbox_claim_ready.sql
hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store/queries/transition_claim_pending.sql
hololive/hololive-alarm-worker/internal/egress/youtubedispatch/dispatcher_claim_gate_test.go
  두 Dispatcher 인스턴스 경합 테스트

hololive/hololive-alarm-worker/internal/service/alarm/dispatchoutbox/queries/repository_claim_0053_02.sql
  send unit FOR UPDATE OF u SKIP LOCKED / unit 경계 LIMIT / 잠근 unit의 due delivery 전체 status='leased' 전이

hololive/hololive-alarm-worker/internal/service/alarm/dispatchoutbox/dispatch_group.go
  assignSendUnits — dispatch_group_key별 최대 10개 send unit과 client_request_id(hololive-alarm:<hash>) 결정

hololive/hololive-alarm-worker/internal/service/alarm/dispatchoutbox/repository_integration_test.go
  send unit 원자성 및 행 배타성 근거 테스트 (//go:build integration)

hololive/hololive-alarm-worker/internal/service/dispatchrun/alarm_dispatch_group.go
  alarmDispatchRegroupKey — send-unit|<id> (send unit 없는 봉투만 alarmDispatchTimeGroupKey 등 keyFunc)

hololive/hololive-alarm-worker/internal/service/dispatchrun/alarm_dispatch_client_request_id.go
  alarmDispatchClientRequestID(저장된 send-unit client_request_id)

hololive/hololive-alarm-worker/internal/service/dispatchrun/alarm_dispatch_runner.go
  sendAlarmDispatchMessage(text 경로)

hololive/hololive-alarm-worker/internal/service/dispatchrun/alarm_dispatch_metrics.go
  현재 메트릭 집합 — 분절 counter 없음

hololive/hololive-alarm-worker/internal/egress/youtubedispatch/dispatcher.go
  Dispatcher.Start — aggregateSyncLoop / telemetryLoop / cleanupLoop / reviveLoop

hololive/hololive-shared/pkg/service/alarm/dedup/fallback.go (`2bc32799f`에서 삭제)
  LocalFallback.TryClaimOnOutage — fail-open

scripts/ci/local-ci.sh, scripts/ci/local-ci-integration.sh, scripts/ci/pre-push-gate.sh
  RUN_INTEGRATION_TESTS 기본값과 integration/race 스텝 구성
```

---

## 범위 밖

감지기 쪽은 이번 결정의 범위가 아니며 동결 상태입니다. detection scheduler, `TieredScheduler`, dedup의 fail-closed 전환, jitter는 이 문서에서 변경하지 않고 판단하지도 않습니다. 이번 결정은 egress 경로의 배타성 소유와 replica 게이트에 한정됩니다.
