# hololive-bot 리팩토링 계획 (2026-06-02)

> cross-cutting 마스터: `iris-stack/docs/REFACTORING_PLAN_20260602.md`
> 범위: `hololive-shared`(domain/contracts/service incl. youtube) + 런타임 5종(kakao-bot, admin-api, alarm-worker, llm-sched, youtube-producer) (~128K LOC)
> 관련 계약 문서: `CONTRACT_MAP.md`, `QUEUE_AND_PUBSUB_CONTRACTS.md`, `ERROR_CONTRACT.md`

## 0. 요약

런타임은 대체로 `hololive-shared` 위의 thin adapter이고, 계약 타입은 `pkg/contracts/*`로 상당 부분 SSOT화돼 있으며, youtube outbox는 4단계 idempotency로 견고합니다. 결함은 **(1) 계약/상수 SSOT 분열, (2) alarm 전달 경로 손실, (3) 레이어 경계 누수, (4) 공통 primitive 중복**에 집중됩니다. cbgk와 달리 이 monorepo는 내부 결합이 크므로 계약 변경은 조율 배포가 필요합니다.

## 1. 검증으로 폐기·정정된 1차 주장

| 주장 | 결과 | 근거 |
|---|---|---|
| alarm-worker `new(alarmDispatchClientRequestID(...))` 컴파일 불가 | **폐기** | `go build ./internal/app/workerapp/` exit 0. `new` shadow helper. |
| youtube `contextMutex` over-unlock panic 도달 | **폐기** | buffer=1 채널 mutex로 동시 Lock 불가. `LockContext` 실패 시 Unlock 미호출. |
| `observation_compare*.go` 14파일 = 중복발송 방어 | **정정** | 사후 감사/리포트 전용. 실제 방어는 `TryClaimAlarmState` CAS. |
| domain GORM struct tag → 전이 GORM 의존 | **정정** | tag는 `gorm.io/gorm` import 불요. 단 `database/sql`·`driver` 직접 import 실재(P2). |
| youtube-producer photo-sync goroutine leak | **축소** | race window 작음(inner.Start ctx 즉시 존중 시 0). `<-done` 누락은 실재. |
| youtube-producer `startActiveActiveRecoveryLoop` stop func 폐기 위험 | **폐기** | loopCtx가 부모 ctx 파생 → 부모 cancel로 종료. 의도적. |
| youtube-producer HasTable 5초마다 | **정정** | 1분 throttle(`shouldRefreshTieredTargets`). P3. |
| admin-api APIKey 빈키 우회 exploitable | **폐기(비취약)** | `validateAPIRouterInputs`(router.go:86)가 startup 차단. |
| admin-api OAuth callback reflected XSS | **폐기(안전)** | `oauth_proxy.go:26` `html/template`(contextual escaping). |
| `dispatchoutbox.ReleaseClaimKeys` no-op → 메시지 drop (P0) | **하향 P1** | PG row lifecycle는 PG 컬럼이 추적. claim key만 TTL까지 잔존(재시도 TTL 차단). |

## 2. P0 — 즉시

### P0-A. alarm-worker: PG 모드 Karing 502 → `sending` 고착 → quarantine → 알람 silent LOSS 〔검증〕
- **증거**: `internal/app/workerapp/alarm_dispatch_runner.go:144-170` + `pkg/service/alarm/dispatchoutbox/repository_transitions.go:57-93`.
- **메커니즘**: `MarkSending`(→sending) 후 502 → retryable 분류 → `persistPreSendFailure`→`ScheduleRetry`. `ScheduleRetry`의 `WHERE status='leased'`가 `sending` 불일치 → 0 rows → 에러 → row `sending` 고착 → `QuarantineStaleSending`(terminal). **재시도 분류됐으나 실제 손실.** (double-send은 quarantine이 차단.)
- **수정**: `sending` 전용 retry 경로(`WHERE status IN ('leased','sending')`) 또는 retryable 판별을 `MarkSending` 이전으로.
- **연계**: 분류가 **에러 문자열 매칭**(`alarm_dispatch_runner.go:163` `strings.Contains("/karing/content-list returned 502")`)이라 래핑 변경 시 깨짐 → `errors.As(*HTTPError)` + status code 교체.
- **Risk/Effort**: 높음/Small-Medium.

