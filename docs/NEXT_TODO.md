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

**현재 상태**: Rust dispatcher 검증 + 배포 정상화 완료 (2026-03-02): 1) rust-dispatcher 리소스 활성화, 2) GO consumer 비활성화, 3) rust-dispatcher `/health`·`/ready` 정상 확인. 24h 모니터링 및 최종 정리 단계는 미완료

### 절차
1. `k8s/base/kustomization.yaml`에서 `rust-dispatcher-*` 주석 해제 (완료, 2026-03-01)
2. Go `alarm-dispatcher`의 `GO_ALARM_QUEUE_CONSUMER_ENABLED=false` 설정 (완료, 2026-03-01)
3. Rust dispatcher 배포 → health/ready 확인 (완료, 2026-03-02)
4. 모니터링: p95 latency < 1s, error rate < 0.1% (24시간) (진행 필요)
5. 안정 확인 후 Go alarm-dispatcher replica=0 또는 제거 (보류: admin/bot의 alarm-dispatcher HTTP 의존 정리 선행 필요)

### Rollback
- Rust dispatcher replica=0 + Go `GO_ALARM_QUEUE_CONSUMER_ENABLED=true` 복원

### 파일
- `k8s/base/rust-dispatcher-deployment.yaml`
- `k8s/base/rust-dispatcher-service.yaml`
- `k8s/base/alarm-dispatcher-deployment.yaml` (Go, 현재 운영 주체)
- `hololive/hololive-rs/Dockerfile.dispatcher`

---

## 2. 교차언어 큐 계약 유지보수 (우선순위: MED)

**현재 상태**: Go/Rust 양측 계약 테스트 통과 (2026-03-02)

### 향후 필요 시점
- `AlarmQueueEnvelope` 스키마 변경 시
- envelope version 2 도입 시

### 작업
- Rust fixture (`hololive-rs/testdata/alarm_queue/`) 수정
- Go 계약 테스트 (`hololive-shared/pkg/service/notification/alarm_queue_consumer_test.go`) 동기화
- `supportedVersions` map 갱신 (Go: `alarm_queue_consumer.go`, Rust: `queue.rs`)

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

## 4. 품질 개선 (우선순위: LOW)

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
