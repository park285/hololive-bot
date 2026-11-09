# CI Gates

## 검사 범위

Architecture gate는 SQL 안전 정책, migration 적용 목록과 보안·배포 설정을 검사합니다.
문서 표현·경로, 패키지 이름과 퇴역 이름의 재등장은 별도 게이트로 검사하지 않습니다.
stack AGENTS.md의 기준에 따라 checker 자체 테스트, 문서 동기화와 제거 코드 문자열 검사는 두지 않습니다.

## 실행 순서

`scripts/architecture/ci-boundary-gate.sh`는 비밀·로컬 산출물 추적,
migration manifest, SQL 안전 정책, native 실행 권한 제한과 topology 설정 일치를
검사한 뒤 환경 파일·health gate·서비스 선택·원격 로그 동기화의 동작 테스트를 실행합니다.

Go 파일·함수 크기는 golangci-lint의 `revive` `file-length-limit`(비테스트 파일 800줄)과
`funlen`(함수 120줄)이 검사합니다. 복잡도는 설정된 린터가 맡으며 별도 구조 예산 검사는 없습니다.
staticcheck도 golangci-lint에 포함된 판본으로 한 번만 실행하며 별도 staticcheck 실행은 두지 않습니다.

로컬 Go 검증과 PR matrix는 race 테스트를 실행합니다. `scripts/ci/go-test-nonrace.sh`가
일반·race 빌드의 테스트 파일을 비교해 race 빌드에서 빠지는 테스트 함수만 추가 실행합니다.
NilAway는 고정 바이너리의 `go vet -vettool` unitchecker로 의존성 분석을 재사용하며,
로컬 기본값과 security workflow는 `-p 1`로 분석 메모리 사용을 제한합니다.

최종 이미지 스캔 인자는 `scripts/ci/final-image-scan-policy.sh`가 소유합니다.
`scripts/ci/check-recurring-security-scan-contract.sh`는 보안 설정을 검사하고,
실제 이미지 스캔은 빌드된 모든 운영 이미지를 검사합니다. 발견 결과는 숨기지 않습니다.

`security.yml`의 SDK·shared-go 체크아웃은 스택에서 검증·게시한 commit SHA로 고정합니다.
과거 release tag를 그대로 쓰면 sibling을 단독으로 검사할 때 보안 패치 전 의존성이 선택될 수 있습니다.
현재 입력은 Go 1.27.3·`x/net v0.61.0`을 사용하며 runtime `go.mod`와 릴리스 태그는 변경하지 않습니다.
`govulncheck`는 기존대로 `go.work`의 모든 모듈을 검사하고, 도달 가능한 취약점이 있으면 실패합니다.

## 보존하는 제품 검사

| 검사 | 스크립트 | 실패 조건 |
|---|---|---|
| native 실행 권한 | `check-repository-ownership.sh` | hololive-api나 shared template 실행기가 `os/exec`·`plugin`을 import함 |
| SQL 안전 정책 | `check-sql-ownership.sh`·`.py` | 런타임 DDL, wildcard projection, 불완전 쿼리, lease·조회 시계 위반 또는 재관측 INSERT의 기존 발송 상태 덮어쓰기 |
| 네트워크 경계 | `check-topology-parity.sh` | 호스트 주소와 방화벽·DB·ingress 설정 불일치 |
| 비밀 유출 | `check-tracked-local-artifacts.sh`·`scripts/refactor/grep-sensitive-logs.sh` | 비밀·운영 산출물 추적 또는 인증 값·응답 원문 로깅 |
| 의존성 정책 | `scripts/ci/check-production-go-workspace.sh`·`check-recurring-security-scan-contract.sh`·`check-go-image-vex.jq` | 비고정 의존성, 취약점 검사 누락·억제 또는 부정확한 VEX 면제 |
| DB 용량 | `scripts/ci/check-postgres-capacity.sh` | pool·예약 connection과 PostgreSQL 용량 불일치 |
| 실행 성능 | `scripts/perf/check-youtube-plane-budget.sh`·`perf-budget.yaml` | 실제 publish/claim/consume 벤치마크의 fleet cycle 시간 초과 |
| 빌드·시험 | `scripts/ci/check-gofmt.sh`·`check-go-test-json.py` | 포맷 위반 또는 실패·panic·예상 밖 skip |

import 위치·심볼 이름만 고정하던 shared-go·notification egress 소유권 검사는 제거했습니다.
Go `internal` 경계와 실제 발송·저장 동작 시험을 유지하며, 제거한 이름을 찾는 별도 검사는 두지 않습니다.
SQL 파일 위치·문서 문자열 검사도 두지 않습니다. `render-release-notes.sh`는 gate가 아닌 수동 기록 도구입니다.
`source-revision-provenance_test.sh`는 checkout·image revision의 실제 판정만 시험하며,
Dockerfile 문구를 고정하던 단언은 제거했습니다. 배포·로그·failover 시험은 실행 결과와 부작용을 검사하므로 유지합니다.

## 로컬 실행

```bash
./scripts/architecture/ci-boundary-gate.sh
FULL_PRE_PUSH=true bash scripts/ci/pre-push-gate.sh
```
