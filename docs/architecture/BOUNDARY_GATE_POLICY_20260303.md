# Architecture Boundary Gate Policy

> 시행일: 2026-03-03  
> 범위: `hololive-bot` 모노레포(Go + Rust)

## 1) 목적

모듈 경계 위반(계층 역전, 계약 drift, 하드코딩 회귀)을 PR 단계에서 즉시 차단한다.

## 2) CI 고정 규칙

아래 규칙은 `.github/workflows/architecture-gates.yml`에서 **항상** 실행되어야 한다.

1. `scripts/architecture/m0-gate.sh`
   - Rust service→infra allowlist 검사
   - admin↔kakao 중복 allowlist + 총 중복 상한 검사
   - `shared-go` 경계(`shared-go -> github.com/kapu/hololive-*`) 금지 검사
   - `shared-go/pkg` 신규 패키지 allowlist 검사
   - Go 호환 어댑터(`*_compat.go`, sharedserver type alias) 금지 검사
   - Go import graph 산출
2. `scripts/architecture/m1-contract-gate.sh`
   - Go↔Rust alarm 계약 상수 parity 검사
   - Trigger 경로 하드코딩 금지 검사
3. `scripts/architecture/check-go-module-loc.sh`
   - M4 분해 대상 Go 파일 LOC 상한 검사
   - 기준 파일: `docs/architecture/go-module-loc-thresholds.txt`
4. `scripts/architecture/m6-gate.sh`
   - deprecated 제거 일정(deadline) 마커 만료 검사
   - `TODO(YYYY-MM-DD)` / `remove_after = "YYYY-MM-DD"`가 현재 UTC 날짜 이전이면 실패
   - release governance 자산(PR 템플릿, release notes 템플릿, 정책 문서) 존재 검사
   - 기준 목록: `docs/architecture/release-governance-assets.txt`

운영 표준 진입점:

```bash
./scripts/architecture/ci-boundary-gate.sh
```

## 2-1) Release 전 운영 체크리스트(필수)

Release 직전(배포 태그 생성/배포 승인 전) 아래 항목을 모두 충족해야 한다.

1. 표준 진입점 실행 및 성공
   - `./scripts/architecture/ci-boundary-gate.sh`
2. 필수 게이트 통과 확인
   - `M0`: `m0-gate.sh`
   - `M1`: `m1-contract-gate.sh`
   - `M4`: `check-go-module-loc.sh`
   - `M6`: `m6-gate.sh`
3. 증적 보관
   - 실행 로그(또는 CI 성공 링크)를 PR/릴리스 노트에 남긴다.
   - 템플릿:
     - PR: `.github/pull_request_template.md`
     - Release Note: `docs/runbook_execution/RELEASE_NOTES_TEMPLATE_20260303.md`

## 3) 브랜치 보호(필수)

`main` 브랜치 보호 규칙에서 아래 체크를 Required로 유지한다.

- `Architecture Gates / architecture-gates`

추가로 다음을 활성화한다.

1. Require branches to be up to date before merging
2. Restrict bypass permissions (관리자 최소화)

## 4) 규칙/allowlist 변경 정책

경계 규칙 변경 PR은 아래를 동시에 포함해야 한다.

1. 변경 이유(왜 allowlist/규칙 변경이 필요한지)
2. 영향 범위(어떤 모듈 경계가 바뀌는지)
3. 롤백 방법
4. 로컬 검증 로그
   - `./scripts/architecture/ci-boundary-gate.sh`

## 5) 예외 처리 원칙

1. 임시 예외는 allowlist에 최소 항목으로만 추가
2. 임시 예외 항목은 제거 일정(마일스톤/이슈)을 PR에 명시
3. 예외 제거 즉시 allowlist에서도 삭제
4. admin↔kakao 중복이 감소하면 `docs/architecture/admin-kakao-duplicate-max.txt` 상한도 즉시 하향
