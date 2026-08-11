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

deploy 계층에서는 `scripts/architecture/ci-notification-egress-gate.sh`가 두 가지를 단언합니다.

- `ALARM_WORKER_EGRESS_LEASE_ENABLED`가 compose에 되살아나지 않을 것. 이 env를 읽는 런타임이 더는 없으므로, 재도입은 proactive egress에 Valkey 가용성 의존만 다시 붙이는 결과가 됩니다.
- alarm-worker 블록이 `NOTIFICATION_SCHEDULER_ROLE: "worker"` 리터럴을 고정할 것. validator가 `off`를 허용하게 되었으므로, 이 리터럴이 유일 인스턴스를 감지기 없이 기동시키는 오배포를 막는 deploy 계층 방어선입니다.

### 코드 위치

```text
hololive/hololive-alarm-worker/internal/service/workerruntime/runtime_alarm_worker.go
  NewNotificationEgressRunner(runners, logger) — leaseCache/leaseEnabled 인자와 lease 분기 제거

hololive/hololive-alarm-worker/internal/egress/lease.go
  삭제됨. NotificationEgressLeaseKey / AcquireNotificationEgressLease / RenewLoop / Release

hololive/hololive-shared/pkg/config/settings/runtime_role_validation.go
  validateProductionAlarmWorkerOwnership — egress role은 owner 강제
  requireNotificationRoleEnv(notificationSchedulerRoleEnv, worker, off) — scheduler role 두 값 열거

hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/repository_claim.go
  PgxRepository.ClaimDue — 행 단위 배타성의 실제 소유자

deploy/compose/docker-compose.prod.yml
  hololive-alarm-worker 서비스 블록: container_name, ports, NOTIFICATION_* env

scripts/architecture/ci-notification-egress-gate.sh
  lease env 부재 단언 + NOTIFICATION_SCHEDULER_ROLE 리터럴 단언
```

---

## D-002. replica는 1을 유지한다 — replica>1의 게이트 목록

### 결정

`hololive-alarm-worker`는 replica=1로 유지합니다. 아래 (a)~(h) 게이트가 모두 해소되기 전에는 replica를 올리지 않습니다.

이 게이트들은 확장 시점에 해소할 목록이지 현행 배포의 결함 목록이 아닙니다. 단일 인스턴스에서는 아래 불변식이 인스턴스 경계 자체로 성립합니다.

### 이유

실제로 지켜야 하는 불변식은 행 단위 배타성이 아니라 **"(room, minute-bucket)당 1개 메시지"**입니다. 이 canonical group은 `alarmDispatchTimeGroupKey`가 `roomID|scheduled|<unix/60>` 형태로 만듭니다.

`ClaimDue`는 행 단위로만 배타적입니다. 그룹 원자성은 어디에서도 보장되지 않습니다 — 배치 `LIMIT`이 그룹 경계와 무관하게 잘리고, 이미 claim된 행은 `status`가 `leased`로 전이되어 다음 워커의 `WHERE status IN ('pending','retry')` 조회에서 빠지므로, 하나의 canonical group이 여러 워커에 나뉘어 claim될 수 있습니다.

`FOR UPDATE SKIP LOCKED`는 이 분절의 원인이 아닙니다. `ClaimDue`는 명시적 트랜잭션 없이 `pool.Query`로 도는 단일 auto-commit statement이고 반환 직전 커넥션을 반납하므로, 함수가 반환된 시점에 row lock은 이미 해제되어 있습니다. `SKIP LOCKED`는 동일 배치가 동시에 겹치는 순간에만 관여하며, 쿼리에서 지워도 `LIMIT`과 status 전이에 의한 분절은 그대로 발생합니다.

분절이 중복 발송으로 이어지는 이유는 **`ClientRequestID` 계산에 그룹 구성이 들어가기 때문**입니다.

- text 경로: `alarmDispatchClientRequestID(group, 0, len(group.envelopes))`가 그룹의 모든 envelope `DispatchOutboxID`와 `(start, end)` 범위를 함께 해싱합니다. 분절된 각 워커는 서로 다른 envelope 집합을 보므로 서로 다른 ID를 만듭니다.
- karing 경로: `alarmDispatchKaringChunkClientRequestID(roomID, identities)`가 chunk의 item identity 집합을 해싱합니다. 그룹이 쪼개지면 chunk 구성이 달라지므로 이 경로에서도 ID가 달라집니다.

즉 양쪽 경로 모두에서 분절된 워커들이 서로 다른 `ClientRequestID`를 만들고, **Iris 멱등성으로도 중복 발송을 걸러낼 수 없습니다**. 참고로 production compose는 `ALARM_DISPATCH_KARING_ENABLED` 기본값이 `false`이므로 현재 라이브 경로는 text 경로입니다.

### 근거 테스트

