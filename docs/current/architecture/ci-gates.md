# CI Gates

## 검사 범위

Architecture gate는 SQL 안전 정책과 보안·배포 설정을 검사합니다.
migration 적용 목록과 작성 규약은 `hololive-api`의 `internal/migrationrunner` Go 시험이
러너와 같은 manifest 파서·문장 분리기로 검사합니다.
문서 표현·경로, 패키지 이름과 퇴역 이름의 재등장은 별도 게이트로 검사하지 않습니다.
stack AGENTS.md의 기준에 따라 checker 자체 테스트, 문서 동기화와 제거 코드 문자열 검사는 두지 않습니다.

## 실행 순서

`scripts/architecture/ci-boundary-gate.sh`는 비밀·로컬 산출물 추적,
SQL 안전 정책과 topology 설정 일치를
검사한 뒤 환경 파일·health gate·서비스 선택·AP QUIC UDP 버퍼 판정·원격 로그 동기화의 동작 테스트를 실행합니다.
native 실행 권한 제한은 golangci-lint `depguard`가 맡습니다.

Go 파일·함수 크기는 golangci-lint의 `revive` `file-length-limit`(비테스트 파일 800줄)과
`funlen`(함수 120줄)이 검사합니다. 복잡도는 설정된 린터가 맡으며 별도 구조 예산 검사는 없습니다.
staticcheck도 golangci-lint에 포함된 판본으로 한 번만 실행하며 별도 staticcheck 실행은 두지 않습니다.

로컬 Go 검증과 PR matrix는 race 시험을 실행합니다. `go-test-nonrace.sh`는 일반 빌드에만 포함된 시험 파일이 있는 패키지에서 Go에 등록된 Test·Example·Fuzz를 비교합니다. 양쪽에 서로 다른 전용 파일이 있으면 같은 이름도 의미가 다를 수 있으므로 해당 패키지는 일반 모드로 전부 실행합니다.
collector CGO 비활성 시험·성능 측정·Go 시험과 명시적으로 선택한 통합 시험은 일회용 PostgreSQL 서버를 공유합니다. dbtest는 시험별 DB를 복제하며 자동 생성한 서버만으로 통합 시험을 활성화하지 않습니다.
NilAway는 고정 바이너리의 `go vet -vettool` unitchecker로 의존성 분석을 재사용하며,
로컬 기본값과 security workflow는 `-p 1`로 분석 메모리 사용을 제한합니다.

`run-npm-audit.sh`는 추적된 모든 `package-lock.json`을 직접 감사합니다. 별도 등록 목록은 없지만 `packageManager`로 npm을 선언한 프로젝트에서 lockfile만 추적 해제하면 실패합니다. 한 프로젝트의 실패로 나머지 감사를 생략하지 않습니다.
이미지는 중앙 Quadlet의 운영 이미지와 arm64 스캔 목록을 대조합니다. Trivy 억제 금지 설정과 실제 취약점 판정은 별도 검사입니다. 태그의 플랫폼과 불변 ID를 한 번에 읽고, Go finding은 같은 ELF에서 추출·검증한 심볼로 VEX를 판정합니다. 다른 이미지나 lockfile의 검사가 성공해도 앞선 실패를 덮지 않습니다.

`security.yml`의 SDK·shared-go 체크아웃은 스택에서 검증·게시한 commit SHA로 고정합니다.
과거 release tag를 그대로 쓰면 sibling을 단독으로 검사할 때 보안 패치 전 의존성이 선택될 수 있습니다.
Go 판본은 `go.work`가, 외부 소스는 workflow의 checkout SHA가 정합니다.
`govulncheck`는 기존대로 `go.work`의 모든 모듈을 검사하고, 도달 가능한 취약점이 있으면 실패합니다.

## 보존하는 제품 검사

| 검사 | 스크립트 | 실패 조건 |
|---|---|---|
| native 실행 권한 | golangci-lint `depguard` `hololive-native-exec` 규칙(iris-stack `tools/lint/golangci.template.yml`) | hololive-api나 shared template 실행기의 비시험 파일이 `os/exec`·`plugin`을 import함 |
| SQL 안전 정책 | `check-sql-ownership.sh`·`.py` | 런타임 DDL, wildcard projection, 불완전 쿼리, lease·조회 시계 위반 또는 재관측 INSERT의 기존 발송 상태 덮어쓰기 |
| 네트워크 경계 | `check-topology-parity.sh` | 호스트 주소와 방화벽·DB·ingress·중앙 Quadlet·collector Podman unit 설정 불일치 |
| 비밀 유출 | `check-tracked-local-artifacts.sh`·`scripts/refactor/grep-sensitive-logs.sh` | 비밀·운영 산출물 추적 또는 인증 값·응답 원문 로깅 |
| 의존성 정책 | `scripts/ci/check-production-go-workspace.sh`·`check-go-image-vex.jq` | 비고정 의존성 또는 부정확한 VEX 면제 |
| DB 용량 | `scripts/ci/check-postgres-capacity.sh`(예산 산식은 배포 preflight와 같은 `scripts/deploy/lib/postgres-capacity.sh`) | pool·예약 connection과 PostgreSQL 용량 불일치 |
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