### P0-B. 알람 계약 SSOT 분열 〔검증〕
- **`AlarmQueueEnvelope` 이중정의**: `pkg/contracts/alarm/contracts.go:75`(5필드, 테스트 전용) vs `pkg/domain/alarm.go:212`(9필드, 런타임 사용 + custom JSON). 계약 테스트가 wire 절반만 검증. version-0 레거시는 `domain.UnmarshalJSON`이 처리(검증) → contracts 버전 **삭제 가능**.
- **`notified:claim:` 4중복**(+`notified:claim:event:` 4중복): `contracts/alarm/contracts.go:57-58`, `service/alarm/keys/keys.go:59-60`, `notification/internal/alarmservice/alarm_types.go:59-60`, `notification/internal/alarmcache/state.go:19-20`. 불일치 시 dedup claim 어긋남 → 이중 발송.
- **수정**: `domain.AlarmQueueEnvelope` SSOT 승격 + contracts 버전 삭제 + 계약 테스트를 9필드로 재작성. claim prefix는 `keys.go` SSOT로 통합(나머지 3곳 교체).

### P0-C. dispatchoutbox `expectRowsAffected` partial-batch 오류 〔검증〕
- **증거**: `pkg/service/alarm/dispatchoutbox/repository_transitions.go:32,54,92,122`.
- **문제**: 동시 worker가 배치 일부를 이미 전이/만료시킨 정상 케이스에서 `RowsAffected<len(ids)` → 에러. P0-A 손실 경로와 결합.
- **수정**: `MarkSent`/`MarkSending`은 affected count warn 로그만, 에러 미반환(quarantine/recover가 처리).

### P0-D. youtube `batchrepo → outbox` 역방향 레이어 의존 〔검증〕
- **증거**: `pkg/service/youtube/poller/internal/batchrepo/repository_batch.go:31,171` — poller가 `outbox.NewDeliveryTelemetryRepository` 직접 호출.
- **수정**: `PostLatencyClassificationPersister` 인터페이스 주입(DI), post-commit hook 위임.

## 3. P1 — 높은 우선순위

### hololive-shared/service
- **circuit breaker 3중 구현** — chzzk(RWMutex+`*time.Time`+int) / twitch(atomic) / holodex(RWMutex). `shared-go/pkg/circuitbreaker` 또는 `internal/circuit`로 단일화. (chzzk·holodex `failureCount`는 circuit mutex 공유로 contention.)
- **`dispatchoutbox.Consumer.ReleaseClaimKeys` no-op**(`consumer.go:243`) → claim key가 TTL(`NotificationSent`)까지 잔존, DLQ 후 동일 알림 재시도 TTL 동안 차단. `ClaimKeyReleaser` narrow interface + `cache.DelMany` 구현.
- **`NotifiedData`/`UpcomingEventNotifiedData` 이중정의**(`dedup/service.go:33` vs `alarmservice/alarm_types.go:75`, JSON tag 동일) → schema drift. canonical 1곳 + import.
- **`targetMinutesMu` 3중복**(`alarm/client.go:47`, `dedup/service.go:51`, `alarmservice`) → `atomic.Pointer[TargetMinutePolicy]`.
- **`valkeyNotificationLocker.TryAcquire`가 Valkey 오류 시 `acquired=true`**(`delivery/locker.go:68`) — 의도적 graceful degradation이나 장애 시 중복 발송. 메트릭 추가.
- **`alarm.Client.WarmCacheFromDB` no-op**(`client.go:289`)이 `domain.AlarmCRUD` 만족용 — 인터페이스 분리로 해소.
- **`cache.Client` god interface** → 소비자별 narrow(`ClaimKeyCache`/`KVCache`/`HashCache`/`SetCache`). 메서드 맵: ratelimit→`LowLevelCache`, delivery/locker→3메서드, auth→KV+Set, dedup→Hash+Set.

### hololive-shared/youtube
- **`Dispatcher`의 `ProcessOnceForTest`/`CleanupForTest`/`AggregateSyncForTest` + hook 필드가 production binary에 export**(`outbox/internal/delivery/dispatcher.go:94,317`) → `_test.go` wrapper 또는 build-tag 격리.
- **전역 `slog.*`(logger 미주입)** — `poller/internal/scheduler_worker.go`(10+곳), `pollers/live_poller.go:270,348`, `batchrepo/repository_batch.go:172`. `Scheduler`/`GormBatchRepository`에 logger 필드 주입.
- **poller proxy 보일러플레이트 5중복**(SetProxyEnabled/ProxyEnabled/Name) → `pollerBase` embed.

