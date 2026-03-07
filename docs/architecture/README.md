# Architecture Gate 운영 가이드

> 작성일: 2026-03-02  
> 마지막 업데이트: 2026-03-03

모듈 경계 회귀를 조기에 차단하기 위한 아키텍처 게이트 운영 문서다.

## 0) 정책 문서

- 경계 게이트 운영 정책(필수): `docs/architecture/BOUNDARY_GATE_POLICY_20260303.md`
- Go↔Rust 계약 의사결정: `docs/architecture/GO_RUST_CONTRACT_SSOT_DECISIONS_20260302.md`
- deprecated deadline 게이트 장애 대응: `docs/architecture/DEPRECATED_DEADLINE_RUNBOOK_20260303.md`
- deprecated 마커 인벤토리: `docs/architecture/DEPRECATED_MARKER_INVENTORY_20260303.md`

## 1) M0 게이트

### 1-1. Go import graph 산출

```bash
./scripts/architecture/export-go-workspace-import-graph.sh
```

- 산출물: `artifacts/architecture/go-workspace-import-graph.txt`

### 1-2. admin↔kakao 중복 게이트

```bash
./scripts/architecture/check-admin-kakao-duplicates.sh
```

- allowlist: `docs/architecture/admin-kakao-duplicate-allowlist.txt`
- 상한값: `docs/architecture/admin-kakao-duplicate-max.txt`
- 정책:
  - 신규 중복 발생 시 실패
  - 총 중복 파일 수가 상한값을 초과하면 실패(감소 목표 고정)

### 1-3. Rust service→infra 게이트

```bash
./scripts/architecture/check-rust-service-infra.sh
```

- allowlist: `docs/architecture/rust-service-infra-allowlist.txt`
- 정책: 신규 direct dependency 발생 시 실패

### 1-4. shared-go 경계 게이트

```bash
./scripts/architecture/check-shared-go-boundary.sh
```

- 정책: `shared-go`에서 `github.com/kapu/hololive-*` import 금지

### 1-5. shared-go 패키지 allowlist 게이트 (M2 준비)

```bash
./scripts/architecture/check-shared-go-packages.sh
```

- allowlist: `docs/architecture/shared-go-package-allowlist.txt`
- 정책: `shared-go/pkg`에 신규 패키지 추가 시 allowlist 검토 없이 병합 금지

### 1-6. Go 호환 어댑터 금지 게이트 (M2 진행)

```bash
./scripts/architecture/check-go-compat-adapters.sh
```

- 금지 파일:
  - `hololive-admin/internal/server/shared_compat.go`
  - `hololive-admin/internal/server/api_trigger_compat.go`
  - `hololive-kakao-bot-go/internal/server/shared_compat.go`
  - `hololive-kakao-bot-go/internal/server/api_trigger_compat.go`
- 정책: 호환 어댑터/타입 alias 기반 우회 경로 금지

### 1-7. M0 일괄 실행

```bash
./scripts/architecture/m0-gate.sh
```

## 2) M1 계약 게이트

```bash
./scripts/architecture/m1-contract-gate.sh
```

- Go↔Rust 계약 상수 parity 검사
  - Queue key
  - Claim key prefix
  - Logical claim key prefix
  - Queue envelope version(v1)
- Trigger 경로 하드코딩(`"/internal/trigger/*"`) 금지

## 3) M4 Go 모듈 LOC 게이트

```bash
./scripts/architecture/check-go-module-loc.sh
```

- 기준 파일: `docs/architecture/go-module-loc-thresholds.txt`
- 정책:
  - 파일별 LOC 상한 초과 시 실패
  - 대상 파일 삭제/이동 시 threshold 파일도 즉시 동기화

## 4) M6 deprecated 제거 일정(deadline) 게이트

```bash
./scripts/architecture/m6-gate.sh
```

- 개별 체크:
  - `./scripts/architecture/check-rust-deprecated-deadline.sh`
  - `./scripts/architecture/check-release-governance-assets.sh`
- 기준 파일:
  - `docs/architecture/release-governance-assets.txt`
- 정책:
  - `TODO(YYYY-MM-DD)` / `remove_after = "YYYY-MM-DD"` 마커가 오늘(`UTC`) 이전이면 실패
  - 만료되지 않은 마커는 pending으로 허용
  - PR/Release 체크리스트 자산(PR 템플릿, release notes 템플릿, 정책 문서) 누락 시 실패

## 5) CI/Release 표준 진입점

```bash
./scripts/architecture/ci-boundary-gate.sh
```

CI workflow(`.github/workflows/architecture-gates.yml`)는 위 진입점을 사용해 M0/M1/M4/M6를 고정 실행한다.

### 5-1) Release 전 체크리스트(필수)

- 배포 전 `./scripts/architecture/ci-boundary-gate.sh` 성공
- 필수 게이트: M0 / M1 / M4 / M6
- 실행 로그 또는 CI 성공 링크를 PR/릴리스 노트에 기록
- 릴리스 노트 템플릿: `docs/runbook_execution/RELEASE_NOTES_TEMPLATE_20260303.md`
- 릴리스 노트 자동 생성(선택):
  - `./scripts/architecture/render-release-notes.sh --version <tag> --pr-link <url> --ci-evidence-link <url> --ci-artifact-url <url> --output <file>`
- 배포 체크리스트: `docs/runbook_execution/DOCKER_COMPOSE_DEPLOYMENT_GUIDE.md`

## 6) CI 연동

- workflow: `.github/workflows/architecture-gates.yml`
- PR 템플릿: `.github/pull_request_template.md`
- 릴리스 노트 템플릿: `docs/runbook_execution/RELEASE_NOTES_TEMPLATE_20260303.md`
- 수행 단계:
  1. `ci-boundary-gate.sh` (M0 + M1 + M4 + M6)
  2. `go-workspace-import-graph.txt` artifact 업로드

## 7) 운영 원칙

1. Incremental Strangler 방식 유지 (빅뱅 금지)
2. allowlist는 의도된 예외만 최소 범위로 유지
3. allowlist 항목 제거 시 즉시 파일에서도 삭제
4. 경계 규칙 변경은 정책 문서(섹션 0) 절차를 따른다
