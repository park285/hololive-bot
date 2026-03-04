# Codex 안티패턴 리팩토링 계획

> 최종 갱신: 2026-03-04
> 배경: Codex가 생성한 과도한 플래그화, fallback, 방어 코드, 위임 체인 등의 안티패턴 식별 및 정리

---

## 완료 항목 (17개)

| ID | 언어 | 요약 |
|----|------|------|
| C1 | Rust | `file_enabled` 죽은 코드 완전 제거 (13파일, observability.rs stdout-only) |
| C2 | Go | ProxyEnabled 5단계 위임 체인 해소 (Scheduler에 `*scraper.Client` 직접 참조) |
| H1 | Rust | ValkeyClient 이중 트레이트 통합 (556줄 → 7줄 re-export) |
| H2 | Rust | twitch_enabled 4단계 전파 → `Option<Duration>` + `Option<Arc<TwitchChecker>>` |
| H3 | Go | MajorEventScheduler 인터페이스 3중 복사 제거 → shared 타입 앨리어스 |
| H4 | Go | context.TODO() 핸들러 내부 사용 → `task.ctx` 직접 사용 |
| M1 | Go | nil receiver guard 과용 제거 (7개 삭제) |
| M2+M3 | Rust | TelemetryConfig 3개 → shared SSOT + resolve 중복 제거 |
| M5 | Go | CleanupEnabled 이중 플래그 → `CleanupInterval > 0` 조건 |
| M6 | Go | AlarmQueueConsumerEnabled + alarm-dispatcher 바이너리 완전 제거 |
| M7 | Go | 불가능한 ctx nil guard 삭제 |
| L1 | Go | ResolveAlarmAdvanceMinutes 1:1 래퍼 제거 |
| L2 | Go | ProvideValkeyConfig 등 1줄 Provider 제거 |
| L3 | Rust | resolve_holodex_api_keys 빈 래퍼 제거 |
| L5 | Rust | `#[allow(dead_code)]` on channel_id 제거 |
| L6 | Rust | dedup 이중 zero-guard → 명시적 에러 전파 |

---

## 대기 항목 (2개, 외부 의존)

### M4: Rust read_notified_data JSON 폴백 만료 시점 지정

- **위치**: `alarm/service/src/dedup/mod.rs:319-340`
- **조건**: Valkey 데이터 마이그레이션 확인 후 제거 일정 확정 필요
- **작업**: JSON 폴백 경로에 deprecated 경고 로그 추가 → 실제 잔존 데이터 확인 → 제거

### L4: Rust Formatter 8트레이트 단일 구현체 → 트레이트 제거

- **위치**: `shared/services/formatter/` (8개 트레이트, 1개 구현체)
- **조건**: 테스트 mock이 트레이트에 의존하는지 확인 필요. mock 존재 시 트레이트 유지.
