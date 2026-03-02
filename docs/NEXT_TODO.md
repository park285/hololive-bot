# 향후 작업 TODO

> 최종 갱신: 2026-03-02
> 아키텍처: 하이브리드 (Rust=compute, Go=network)

---

## 0. 로깅 SSOT: stdout → Fluent Bit → Loki (완료, 2026-03-02)

**상태**: 매니페스트/스크립트/문서 변경 완료, 배포 검증 미완료

### 완료 항목
- k8s 매니페스트 8개: hostPath 파일 로깅 제거 (Go 5개 + Rust 3개)
- logcli 래퍼 스크립트: `scripts/logs/tail.sh`, `scripts/logs/query.sh`
- 문서 동기화: README.md, runbook, LOGGING_POLICY.md
- 로컬 로그 파일 삭제: `logs/alarm-dispatcher.log`, `logs/combined.log`

### 배포 후 검증 (수동)
1. `kubectl kustomize k8s/overlays/prod --enable-helm | kubectl apply --server-side -f -`
2. pod 재시작 확인: `kubectl -n hololive rollout status deploy/<name>`
3. Grafana(`localhost:30090`)에서 각 서비스 로그 유입 확인
4. `./scripts/logs/tail.sh bot` 실행 테스트
5. 호스트 `/home/kapu/gemini/hololive-bot/logs/`에 새 파일 생성 안 됨 확인

---

## 1. Rust Dispatcher Cutover (우선순위: HIGH)

**상태**: 완료 (2026-03-02). Go alarm-dispatcher 바이너리 + AlarmQueueDispatcher 완전 제거 완료.

### 완료 항목
1. `k8s/base/kustomization.yaml`에서 `rust-dispatcher-*` 주석 해제 (2026-03-01)
2. Go `alarm-dispatcher` 비활성화 → 바이너리 전체 제거 (2026-03-02)
3. Rust dispatcher 배포 → health/ready 확인 (2026-03-02)
4. Go 큐 소비자 코드 제거: `AlarmQueueConsumer`, `AlarmQueueDispatcher`, config 플래그, Provider 함수 (2026-03-02)
5. 인프라 설정 정리: docker-compose, k8s에서 `GO_ALARM_QUEUE_CONSUMER_ENABLED` 제거 (2026-03-02)

### Rollback
- Rust dispatcher는 독립 운영. Go 소비자 경로는 완전 제거됨 (복원 불가, git revert 필요)

### 파일
- `k8s/base/rust-dispatcher-deployment.yaml`
- `k8s/base/rust-dispatcher-service.yaml`
- `hololive/hololive-rs/Dockerfile.dispatcher`

---

## 2. 교차언어 큐 계약 유지보수 (우선순위: MED)

**현재 상태**: Go/Rust 양측 계약 테스트 통과 (2026-03-02)

### 향후 필요 시점
- `AlarmQueueEnvelope` 스키마 변경 시
- envelope version 2 도입 시

### 작업
- Rust fixture (`hololive-rs/testdata/alarm_queue/`) 수정
- Go 계약 테스트 (`hololive-shared/pkg/domain/alarm_test.go`) 동기화
- Rust `queue.rs` 버전 맵 갱신

---

## 3. 레거시 코드 정리 (우선순위: MED)

### 3-1. OpenAI Responses→Chat fallback 단순화
- **위치**: `hololive-shared/pkg/llm/openai_client.go`
- **문제**: 문자열 힌트 기반 fallback 분기로 과대 fallback 가능
- **상태**: 완료 (2026-03-02)
- **작업 결과**: Responses API 에러 코드/HTTP status 기반 분기로 전환, Chat fallback 경로 축소, 테스트 케이스 보강

