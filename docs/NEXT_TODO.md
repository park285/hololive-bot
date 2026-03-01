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

**현재 상태**: Rust dispatcher 검증 완료 (P1-3.5), 리포지토리 반영 완료(2026-03-01): 1) rust-dispatcher 리소스 활성화, 2) GO consumer 비활성화. 운영 단계(배포/24h 모니터링/안정화 후 replica=0)는 미완료

### 절차
1. `k8s/base/kustomization.yaml`에서 `rust-dispatcher-*` 주석 해제 (완료, 2026-03-01)
2. Go `alarm-dispatcher`의 `GO_ALARM_QUEUE_CONSUMER_ENABLED=false` 설정 (완료, 2026-03-01)
3. Rust dispatcher 배포 → health/ready 확인
4. 모니터링: p95 latency < 1s, error rate < 0.1% (24시간)
5. 안정 확인 후 Go alarm-dispatcher replica=0 또는 제거

### Rollback
- Rust dispatcher replica=0 + Go `GO_ALARM_QUEUE_CONSUMER_ENABLED=true` 복원

### 파일
- `k8s/base/rust-dispatcher-deployment.yaml`
- `k8s/base/rust-dispatcher-service.yaml`
- `k8s/base/alarm-dispatcher-deployment.yaml` (Go, 현재 운영 주체)
- `hololive/hololive-rs/Dockerfile.dispatcher`

---

## 2. 교차언어 큐 계약 유지보수 (우선순위: MED)

**현재 상태**: Go/Rust 양측 fixture 테스트 통과

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
- **작업**: Responses API 에러 코드 기반 분기로 전환, Chat fallback 경로 축소
- **공수**: 중 (테스트 포함 1-2시간)

### 3-2. admin/kakao-bot-go 핸들러 중복 제거
- **위치**: `hololive-admin/internal/server/` ↔ `hololive-kakao-bot-go/internal/server/`
- **문제**: `api_stream.go`, `api_response.go`, `api_settings.go` 등에서 동일 로직 중복
- **작업**: `hololive-shared/pkg/server/` 로 공통 핸들러 추출
- **공수**: 대 (파일 10+개 변경, 테스트 필요, 3-5시간)

### 3-3. YouTube 수집 fallback 과호출 제거
- **위치**: `hololive-shared/pkg/service/youtube/scraper/videos.go`
- **문제**: 빈 결과(성공)에도 RSS fallback 추가 호출 발생
- **작업**: `len(events)==0` 분기 제거, 빈 결과를 정상 처리로 변경
- **공수**: 소 (30분)

### 3-4. Go↔Rust 이중 정책 경로 정리
- **대상**: 링크체커/날짜추출/dedup 포맷
- **현재**: Rust cutover 완료된 영역의 Go 측 코드가 잔존
- **작업**: Go 측 미사용 코드 제거 (scraper 관련 Go 코드는 이미 제거 완료, 나머지 확인)
- **공수**: 소 (확인 후 삭제만 필요, 30분)

### 3-5. shared-go conc/atomic/multierr 의존성 제거
- **현재**: `go mod tidy`로 indirect 정리 완료, direct import는 이미 제거됨
- **작업**: 확인 후 완료 마킹 (이미 이번 세션에서 tidy 완료)
- **공수**: 없음 (확인만)

---

## 4. 품질 개선 (우선순위: LOW)

### 4-1. Rust str_to_string 점진적 전환
- **현재**: 107건 잔존 (allow 상태), 신규 코드는 `to_owned()` 강제
- **작업**: 기존 코드에서 `.to_string()` → `.to_owned()` 일괄 전환
- **공수**: 소 (기계적 치환, 1시간)

### 4-2. Rust wildcard_enum_match_arm 점진적 전환
- **현재**: allow 상태
- **작업**: `_ =>` 패턴을 명시적 variant 나열로 변경
- **공수**: 소-중 (1-2시간)

### 4-3. Go 테스트 커버리지 확대
- **대상**: hololive-shared 핵심 패키지 (adapter, service/notification, service/youtube)
- **작업**: 기존 테이블 드리븐 테스트 패턴으로 커버리지 향상
- **공수**: 대 (지속적)
