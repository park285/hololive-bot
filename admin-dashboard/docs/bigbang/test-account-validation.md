# 관리자 임시 조회 계정 검증

정본 결정은 `DEC-20260910-hololive-admin-temporary-test-account`입니다. 기존 관리자 암호를 유지하며 CLI가 한 개의 임시 조회 계정을 발급·조회·폐기합니다. 기본 유효 기간은 15분이며 1~60분 범위를 허용합니다.

## 구현과 로컬 검증

2026-09-10에 다음 검사를 통과했습니다.

| 검사 | 결과와 근거 |
| --- | --- |
| `bash scripts/ci/admin-dashboard-go-ci.sh` | Go-only, 모듈 정합성, gofmt, vet, staticcheck, golangci-lint 0건, NilAway, 전체 build/test/race 통과 |
| 같은 CI의 govulncheck | 호출 경로 취약점 0건, import된 패키지 취약점 0건. require된 모듈에는 호출하지 않는 취약점 1건이 보고됨 |
| `bash scripts/architecture/check-admin-contract.sh` | endpoint inventory·OpenAPI·생성된 클라이언트 계약 검사 통과 |
| `bash scripts/ci/check-structure.sh --mode hard --format json` | CLI 파싱과 인자 제약 검증을 분리한 뒤 hard 위반 0건. 새 CLI 의존 관계 3개를 생성된 import graph에 반영 |
| `internal/session/test_account_test.go` | 동시 발급 한 건, 중복 보존, 잘못된 식별자 폐기 거부, 만료·폐기·재발급·회전 경쟁과 저장소 오류 구분 |
| UTC·Asia/Seoul 만료 시각 회귀 | GitHub에서 확인한 `Local`·`UTC` 표현 차이를 로컬 UTC에서 재현. 동일 시각을 오차 0으로 비교하도록 수정하고 두 시간대의 경쟁 상태 검사 통과 |
| `internal/auth/crypto_domain_test.go` | 기존 관리자 서명 유지, 임시 세션의 별도 HMAC 도메인과 구형 서명 검증기 거부 |
| `internal/httpapi/test_account_test.go` | 정상 로그인·조회·CSRF·logout, 모든 업무 endpoint 403과 claim 미실행, 폐기 후 WS 1008·HTTP 401·재로그인 401, 관리자 실패 예산 보존 |
| `internal/testaccountcli/command_test.go` | 출력에 자격증명 없음, 새 0600 파일, 기존 파일·symlink·공개 디렉터리 거부, 결과 출력 실패 시 발급 파일 보존 및 정확한 계정 폐기 |

발급 파일을 먼저 기록한 뒤 Valkey에 생성합니다. I/O 결과가 불명확하면 자동 재발급하지 않고 파일에 남은 식별자와 현재 상태를 대조합니다. 테스트 계정의 세션과 family는 같은 계정에 바인딩되며, 조회·갱신·회전의 원자 연산에서 계정 상태를 확인합니다. 폐기 후 저장소에 남는 세션 레코드는 권한을 잃고 기존 TTL로 정리됩니다.

G302 억제는 CLI 음성 검사의 임시 디렉터리를 0750으로 만드는 한 줄에만 적용했습니다. 디렉터리를 일반 파일로 판단하는 오탐이며, 해당 권한을 실제로 거부하는 검사는 유지합니다. 다른 정적 검사나 경쟁 상태 검사를 우회하지 않았습니다. 추가·수정한 설명 주석은 한국어입니다.

Fallback delta: none. 새 의존성·공개 발급 endpoint·업무 변경 권한은 없습니다. 기존 관리자 cookie/CSRF와 공개 wire generation은 유지합니다. 화면의 업무 변경 버튼은 기존 표시를 유지하며 서버가 임시 계정의 변경 요청을 거부합니다.

## 운영 인수

운영 반영과 실제 임시 계정 검증은 아직 실행하지 않았습니다. 정상 발행 이후 중앙 `admin-dashboard`만 반영하고, 로그인·7개 메뉴 조회·heartbeat CSRF·통계 WS를 검사합니다. 계정 폐기 후 동일 cookie와 새 로그인 거부, WS 종료, 계정 부재와 자격증명 파일 제거를 별도 기록해야 합니다. 기존 RSS 회복 검사의 예외는 이 기능의 검사로 해소됐다고 주장하지 않습니다.