두 테스트 모두 `hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/repository_integration_test.go`에 있으며 `//go:build integration` 태그가 붙어 있습니다.

- `TestPgxRepositoryClaimDue_ConcurrentWorkersSplitOneCanonicalGroup` — 같은 room, 같은 minute bucket 3행을 worker-1과 worker-2가 각각 `limit=2`로 동시에 claim합니다. 3행에 워커당 상한 2행이므로 어떤 인터리빙에서도 한 워커가 그룹 전체를 가져갈 수 없어 분할이 결정적으로 보장됩니다. 단언은 합집합=3행, 교집합=∅, 두 워커 모두 1행 이상.
- `TestPgxRepositoryClaimDue_ConcurrentWorkersClaimDisjointRows` — 행 단위로는 배타적임(합집합=전체, 교집합=∅)을 고정합니다.

두 테스트를 함께 읽으면 결론이 나옵니다. 문제는 행 배타성이 아니라 **그룹 원자성**입니다.

### 게이트 목록

**(a) group-affinity claim.** persisted `dispatch_group_key` 기반의 원자적 그룹 claim, 또는 선정 전에 획득해 claim 트랜잭션을 관통하는 advisory lock이 필요합니다. `alarm_dispatch_deliveries`에는 현재 그룹 키 컬럼이 없고 그룹핑은 claim 이후 메모리에서만 일어납니다. *claim 시점에만 잡는 lock은 불충분합니다 — 외부 발송 전에 풀려서 다른 워커가 같은 그룹의 나머지를 가져갈 수 있습니다.*

**(b) YouTube dispatcher 내부 background loop 조율.** `Dispatcher.Start`가 `aggregateSyncLoop`, `telemetryLoop`, `cleanupLoop`, `reviveLoop`를 인스턴스마다 무조건 기동합니다. 인스턴스가 늘면 이 루프들이 그대로 중복 수행됩니다.

**(c) compose `container_name`과 고정 호스트 포트.** 현재 단일 인스턴스를 강제하는 바로 그 장치가 replica>1을 물리적으로 막습니다. 스케일 배선 자체가 선행 작업입니다.

**(d) dedup `LocalFallback` fail-open.** `LocalFallback.TryClaimOnOutage`는 공유 백엔드 오류 시 프로세스 로컬 맵으로 claim을 시도하고 그 결과를 그대로 반환합니다. 인스턴스가 늘수록 조율 백엔드 장애 구간에서 중복이 통과할 확률이 커집니다.

**(e) canonical-group 분절 관측 counter 부재.** `alarm_dispatch_metrics.go`의 메트릭에는 그룹 분절을 세는 지표가 없습니다. 분절이 실제로 일어났는지 운영에서 알 방법이 없으면 게이트 해소 여부를 측정할 수 없고, 전환 판단도 할 수 없습니다.

**(f) `dispatchrun` 쪽 대응 단위 테스트 부재.** "분절된 그룹이 서로 다른 `ClientRequestID`를 만든다"를 직접 고정하는 테스트가 `hololive/hololive-alarm-worker/internal/service/dispatchrun`에 없습니다. 인접 커버리지는 있습니다 — `TestAlarmDispatchClientRequestID`가 `(start, end)` 범위 변화와 단일 envelope의 `DispatchOutboxID` 변화가 ID를 바꾼다는 것까지는 고정하고, `alarm_dispatch_karing_request_id_test.go`의 테스트들은 karing chunk ID를 다룹니다. 그러나 하나의 canonical group이 두 워커로 쪼개진 시나리오 자체를 구성하는 테스트는 없습니다. 현재는 dispatchoutbox 쪽에서 절반만 잠겨 있습니다.

**(g) 근거 테스트가 기본 pre-push 게이트에서 실행되지 않는다.** `scripts/ci/local-ci.sh`의 `RUN_INTEGRATION_TESTS` 기본값이 `false`이고 `scripts/ci/pre-push-gate.sh`는 `local-ci.sh` 호출 시 `RUN_RACE_TESTS`만 전달할 뿐 `RUN_INTEGRATION_TESTS`를 설정하지 않습니다. 따라서 위 두 통합 테스트는 `RUN_INTEGRATION_TESTS=true`와 PostgreSQL이 있을 때만 돕니다. 평시 무조건 도는 것은 `check_integration_tag_compilation`의 `go vet -tags=integration` 컴파일 검증뿐입니다. 또한 race 스텝은 `-tags=integration` 없이 `go test -race`를 돌리므로 이 동시성 테스트들은 상시 race 대상이 아닙니다. 이는 이번 변경이 만든 문제가 아니라 기존 CI 인프라의 성질이며 이번 범위에서 고치지 않습니다. 다만 "이 테스트들이 replica>1을 자동으로 막아 주지는 않는다"는 사실을 기록해 둡니다.

