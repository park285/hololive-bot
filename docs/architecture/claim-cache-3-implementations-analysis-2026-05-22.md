# 2026-05-22 — Claim cache 3 구현체 차이 분석 (Phase 2.B.2 진입 전)

본 문서는 master plan Phase 2.B.2 의 "lease/claim 캐시 공통화" 진입 전 3 (또는 그 이상) 구현체의 TTL / 재시도 / token semantics / 사용 패턴 차이를 정리해 helper 본거지 결정과 마이그레이션 가능성을 평가한다. 본 문서는 결정 doc 만, helper 구현은 후속 task.

## 1. 구현체 카탈로그

### 1.1 `ingestionlease.Lease` — runtime singleton lease

- 위치: `hololive/hololive-youtube-producer/internal/runtime/ingestionlease/lease.go:34`
- 핵심 type / func: `Lease`, `Acquire(ctx, cacheSvc, role, logger) (*Lease, error)`, `(*Lease).StartRenewLoop`, `(*Lease).Release`
- Key 정책: `lock:ingestion:runtime` 단일 key 를 서비스 전체 runtime lock 으로 사용한다.
- TTL / renew interval: TTL 2분, renew gap 40초.
- 충돌 처리: acquire 는 `SetNX`; renew 는 `CompareAndExpire`; release 는 `CompareAndDelete`.
- ownership / token 정책: owner string 은 `role:pid:unix_nano`; renew/release 는 cache value 가 owner 와 같을 때만 성공한다. ownership mismatch 는 `errIngestionLeaseOwnershipLost` 또는 release mismatch error 로 드러난다.
- 외부 surface: `Acquire`, `StartRenewLoop`, `Release`. TTL 과 renew interval 은 내부 상수라 caller cfg 가 아니다.
- 재시도 정책: renew 에만 3회 retry 가 있고 base delay 1초, exponential multiplier, jitter 500ms 를 사용한다. ownership lost 는 retry 하지 않는다.

### 1.2 `ingestionlease.JobRunGuard` — active-active job lease + cooldown

- 위치: `hololive/hololive-youtube-producer/internal/runtime/ingestionlease/job_run_guard.go:17`
- 핵심 type / func: `JobIdentity`, `JobClaimStatus`, `JobRunGuard`, `JobRunClaim`, `TryClaim(ctx, identity, leaseTTL, cooldownTTL)`, `(*JobRunClaim).Renew`, `MarkCompleted`, `Release`
- Key 정책: namespace + hashed poller/channel tag 로 key-per-job 을 만든다. lease key 와 cooldown key 를 분리한다.
- TTL / renew interval: `leaseTTL` 과 `cooldownTTL` 은 caller 가 넘긴다. scheduler 는 `pollTimeout + 15s` 를 최소 1분으로 올려 lease TTL 로 쓰고 renew 는 `ttl / 3` 간격이다. photo-sync 는 lease TTL 2분, cooldown/retry 30초, renew 는 `ttl / 3` 이다.
- 충돌 처리: acquire/renew/complete/release 모두 Valkey Lua script 로 처리한다. acquire 는 cooldown key 존재 시 already-completed, lease `SET NX PX` 성공 시 acquired, 그 외 peer-owned 를 반환한다.
- ownership / token 정책: owner token 은 `instanceID:pid:unix_nano`; `JobClaimStatus.OwnerToken` 과 `JobRunClaim.OwnerToken()` 으로 노출된다. `MarkCompleted` 는 owner 가 맞을 때 cooldown key 를 쓰고 lease key 를 삭제한다.
- 외부 surface: `TryClaim`, `Renew`, `MarkCompleted`, `Release`, `LeaseKey`, `CooldownKey`, `OwnerToken`. `hololive-shared/pkg/service/youtube/poller` 의 `JobClaimer`/`JobClaim` interface 로도 감싸진다.
- 재시도 정책: store operation 자체 retry 는 없다. skip 시 retry-after 는 Valkey `PTTL` 이고 scheduler 는 `RetryAfter` 또는 `errorBackoffMin` 으로 reschedule 한다. photo-sync 는 `RetryAfter` 와 30초 fallback 중 더 작은 값을 기다린다.

