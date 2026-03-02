# Codex 안티패턴 리팩토링 계획

> 최종 갱신: 2026-03-02
> 배경: Codex가 생성한 과도한 플래그화, fallback, 방어 코드, 위임 체인 등의 안티패턴 식별 및 정리

---

## 완료 항목

### C1: Rust `file_enabled` 죽은 코드 완전 제거

- **변경**: 13개 파일. `TracingInitConfig`에서 `file_enabled/dir/file/combined_file` 제거, `observability.rs` file 분기 78줄 → stdout-only, `tracing-appender` 의존성 완전 제거, TOML 2개 정리.

### C2: Go ProxyEnabled 5단계 위임 체인 해소

- **변경**: 10개 파일. pollers.go 10개 위임 메서드(80줄) 삭제, `Scheduler`에 `*scraper.Client` 직접 참조, youtube/holodex → `ScraperClient()` 접근자로 교체.

### H3: Go MajorEventScheduler 인터페이스 3중 복사 제거

- **변경**: admin/kakao-bot의 `api_majorevent.go` 로컬 인터페이스 삭제 → shared 타입 앨리어스 사용.

### H4: Go context.TODO() 핸들러 내부 사용 수정

- **변경**: webhook_handler에서 `task.ctx` 직접 사용, youtube/scheduler에서 `context.TODO()` fallback 제거.

### L1: Go ResolveAlarmAdvanceMinutes 1:1 래퍼 제거

- **변경**: private 함수 본문을 public으로 병합, private 삭제.

### L2: Go ProvideValkeyConfig 등 1줄 Provider 제거

- **변경**: `ProvideValkeyConfig`, `ProvidePostgresConfig`, `ProvideMembersData` 제거. 호출부에서 `cfg.Valkey`/`cfg.Postgres`/직접 interface 바인딩으로 치환.

### M7: Go 불가능한 ctx nil guard 삭제

- **변경**: admin `bootstrap_admin.go`, llm-sched `bootstrap_llm_scheduler.go`에서 ctx nil guard 제거.

### L3: Rust resolve_holodex_api_keys 빈 래퍼 제거

- **변경**: 래퍼 삭제, 호출부에서 `normalize_api_keys` 직접 사용.

### L5: Rust #[allow(dead_code)] on channel_id 제거

- **변경**: `ChzzkChannelInfo.channel_id` 필드 + suppress 어트리뷰트 삭제.

### L6: Rust dedup 이중 zero-guard → 에러 전파

- **변경**: `unwrap_or(zero)` → `map_err(|_| AlarmError::Config(...))` 명시적 에러 전파.

### M1: Go nil receiver guard 과용 제거

- **변경**: membernews/service.go 6개 + holodex/service.go 1개 불필요 nil guard 삭제. 생성자 계약으로 보장되는 필드만 대상.

### M5: Go CleanupEnabled 이중 플래그 → zero-value 비활성화

- **변경**: delivery/outbox 두 dispatcher에서 `CleanupEnabled bool` 삭제, `CleanupInterval > 0` 조건으로 대체.

### M2+M3: Rust TelemetryConfig 3개 → shared 통합 + resolve 중복 제거

- **변경**: shared/infra에 `TelemetryConfig` + `resolve_otel_env_overrides` SSOT 추가. alarm/scraper infra는 re-export, app은 shared 호출로 위임.

### H1: Rust 이중 ValkeyClient 트레이트 통합

- **변경**: alarm의 로컬 ValkeyClient 트레이트 556줄 → 7줄 re-export. `From<SharedError> for AlarmError` 브릿지 추가. 테스트 Mock 통합.

### H2: Rust twitch_enabled 4단계 전파 → Option 패턴

- **변경**: `twitch_enabled: bool` 4단계 전파 제거. `twitch_poll_interval: Option<Duration>` + `twitch_checker: Option<Arc<TwitchChecker>>`로 변환. `None`이면 루프 스폰 자체 생략.

### M6: Go AlarmQueueConsumerEnabled cutover 완료 후 제거

- **변경**: Go alarm-dispatcher 바이너리 전체 제거 (`hololive-alarm/`), `AlarmQueueConsumer`/`AlarmQueueDispatcher` 코드 4파일 삭제, `NotificationConfig.AlarmQueueConsumerEnabled` 필드 + `config/alarm_dispatcher.go` 삭제, `ProvideAlarmQueueDispatcher` Provider 삭제, `BotRuntime.AlarmQueueDispatcher` 필드 + 시작/조립 코드 제거, docker-compose/k8s에서 `GO_ALARM_QUEUE_CONSUMER_ENABLED` env 제거.

---

## 대기 항목 (외부 의존)

### M4: Rust read_notified_data JSON 폴백 만료 시점 지정

- **위치**: `alarm/service/src/dedup/mod.rs:319-340`
- **조건**: Valkey 데이터 마이그레이션 확인 후 제거 일정 확정 필요
- **작업**: JSON 폴백 경로에 deprecated 경고 로그 추가 → 실제 잔존 데이터 확인 → 제거

### L4: Rust Formatter 8트레이트 단일 구현체 → 트레이트 제거

- **위치**: `shared/services/formatter/` (8개 트레이트, 1개 구현체)
- **조건**: 테스트 mock이 트레이트에 의존하는지 확인 필요. mock 존재 시 트레이트 유지.
