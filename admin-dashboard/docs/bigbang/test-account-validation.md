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

메타 저장소의 마지막 게시 검사에서 CLI 만료 시각의 정수 `omitempty` 표기 한 건을 확인해 제거했습니다. Go 1.27.1 `encoding/json/v2`는 숫자 0을 `omitempty`로 생략하지 않으므로, `expires_at_unix`를 필수 필드로 명시하여 발급·활성 계정의 실제 만료 시각과 부재·폐기 응답의 0을 유지했습니다. 해당 출력 검사를 수정 전에도 통과하는지 확인한 뒤 태그를 명시화했으며, 수정 후 전체 관리자 Go CI와 `check-stack-modern-go.sh`의 12개 모듈 drift·정적 정책 검사를 통과했습니다. 운영 인수 이후의 이 보완은 응답 형식을 바꾸지 않으며, 아래 실제 배포 source와 후속 게시 source를 구분합니다.

발급 파일을 먼저 기록한 뒤 Valkey에 생성합니다. I/O 결과가 불명확하면 자동 재발급하지 않고 파일에 남은 식별자와 현재 상태를 대조합니다. 테스트 계정의 세션과 family는 같은 계정에 바인딩되며, 조회·갱신·회전의 원자 연산에서 계정 상태를 확인합니다. 폐기 후 저장소에 남는 세션 레코드는 권한을 잃고 기존 TTL로 정리됩니다.

G302 억제는 CLI 음성 검사의 임시 디렉터리를 0750으로 만드는 한 줄에만 적용했습니다. 디렉터리를 일반 파일로 판단하는 오탐이며, 해당 권한을 실제로 거부하는 검사는 유지합니다. 다른 정적 검사나 경쟁 상태 검사를 우회하지 않았습니다. 추가·수정한 설명 주석은 한국어입니다.

Fallback delta: none. 새 의존성·공개 발급 endpoint·업무 변경 권한은 없습니다. 기존 관리자 cookie/CSRF와 공개 wire generation은 유지합니다. 화면의 업무 변경 버튼은 기존 표시를 유지하며 서버가 임시 계정의 변경 요청을 거부합니다.

## 운영 인수

PR [#486](https://github.com/park285/hololive-bot/pull/486)의 정상 pre-push 검사는 전체 Go build/test/race/lint/NilAway, 프런트 175건·3개 엔진(skip 0), 모든 모듈의 실제 호출 취약점 0건을 확인했습니다. UTC 회귀를 수정한 최종 GitHub 실행 [34490464652](https://github.com/park285/hololive-bot/actions/runs/34490464652)은 11개 job과 fast-gate를 모두 통과했습니다. main에 반영된 `be4a48737036000ba8209340458c38921c2480a6`의 전체 source tree가 검사한 `2702ea41720fa496ad553682462c7ccba7644d95`와 같은지 확인했습니다.

깨끗한 main source를 kapu의 `kapu-multiarch`에서 `linux/arm64`로 빌드했습니다. 검증·전송한 image는 `sha256:f7fd9b5a163d51839945b5164e8e379cc49479eea0a825439cb3fd9c345c08f6`이며 원격에서 architecture·40자리 revision·UID/GID를 다시 확인했습니다. 전송 묶음에는 image, SBOM, manifest, checksum, 상태 기준과 새 운영 CLI 설명만 포함했습니다. 중앙 release는 `/opt/hololive-bot/releases/admin-test-account-20260910T145155Z-be4a48737`입니다.

2026-09-10 23:52:01 KST에 관리자 ingress를 정비 상태로 전환했습니다. 이전 BFF는 445ms 안에 exit 0·OOM 없이 종료됐고, `up -d --no-build --no-deps --force-recreate admin-dashboard`로 해당 서비스만 반영했습니다. health 통과 뒤 origin을 개방했습니다. 이전 image `sha256:d22b9b83465e04f4c5976b4a2f1f71d699592827e43003dc7fb9bccb9e9c68ef`는 `admin-dashboard:rollback-admin-test-account-20260910T145155Z-be4a48737` tag로, 이전 runbook은 release의 root 전용 `rollback/`에 보존했습니다.

23:52:18 KST부터 공개 `https://admin.holoshi.com`에서 실제 Chrome을 사용했습니다. 정상 CLI로 15분 조회 계정을 한 번 발급하고 로그인 화면에서 제출했습니다. 자격증명은 컨테이너의 0600 파일과 검증 프로세스 메모리 안에서만 사용했으며 cookie·CSRF·응답 본문·화면·trace를 파일이나 출력에 남기지 않았습니다.

| 실제 운영 검사 | 결과 |
| --- | --- |
| 로그인 화면에서 임시 계정 제출 | HTTP 200, 통계 화면 이동 |
| stats·streams·members·calendar·alarms·rooms·settings | 7개 메뉴 렌더링, 11개 조회 요청 모두 200, 오류 alert 없음 |
| CSRF 없는 / 유효 CSRF heartbeat | 403 / 200 |
| 두 인증 cookie | HttpOnly·Secure·SameSite=Strict |
| 통계 WebSocket | 정상 연결·2개 frame 수신·오류 0 |
| 지정 계정 폐기 | `revoked`, 기존 WS close code 1008 |
| 폐기 뒤 동일 cookie / 재로그인 | 401 / 401 |
| 최종 계정·파일 | `absent`, 자격증명 파일과 소유한 임시 디렉터리 제거 |
| 브라우저·CSP·업무 mutation | page error 0, CSP 위반 0, 업무 mutation 0 |

검증은 300초 상한·`KillMode=control-group`·core dump 금지인 transient unit에서 16.727초 만에 성공 종료했습니다. 이는 로컬 브라우저 검증 프로세스의 실행 시간이며 서버 성능 표본이 아닙니다. 폐기된 세션 레코드는 기존 TTL까지 남을 수 있지만 모든 해당 인증 권한은 즉시 사라집니다.

23:53:17 KST 재확인에서 운영 image가 위 revision과 일치하고 healthy·재시작 0·OOM 없음, 전환 이후 error/panic/fatal 계열 표지 0건, 테스트 계정 `absent`를 확인했습니다. 다른 7개 컨테이너의 ID·image·시작 시각·재시작 등 상태와 실제 Compose overlay 경로가 전환 전과 같습니다. 기존 관리자 파일 4개와 master env의 소유권·mode·크기·inode·mtime·ctime도 변하지 않았습니다. 기존 관리자 암호·서명 키 변경과 세션 prefix purge는 수행하지 않았습니다.

이 실제 인수로 기존 교체 계획의 T11/AC11/V11에 남아 있던 로그인·조회·실시간 통계 검증을 충족했습니다. 기존 RSS 회복 검사의 실패와 사용자가 수용한 성능 예외는 유지하며 이 기능의 검사로 해소됐다고 주장하지 않습니다.