### 1.3 outbox `dispatcher_claim_*` — delivery pre-send claim gate + batch-local reuse

- 위치:
  - `hololive/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatcher_claim.go:32`
  - `hololive/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatcher_claim_acquire.go:14`
  - `hololive/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatcher_claim_release.go:15`
  - `hololive/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatcher_claim_finalize.go:16`
  - `hololive/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatcher_claim_gate.go:15`
- 핵심 type / func: `deliveryClaimDecision`, `deliveryClaimToken`, `deliveryClaimReuseCache`, `tryClaimDelivery`, `selectClaimedDeliveries`, `releaseDeliveryClaims`, `finalizeClaimSuccess`, `finalizeClaimMiss`
- Key 정책: delivery row 자체 claim 은 row lock(`youtube_notification_delivery.locked_at`)이고, community/shorts pre-send claim 은 alarm state 의 `(kind, post_id)` per subject key 를 사용한다. reuse cache key 는 `kind + "\x00" + postID` batch-local identity 다.
- TTL / renew interval: delivery row lock timeout 은 default 5분. alarm state pre-send claim stale timeout 은 `min(cfg.LockTimeout, 2m)` 이며 default cfg 에서는 2분이다. renew 는 없다.
- 충돌 처리: delivery row fetch 는 SQL `FOR UPDATE SKIP LOCKED` + `locked_at`; pre-send claim 은 `TryClaimAlarmState` DB upsert 를 호출한다. stale claim 은 `ReleaseAlarmStateClaim` 으로 먼저 해제한 뒤 reload 한다.
- ownership / token 정책: token 은 cache owner string 이 아니라 `authorizedAt time.Time` 이다. `deliveryClaimToken{kind, postID, authorizedAt}` 은 send 실패 시 release 에 쓰이고, send 성공 시 `MarkSentBatch` 의 tracking mark 로 전달된다. reuse hit 은 decision 만 재사용하고 token 은 반환하지 않는다.
- 외부 surface: dispatcher 내부 surface 는 `selectClaimedDeliveries`, `tryClaimDelivery`, `releaseDeliveryClaims`; 저장소 surface 는 observation repository 의 `TryClaimAlarmState` / `ReleaseAlarmStateClaim` 이다.
- 재시도 정책: claim miss/error 는 `deliveryClaimDecisionRetryLater` 로 분류되고 delivery row failure bucket 에 들어가 `MarkFailedRetryBatch` 가 default 1분 `RetryBackoff` 로 next attempt 를 잡는다. alarm state claim 자체에는 retry-after 값이 없다.

### 1.4 observation `alarm_state_repository_claim` — DB alarm state claim store

- 위치: `hololive/hololive-shared/pkg/service/youtube/tracking/internal/observation/alarm_state_repository_claim.go:19`
- 핵심 type / func: `TryClaimAlarmState(ctx, record) (bool, error)`, `ReleaseAlarmStateClaim(ctx, kind, postID, authorizedAt) (bool, error)`
- Key 정책: `youtube_community_shorts_alarm_states` 의 primary key `(kind, post_id)` 가 subject key 이다.
- TTL / renew interval: repository 내부 TTL 은 없다. `authorized_at` 이 claim token 이며 expiry 판단은 outbox caller 의 `isStaleAlarmStateClaim` 이 맡는다.
- 충돌 처리: insert/upsert 는 `ON CONFLICT (kind, post_id) DO UPDATE ... WHERE authorized_at IS NULL AND alarm_sent_at IS NULL` 로 atomic 하게 선점한다. 성공 후 reload 로 `authorized_at` 일치를 확인한다.
- ownership / token 정책: token 은 normalized `authorized_at`. release 는 `alarm_sent_at IS NULL` 이고 `authorized_at = ?` 인 row 만 `authorized_at = NULL`, `delivery_status = DETECTED` 로 되돌린다.
- 외부 surface: `TryClaimAlarmState`, `ReleaseAlarmStateClaim`, 보조적으로 `FindAlarmStateByPostID` 와 sent mark 저장 경로가 claim status 확인에 쓰인다.
- 재시도 정책: repository 는 bool/error 만 반환하며 retry-after 를 계산하지 않는다. 호출부가 delivery retry queue 정책을 적용한다.

