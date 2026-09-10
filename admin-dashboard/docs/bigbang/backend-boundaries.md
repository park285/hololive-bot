# T03 설정·HTTP·인증 경계 검증

2026-09-09, `DEC-20260909-hololive-admin-bigbang-replacement`. [T02](contract-implementation.md)에 이어 수행한 로컬 구현·검증입니다. 운영 설정·secret·세션·이미지는 변경하지 않았습니다.

## 구현

- `config/load.go`가 필요한 환경 이름을 한 번 읽고 `secrets.go`에서 `_FILE` 입력을 결합한 뒤 `Config.Validate`를 한 번 호출합니다. process 환경 materialization·복원과 이중 Load 경로를 제거했습니다. 기존 설정 alias는 유지합니다.
- 잘못된 숫자·boolean·security mode·기간 overflow는 시작 오류이며 입력 원문을 오류에 넣지 않습니다. production은 HTTPS·CSRF/WS enforce·명시한 trusted proxy 범위·정확한 origin을 요구합니다. SESSION_SECRET 최소 32 bytes를 유일한 loader에서 검증합니다.
- secret 파일은 regular/non-symlink·64 KiB·NUL/개행·open 전후 identity 검사에 owner/mode 검사를 추가했습니다. root 또는 runtime uid 소유, 소유자만 쓰기, runtime group까지만 읽기를 허용하여 owning materializer의 root:runtime-group 0640 계약을 보존합니다. NOFOLLOW/NONBLOCK으로 검사 중 symlink/FIFO 교체를 제한합니다.
- 기존 `internal/app`을 제거하고 `bootstrap`이 외부 자원과 수명, `httpapi`가 route·인증·응답을 소유합니다. 분산 로그인 limiter는 `session`으로 옮겼고 미초기화 limiter는 로그인을 거부합니다. local limiter·bcrypt slot·timing·세션/CSRF 알고리즘·회전 유예·family 폐기·store 실패 거부를 유지합니다.
- `httpapi/access.go`에 35개 endpoint의 method/path/operation/access/handler를 명시하고 생성 inventory와 양방향 대조합니다. 누락·중복·미분류·접근 완화는 서버 listen 전에 실패합니다. health·static·인증된 docs·WS는 별도 명시 경로입니다.
- 보안 헤더는 `httpapi/security.go` 하나가 설정합니다. API·metadata는 no-store, metadata에 ETag/304를 적용하지 않으며 API 404/405와 panic에는 안전한 envelope를 사용합니다. credential이 섞일 수 있는 panic/request dump를 남기지 않습니다. 누락 API·asset을 SPA로 반환하거나 trailing-slash redirect하지 않습니다.

## 실제 확인

`bash scripts/ci/admin-dashboard-go-ci.sh` **exit 0**: toolchain, Go-only, go.work sync drift, go mod tidy diff, gofmt, vet, staticcheck, golangci-lint(0 issues), NilAway, build, 전체 backend test, 전체 race test, govulncheck가 통과했습니다. 시험이 있는 package는 auth/bootstrap/config/contract/docker/holo/httpapi/httpx/session/static/status 11개입니다. `bash scripts/architecture/check-admin-contract.sh`도 새 httpapi 경로에서 통과했습니다. `git diff --check`는 통과했습니다.

추가한 핵심 실행 사례는 환경값 거부·기간 overflow·파일 권한/교체·지원 alias/production 설정, 누락/미분류/접근 완화 route 거부, 404/405의 JSON/보안 헤더, panic의 secret 비노출, 기동 context 취소 후 관측 유지입니다. 기존 HTTP/WS family 폐기, 회전 후 한도 유지, store 실패 시 WS 종료, CSRF cookie와 cleanup context 사례도 전체 test/race gate에 포함했습니다.

CI에서 발견한 NilAway 지적은 멤버 projection의 map nil 불변식이었습니다. null envelope/item/calendar member를 명시적으로 거부하고 null fixture를 추가했습니다. 검사 억제나 nil 기본값을 추가하지 않았습니다. miniredis는 client tracking을 지원하지 않으므로 시작 수명 시험은 bootstrap이 소유한 관측 시작 함수를 실제 취소한 context로 검증합니다. 이를 실제 Valkey 또는 전체 운영 프로세스 기동 시험으로 표시하지 않습니다.

govulncheck의 호출·import package 취약점은 0건입니다. module 수준 1건은 `golang.org/x/crypto@v0.56.0`의 **GO-2026-5932**로, 미유지 OpenPGP 패키지에 해당합니다. [Go 공식 취약점 기록](https://pkg.go.dev/vuln/GO-2026-5932). `go list -deps ./admin-dashboard/backend/...`에서 해당 범위는 bcrypt만 나오며 openpgp 하위 package는 포함되지 않았습니다. 이 모듈을 취약점 없는 모듈이라고 표시하거나 인증 의존성을 불필요하게 교체하지 않았습니다.

## 다음 경계

typed upstream/Docker adapter와 공통 Docker 정책, WebSocket subprotocol·admission·drain·종료 수명은 T04, frontend authGeneration/CSRF 상태와 metadata 활성화는 T05의 미완료 작업입니다. 전체 production CSP·운영 메타데이터·세대 양방향·성능·cutover/rollback 게이트는 아직 NOT RUN입니다. T03 결과는 최종 후보 G01~G12의 PASS가 아닙니다.

Fallback delta: none. 잘못된 입력의 default-on-error와 미초기화 limiter의 허용을 제거했으며 새 재시도·대체 경로는 추가하지 않았습니다.