### hololive-shared/core
- **domain persistence 누수**: `database/sql`·`database/sql/driver` 직접 import(`notification_delivery.go:24`, `alarm.go:24`) + GORM tag 182 + TableName 21. `AlarmTypes.Value()/Scan()`(`alarm.go:78`)도 동일. → `persistence/` 분리(장기, Wave 5). **상세: `GORM_REMOVAL_PLAN_20260602.md`** (domain 순수화 Phase 1 + 전체 GORM→pgx 이식 Phase 2~4).
- **`AddAlarmRequest.Ctx context.Context` 필드**(`alarm_interfaces.go:29`) — 사용처 0(dead) + anti-pattern. 제거.
- **`ConfigUpdateV1` 이중정의**(`contracts/settings/contracts.go:44` vs `configsub/subscriber.go:36`) — 런타임은 `configsub.ConfigUpdate` 사용. contracts 타입으로 통합.
- **admin-api `"membernews_weekly_run_now"` 하드코딩**(`settings_handler.go:324,328`) → `contractssettings.UpdateTypeMemberNewsRunNow` 상수.

### 런타임
- **admin-api(security)**: `/api/auth/*`(login·reset-request)에 IP allowlist 없음(`registration.go:67`). service-layer rate-limit(login 30/min, reset 10/min per-IP) 존재하나 IP 로테이션 회피 가능. 인터넷 비노출이면 수용, 아니면 `pkg/server/middleware/ip_allowlist.go` 적용. reset-request 타이밍 오라클(존재 시 DB 2회 추가) + `auth.Service.Refresh` 세션 회전 window(P1-q, holo-svc).
- **dead code**: `SubscriberGraphCommand`(미등록, `handler_subscriber_graph.go:44`) 삭제; `Registry.aliasKeys`(미사용, `registry.go:38`) 제거.
- **llm-sched**: client-side LLM rate-limit/cost ceiling 부재(consensus 최대 4 serial, `openai_client.go:108`). `membernews_weekly_run_now` Pub/Sub(비내구) → HTTP trigger API 전환(`QUEUE_AND_PUBSUB_CONTRACTS.md`도 권고).

## 4. P2/P3 (요약)

- [P2] youtube **VIDEO/LIVE는 `ON CONFLICT ... WHERE kind IN ('COMMUNITY_POST','NEW_SHORT')` 재활성화 제외**(`batchrepo/repository_batch_writes.go:255`) → FAILED 후 재poll 시 silent no-op → 알람 영구 손실 가능. `isCommunityShortsOutboxKind`가 rearm 경로도 VIDEO/LIVE 제외. **의도면 문서화, 아니면 WHERE 확장.**
- [P2] youtube-producer `photo_sync_guard.go:80` — `cancel()` 후 `<-done` 대기 없이 `Release()` → inner.Start 종료 전 lease 해제 가능(작은 중복 sync window). `<-done` 추가.
- [P2] youtube-producer `readinessReportingJobClaimer.TryClaim`가 `JobClaimUnavailable`에 `fmt.Errorf("job lease unavailable")`로 원인 소실(`readiness_job_claimer.go:50`). result를 `%w`/attr로. + 일시 Valkey 오류가 전체 readiness 503 → LB churn 가능.
- [P2] youtube-producer `ingestionlease`가 `JobClaimResult/Status/Claim/Claimer` 중복정의 + `mapJobClaimResult` 브리지(`job_run_guard.go:23`) → `poller.JobClaimer` 직접 구현으로 제거.
- [P2] `tryBuildTieredChannelPollerRegistrations`/`SyncAt`가 `context.Background()` 사용(`youtube_producer_components.go:232`, `youtube_poll_target_scheduler.go:48`) → caller ctx 전파.
- [P2] `cache.Service.MGet`이 nil/빈값 구분 불가(`service_kv.go:69`) — `Get`/`GetString`과 불일치.
- [P2] `alarm/cache_warm.go` package-level var hooks(`:21`) → Repository interface 주입(parallel test race).
- [P2] auth `cacheClient nil` 분기 5+곳; bcrypt `DefaultCost` 하드코딩(`auth/service.go:100`) → config화(권장 cost 12).
- [P2] bot: admin Handler god-struct 22필드 + `DomainHandlers struct{*Handler}`(`api.go:55`) → per-domain dep struct(Wave 5). `alarmActionHandlers()` per-call map alloc(`handler_alarm.go:83`) → switch. `SettingsAPIHandler.settingsHandler()` per-request 재할당(`api_settings.go:28`) → 필드. ingress 로깅 `context.Background()`(`bot_ingress.go:187`) → 요청 ctx 전파.
- [P2] `dbx.InTx`/`InTxWithResult` 미사용(9곳이 `gorm.DB.Transaction` 직접) — wrapper 통일 또는 제거(panic→rollback은 GORM이 보장).
- [P2] `contracts/delivery` prod 소비자 0(alias 통과 계층) — 제거/통합.
- [P3] KST 15곳(이 repo: llm-sched 4 + youtube 1 + alarm-worker 1 + `pkg/util/time.go` 1) → `pkg/util/time.go` 단일 참조.
- [P3] `dispatcher.go:253` `processAvailable(ctx, 4)` magic number → Config. holodex `getNextAPIKey` 단일키(`KeyRotationError` dead). wakeup metric label 하드코딩 `"pg"`(`alarm_dispatch_idle.go:66`). `template.Renderer` in-memory cache TTL 없음. `apperrors.NewAPIError`/`NewValidationError` 시그니처-동작 간극(`value any` 무시). bot 하드코딩 한국어 backpressure 문자열(`bot_message_async.go:32`) → adapter.