## 2. 호출부 비교 표

| 항목 | ingestionlease | outbox dispatcher_claim | alarm_state observation |
|------|----------------|-------------------------|--------------------------|
| Key 형태 | `Lease`: single per service. `JobRunGuard`: key-per poller/channel + cooldown key | per delivery row + per alarm state `(kind, post_id)` + batch-local identity | per alarm state `(kind, post_id)` |
| TTL | `Lease`: 2 min. `JobRunGuard`: caller `leaseTTL`; scheduler minimum 1 min, photo-sync 2 min | row lock default 5 min; pre-send stale timeout `min(LockTimeout, 2 min)` | 내부 TTL 없음; caller 가 stale 판단 |
| Renew | `Lease`: yes, 40s gap. `JobRunGuard`: yes, `ttl / 3` caller loop | 없음. stale 이면 release 후 reacquire | 없음 |
| Atomic 방식 | `SetNX`, `CompareAndExpire`, `CompareAndDelete`, 또는 Valkey Lua script | SQL row lock, `TryClaimAlarmState` upsert, batch-local mutex cache | SQL `INSERT ... ON CONFLICT ... WHERE authorized_at IS NULL AND alarm_sent_at IS NULL` |
| Token | owner string (`role:pid:nano` 또는 `instanceID:pid:nano`) | `authorizedAt time.Time` wrapped in `deliveryClaimToken`; reuse hit 은 token 없음 | `authorized_at time.Time` |
| 실패 시 | acquire conflict 는 error 또는 `PeerOwned`; renew ownership lost 는 terminal error; job skip 은 `RetryAfter` 로 reschedule | retry-later decision 후 delivery row `RetryBackoff` default 1 min; send failure 는 claim release best-effort | `false, nil` 또는 error; caller 가 retry/backoff 결정 |

## 3. 통합 가능성 분석

결론은 **(b) 부분 가능 — 일부는 통합, 일부는 보존** 이다.

### (a) 가능 — TTL/Token 정책이 cfg 로 흡수 가능

장점은 중복된 claim decision reuse, owner-token renew/release, retry-after 표현을 하나의 helper API 로 모을 수 있다는 점이다. `ClaimKey`, `ClaimStatus`, `ReuseCache`, `ClaimStore` 로 `RetryAfter`, holder, TTL 을 표현하면 Valkey job guard 와 alarm state claim 의 결과 shape 는 어느 정도 맞출 수 있다.

하지만 token 의미가 다르다. Valkey 계열 token 은 value ownership string 이고 renew 가능한 lease handle 이다. DB alarm state token 은 `authorized_at` 비교 값이며 caller 가 stale timeout 을 계산한다. 이를 하나의 token struct 로 강제하면 domain semantics 를 helper 로 끌어올리게 된다.

### (b) 부분 가능 — 일부는 통합, 일부는 보존

권장안이다. 공통 helper 는 cross-cutting doc 의 결정처럼 `ResolveClaim` / `ReuseCache` 중심의 claim decision reuse 와 `ClaimStatus` 표준화까지만 맡는다. 저장소별 atomic primitive 는 adapter 로 보존한다.

