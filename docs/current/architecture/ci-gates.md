# CI Gates

## 검사 범위

Architecture gate는 코드 경계, SQL·migration 소유권과 배포 설정을 검사합니다.
문서 표현·경로, 패키지 이름과 퇴역 이름의 재등장은 별도 게이트로 검사하지 않습니다.
stack AGENTS.md의 기준에 따라 checker 자체 테스트, 문서 동기화와 제거 코드 문자열 검사는 두지 않습니다.

## 실행 순서

`scripts/architecture/ci-boundary-gate.sh`는 shared-go 경계·패키지 allowlist, 추적된 로컬 산출물,
내부 route 하드코딩, migration manifest, SQL 소유권, runtime import 경계,
notification egress 소유권, topology 설정 일치를 검사한 뒤 배포·Compose·로그·failover
스크립트의 실제 동작 테스트를 실행합니다.

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
현재 입력은 Go 1.27.2·`x/net v0.60.0`을 사용하며 runtime `go.mod`와 릴리스 태그는 변경하지 않습니다.
`govulncheck`는 기존대로 `go.work`의 모든 모듈을 검사하고, 도달 가능한 취약점이 있으면 실패합니다.

## 주요 코드 경계

| 검사 | 스크립트 | 실패 조건 |
|---|---|---|
| 내부 route 상수 | `check-internal-route-hardcoding.sh` | 허용된 파일 밖에서 내부 route를 하드코딩함 |
| 저장소 소유권 | `check-repository-ownership.sh` | 다른 runtime의 internal 코드나 소유권 밖 저장소에 직접 접근함 |

## 로컬 실행

```bash
./scripts/architecture/ci-boundary-gate.sh
FULL_PRE_PUSH=true bash scripts/ci/pre-push-gate.sh
```