## 5. 계약 blast-radius matrix (요약)

| 등급 | 항목 |
|---|---|
| HIGH | Valkey `DispatchQueueKey`/`Retry`/`DLQ`(in-flight), `NotifyClaimKeyPrefix`(4중복), `PubSubChannelV1`(4서비스), `contracts/trigger`(3서비스 5파일) |
| MEDIUM | HTTP route 상수(client+server 동시), `membernews`/`majorevent`/`subscription` DTO, `common.APIKeyHeader` |
| LOW/삭제가능 | `contracts/alarm.AlarmQueueEnvelope`(테스트만), `contracts/delivery`(소비자 0), `contracts/settings.ConfigUpdateV1`(런타임 미사용) |

**경계**: cbgk는 hololive-shared 미import → 위 변경은 hololive-bot monorepo 내부 한정.

## 6. delivery idempotency (검증된 모델)
- **alarm Valkey 모드(prod 기본 `valkey_only`)**: `DrainBatch`(BRPOP) 즉시 제거 + Mark* no-op → at-most-once, 크래시 시 silent loss(미문서화 속성).
- **alarm PG 모드(opt-in `pg_first`)**: lease claim + `RecoverExpiredLeased`/`QuarantineStaleSending`. P0-A 경로에서 손실, double-send은 quarantine이 차단.
- **ClientRequestID**: 평문 `SendMessage`도 `IrisMessageSender`가 인터페이스 구현 → 항상 전달. (서버측 dedup 시간창은 범위 밖.)
- **youtube 4단계**: production INSERT `ON CONFLICT (kind,content_id)` → delivery `(outbox_id,room_id)` → `FOR UPDATE SKIP LOCKED` → community/shorts `TryClaimAlarmState` CAS. 견고. split-brain은 Valkey 정상 시 per-job Lua lease로 차단, 장애 시 PG upsert가 최종 방어.

## 7. 미해결(추적)
- Iris 서버측 `ClientRequestID` dedup 시간창.
- `NotificationSentTTL` 값(ReleaseClaimKeys no-op 영향 기간).
- youtube VIDEO/LIVE 재활성화 제외가 의도인지.
- domain GORM entity 분리 후 AutoMigrate 소유권(현재 prod AutoMigrate 호출 0, 테스트만).

## 8. Deep-read (opus 2차)
youtube outbox/delivery/claim 코어(dispatcher/claim_acquire/claim_gate/send/repository_lock/sent_at), `observation_compare*`(감사 전용 확정), `published_at_resolver`(nil claimer 안전), alarm dispatchoutbox 상태머신 전이, auth 세션/리셋 경로, dbx tx/DSN(SQL injection 없음, double-close 안전), contracts 전 패키지 + 소비자 목록 정독.