이 경우 outbox 의 `deliveryClaimReuseCache` 는 가장 먼저 공통화할 수 있다. `JobRunGuard` 와 `alarm_state_repository_claim` 은 각각 `ClaimStore` adapter 로 감쌀 수 있지만, Lua script 와 SQL upsert 조건은 그대로 유지해야 한다. `ingestionlease.Lease` singleton lock 은 같은 cache client primitive 를 쓰지만 subject-key claim/cache reuse 문제와는 거리가 있어 wrapper 적용 대상에 가깝다.

### (c) 통합 어려움 — domain semantics 가 너무 다름

통합을 완전히 보류하는 선택지는 token semantics drift 를 가장 안전하게 피한다. 특히 `authorized_at` 이 sent mark 의 optimistic condition 으로 이어지는 outbox 경로는 단순 cache lease 와 다르다.

다만 현재 중복의 핵심은 "claim 결과를 batch 안에서 재사용하고, 동일 subject 에 대해 acquired/peer-owned/already-completed/retry-later 를 표준화하는 orchestration" 이다. 이 부분은 domain contract 변경 없이 추출 가능하므로 전체 보류는 과도하다.

## 4. 본거지 결정

task 09 의 `hololive-shared/pkg/service/cache/claim` 본거지 결정은 **유효하다**.

근거:

- `hololive-shared/pkg/service/cache/interface.go` 가 이미 `SetNX`, `CompareAndExpire`, `CompareAndDelete`, Valkey builder 를 제공한다.
- youtube-producer 는 이미 `hololive-shared/pkg/service/cache` 를 의존하므로 새 shared-go dependency 역전이 필요 없다.
- outbox 와 observation 은 `hololive-shared` 내부이며, `internal` 경계 때문에 `hololive-shared/pkg/service/youtube/outbox/internal/delivery` 의 helper 를 다른 module 이 직접 재사용할 수 없다.
- task 09 는 helper 가 cache client 를 대체하지 않고 claim lifecycle orchestration 만 맡는다고 결정했으며, 본 분석에서도 이 경계가 맞다.

따라서 Phase 2.B.2 에서는 `hololive/hololive-shared/pkg/service/cache/claim` 에 helper 를 신설하고, 저장소별 adapter 는 기존 위치 또는 호출부 가까이에 둔다. `authorized_at` token, Lua script, SQL upsert 조건은 helper 로 끌어올리지 않는다.

## 5. 마이그레이션 가능 step list

1. helper signature freeze + 단위 테스트
   - 내용: `ClaimKey`, `ClaimStatus`, `ReuseCache`, `ResolveClaim` 의 필드를 확정하고, cache hit/miss/error 시 compute 호출 횟수와 reused bool 을 테스트로 고정한다.
   - stop rule: token value 를 helper public API 로 강제해야만 테스트가 통과하는 설계가 나오면 중단한다.
   - risk: 너무 넓은 interface 로 시작하면 DB/Valkey 세부 의미가 helper 로 새어 나온다.

2. outbox dispatcher `deliveryClaimReuseCache` 먼저 마이그레이션
   - 내용: `dispatcher_claim_gate.go` 의 batch-local decision reuse 를 `ReuseCache`/`ResolveClaim` 으로 교체한다.
   - stop rule: reuse hit 에서 token 이 nil 이어야 하는 현재 release/sent mark 정책을 보존할 수 없으면 중단한다.
   - risk: 같은 batch 의 grouped/individual send 경로에서 claim token 중복 release 또는 sent mark 누락이 생길 수 있다.
   - Task 24 확인(2026-05-22): Task 18 에 구현된 현재 `ReuseCache` interface 만으로는 이 step 을 바로 진행하지 않는다. `Claim(ctx, key, holder, ttl)` 는 hit 조회 API 가 아니라 miss 시 새 holder 를 저장하는 mutation API 이므로, `dispatcher_claim_gate.go` 의 기존 `resolve` 처럼 compute 전에 기존 decision 을 확인할 수 없다. 임의 holder 로 probe 하면 miss 에서 잘못된 decision holder 를 먼저 저장하고, compute 후 실제 decision 으로 바꿀 API 도 없다.
   - Task 24 확인(2026-05-22): 현재 outbox reuse cache 는 holder/token cache 가 아니라 batch-local decision memoization 이다. 첫 miss 는 `deliveryClaimToken{kind, postID, authorizedAt}` 를 반환하지만 reuse hit 은 의도적으로 token 을 `nil` 로 돌려 중복 release 와 중복 sent mark 를 막는다. `ReuseCache` 의 `Holder`, `ExpiresAt`, `RetryAfter`, `Release(holder)` semantics 로 이 contract 를 직접 표현하면 `authorized_at` token 과 cache holder 의 의미가 섞인다.
   - 후속 조건: 이 step 은 `ResolveClaim` 또는 동등한 get-or-compute helper 가 추가되어 "hit 에서는 compute 미호출 + token nil", "miss 에서는 compute 1회 + decision 저장", "compute error 는 저장하지 않음" 을 테스트로 고정한 뒤 재개한다. `ReuseCache` 를 그대로 적용하는 adapter 는 이번 task 에서 skip 한다.