### 3-2. admin/kakao-bot-go 핸들러 중복 제거
- **위치**: `hololive-admin/internal/server/` ↔ `hololive-kakao-bot-go/internal/server/`
- **문제**: `api_stream.go`, `api_response.go`, `api_settings.go` 등에서 동일 로직 중복
- **상태**: 1차 완료 (2026-03-02)
- **작업 결과**: `hololive-shared/pkg/server/`에 `settings_handler.go`, `stream_handler.go` 공통 로직 추출, admin/kakao는 thin wrapper로 전환
- **잔여**: `api_response.go` 포함 나머지 핸들러 군 공통화는 후속 분리 작업으로 진행

### 3-3. YouTube 수집 fallback 과호출 제거 (완료)
- **위치**: `hololive-shared/pkg/service/youtube/scraper/videos.go`
- **상태**: 이미 수정 완료 — HTML 성공 시 빈 결과도 즉시 반환 (L248-249), RSS 미호출
- **검증**: `TestGetRecentVideos_NoRSSFallbackOnEmptySuccess` 통과

### 3-4. Go↔Rust 이중 정책 경로 정리 (완료)
- **상태**: 완료 (2026-03-02)
- **삭제**: scraper, service, link_checker, date_extractor, rss_parser + 테스트/testdata/dev tool (~5,200줄)
- **정리**: MajorEventConfig dead fields, Provider 2개, config struct/test, `golang.org/x/text` direct→indirect
- **이동**: `kst`, `GetWeekRange()` → scheduler.go

### 3-5. shared-go conc/atomic/multierr 의존성 제거 (완료)
- **상태**: 완료 — `go mod tidy`로 indirect 정리 완료, direct import는 이미 제거됨

---

## 4. Codex 안티패턴 리팩토링 (우선순위: MED)

**상태**: 17/19 완료 (2026-03-02), 잔여 2개(대기 항목)
**상세 문서**: `docs/CODEX_ANTIPATTERN_REFACTORING.md`

### 완료 (17개)
- C1: Rust `file_enabled` 죽은 코드 완전 제거
- C2: Go ProxyEnabled 5단계 위임 체인 해소
- H1: Rust ValkeyClient 이중 트레이트 통합 (556줄 → 7줄 re-export)
- H2: Rust twitch_enabled 4단계 전파 → Option 패턴
- H3: Go MajorEvent 인터페이스 3중 복사 제거
- H4: Go context.TODO() 핸들러 내부 사용 수정
- M1: Go nil receiver guard 과용 제거
- M2+M3: Rust TelemetryConfig 3개 → shared 통합
- M5: Go CleanupEnabled → zero-value 비활성화
- M6: Go AlarmQueueConsumerEnabled + alarm-dispatcher 바이너리 완전 제거
- M7: Go 불가능 nil guard 삭제
- L1/L2/L3/L5/L6: Go/Rust 래퍼 제거 + dead code 정리

### 대기 (2개, 외부 의존)
- M4: JSON 폴백 제거 (Valkey 데이터 마이그레이션 확인 후)
- L4: Formatter 트레이트 제거 (mock 의존 확인 후)

---

## 5. 품질 개선 (우선순위: LOW)

### 4-1. Rust str_to_string 점진적 전환
- **현재**: 대량 치환 완료, 잔여 약 56건(2026-03-02 기준, 일부는 포맷/에러 문자열 변환 목적)
- **작업**: 기존 코드에서 `.to_string()` → `.to_owned()` 일괄 전환
- **공수**: 소 (기계적 치환, 1시간)

### 4-2. Rust wildcard_enum_match_arm 점진적 전환
- **현재**: 대량 전환 완료, `_ =>` 패턴 약 21건 잔여(2026-03-02 기준, enum 외 일반 match 포함)
- **작업**: `_ =>` 패턴을 명시적 variant 나열로 변경
- **공수**: 소-중 (1-2시간)

### 4-3. Go 테스트 커버리지 확대
- **대상**: hololive-shared 핵심 패키지 (adapter, service/notification, service/youtube)
- **상태**: 진행 중 (2026-03-02)
- **작업 결과**: helper/dispatcher 경로 중심 신규 테스트 5개 추가
- **공수**: 대 (지속적)