**(h) 세 dispatcher 경로의 claim 락 커버리지가 고르지 않다.** egress 배타성이 이제 전적으로 PG 레벨 claim 락에 의존하므로, 그 락이 세 경로 모두에서 성립하는지가 확장의 전제입니다. 확인된 것과 확인되지 않은 것은 다음과 같습니다.

| 경로 | claim 락 | 동시성 테스트로 확인된 것 |
|---|---|---|
| alarm dispatch outbox (`dispatchoutbox`) | `repository_claim_0053_02.sql`의 `FOR UPDATE SKIP LOCKED` | 행 단위 배타성만. 그룹 원자성은 성립하지 않음 — (a) 참조 |
| generic notification delivery outbox (`pkg/service/delivery`) | `outbox_repository_0129_03.sql`의 `FOR UPDATE OF o SKIP LOCKED` + `locked_by`/`lock_expires_at` lease | 없음. lease 만료 회수와 stale worker fence는 고정되어 있으나(`TestFetchAndLock_ReclaimsExpiredLease`, `TestMarkSent_FenceRejectsStaleWorkerAfterReclaim`), 두 워커가 동시에 `FetchAndLock`을 호출하는 시나리오를 고정하는 테스트는 없음 |
| YouTube outbox (`alarm-worker/internal/egress/youtubedispatch`) | `dispatcher_claim_0050_01.sql`의 `FOR UPDATE SKIP LOCKED` | 별개 `Dispatcher` 인스턴스 2개가 하나의 DB를 공유해 같은 delivery row를 경합해도 post당 1회만 전송이 시작됨 — `TestDispatchDeliveryRowsConcurrentExecutionsStartCommunityShortsDeliveryOncePerPost`. community post와 short kind에 한함 |

"락이 있다"와 "중복 전송이 불가능하다"는 다른 명제입니다. (a)가 바로 그 반례로, `dispatchoutbox`는 행 락이 정상 동작하는데도 그룹 단위 불변식은 지켜지지 않습니다. 세 경로 중 cross-instance 중복 전송 불가가 테스트로 고정된 것은 YouTube outbox 경로뿐이며, 그것도 두 kind에 한정됩니다. egress 전용 인스턴스를 실제로 띄우기 전에 나머지 두 경로를 각각 확인해야 합니다.

### 코드 위치

```text
hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/repository_claim.go
  PgxRepository.ClaimDue — pool.Query 단일 auto-commit statement

hololive/hololive-shared/pkg/service/delivery/outbox_repository.go
  OutboxRepository.FetchAndLock — 동일 형태의 단일 statement claim
hololive/hololive-shared/pkg/service/delivery/queries/outbox_repository_0129_03.sql
hololive/hololive-alarm-worker/internal/egress/youtubedispatch/queries/dispatcher_claim_0050_01.sql
hololive/hololive-alarm-worker/internal/egress/youtubedispatch/dispatcher_claim_gate_test.go
  두 Dispatcher 인스턴스 경합 테스트

hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/queries/repository_claim_0053_02.sql
  WHERE status IN ('pending','retry') / LIMIT $1 / FOR UPDATE SKIP LOCKED / status='leased' 전이

hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/repository_integration_test.go
  분절 및 행 배타성 근거 테스트 (//go:build integration)

hololive/hololive-alarm-worker/internal/service/dispatchrun/alarm_dispatch_group.go
  alarmDispatchTimeGroupKey — roomID|scheduled|<minuteBucket>

hololive/hololive-alarm-worker/internal/service/dispatchrun/alarm_dispatch_karing.go
  alarmDispatchClientRequestID, alarmDispatchKaringChunkClientRequestID

hololive/hololive-alarm-worker/internal/service/dispatchrun/alarm_dispatch_runner.go
  sendAlarmDispatchMessage(text 경로), dispatchKaringContentListGroup(karing 경로)

hololive/hololive-alarm-worker/internal/service/dispatchrun/alarm_dispatch_metrics.go
  현재 메트릭 집합 — 분절 counter 없음

hololive/hololive-alarm-worker/internal/egress/youtubedispatch/dispatcher.go
  Dispatcher.Start — aggregateSyncLoop / telemetryLoop / cleanupLoop / reviveLoop

hololive/hololive-shared/pkg/service/alarm/dedup/fallback.go
  LocalFallback.TryClaimOnOutage — fail-open

scripts/ci/local-ci.sh, scripts/ci/local-ci-integration.sh, scripts/ci/pre-push-gate.sh
  RUN_INTEGRATION_TESTS 기본값과 integration/race 스텝 구성
```

---

## 범위 밖

감지기 쪽은 이번 결정의 범위가 아니며 동결 상태입니다. detection scheduler, `TieredScheduler`, dedup의 fail-closed 전환, jitter는 이 문서에서 변경하지 않고 판단하지도 않습니다. 이번 결정은 egress 경로의 배타성 소유와 replica 게이트에 한정됩니다.