3. alarm_state observation adapter 작성
   - 내용: `TryClaimAlarmState` / `ReleaseAlarmStateClaim` 을 `ClaimStore` adapter 로 감싸되 SQL 조건과 `authorized_at` equality 는 그대로 둔다.
   - stop rule: DB schema, queue payload, dedup key 정의 변경이 필요하면 중단한다.
   - risk: repository 내부에는 TTL 이 없으므로 caller stale timeout 을 helper cfg 로 착각하면 behavior drift 가 난다.

4. `JobRunGuard` adapter 작성
   - 내용: Lua script 기반 acquire/renew/complete/release 를 `ClaimStore` adapter 로 감싸고, `RetryAfter` 는 Valkey `PTTL` 결과를 유지한다.
   - stop rule: cooldown key 와 lease key 의 two-key atomic script 를 generic cache operation 으로 풀어야 한다면 중단한다.
   - risk: `MarkCompleted` 가 cooldown 을 생성하는 의미는 일반 release 와 다르므로 helper 에 완료 상태를 별도 모델링해야 한다.

5. `ingestionlease.Lease` wrapper 적용 여부 재평가
   - 내용: singleton runtime lock 은 Phase 2.B.2 helper 에 직접 넣기보다 `ClaimStore` adapter 또는 현상 유지 중 선택한다.
   - stop rule: single-key service leadership 과 per-subject claim 을 하나의 migration 으로 묶어 diff 가 커지면 중단한다.
   - risk: 현재 renew retry 정책(3 attempts, 1s base, 500ms jitter)을 잃으면 active runtime failover behavior 가 바뀐다.

6. 기존 dedicated helper 제거 또는 wrapper 화
   - 내용: migration 후 `deliveryClaimReuseCache` 같은 local helper 는 제거하거나 thin wrapper 로 남긴다.
   - stop rule: public/internal boundary 가 불명확해져 outbox internal type 이 shared package 로 역류하면 중단한다.
   - risk: 테스트가 adapter behavior 와 orchestration behavior 를 섞어 brittle 해질 수 있다.

## 6. 결론

- 통합 가능성: **(b) 부분 가능**. 공통 helper 는 decision reuse / status normalization / lifecycle orchestration 까지만 담당하고, Valkey Lua script 와 DB upsert semantics 는 adapter 로 보존한다.
- 본거지: **`hololive/hololive-shared/pkg/service/cache/claim` 유지**. task 09 의 결정은 본 분석 후에도 유효하다.
- 마이그레이션 우선순위: **outbox reuse cache → alarm_state DB adapter → JobRunGuard Valkey adapter → ingestion runtime singleton lease 재평가**.
- 권장 다음 step: helper 구현 task 로 진입하되, 먼저 behavior-preserving assertion tests 를 작성한다. 특히 `authorized_at` token equality, reuse hit token nil, job cooldown retry-after, singleton renew retry 를 테스트로 고정한다.
