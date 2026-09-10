# T04 adapter 작업 중간 증거

2026-09-09, `DEC-20260909-hololive-admin-bigbang-replacement`. **T04 구현·로컬 Go 검증 완료**입니다. 실제 proxy/cutover·출시 게이트 완료를 뜻하지 않습니다.

## Docker 정책

`contract/docker-policy.json`의 9개 이름·동작에서 `adapters/docker/policy_generated.go`와 `deploy/compose/admin-docker-policy.generated.json`을 생성합니다. 기존 security overlay가 generated Compose service를 extends하고, 계약 checker와 PR frontend gate가 `generate-admin-docker-policy.mjs --check`로 stale 출력을 거부합니다. 정책 hash는 `9e2e88cfd54c8836ff986e561fc542d874550fec5bab6d3b52cec6c8e7254d3a`입니다.

앱의 prefix 허용을 제거하고 실제 중앙 proxy가 허용하던 9개 이름만 허용합니다. source JSON과 별도로 작성한 금지 목록에서 container ID·다른 AP collector·이름 접미사·경로 탈출과 인프라 stop은 Docker I/O 이전에 거부됩니다. 조회 캐시의 취소된 leader에 대한 기존 1회 재조회 계약은 유지하며 변경 요청은 재시도하지 않습니다.

Docker 응답은 204만 완료로 인정하며 start/stop에 한해 304를 이미 목표 상태로 인정합니다. 200/202나 restart의 304는 완료 근거로 사용하지 않습니다. [Docker Engine API v1.52](https://docs.docker.com/reference/api/engine/version/v1.52/)의 공식 [명세](https://docs.docker.com/reference/api/engine/version/v1.52.yaml)를 확인했습니다. local Engine API도 1.52입니다. 이 버전 확인을 중앙 Engine 버전 확인으로 표시하지 않습니다.

`go test -race -count=1 ./admin-dashboard/backend/internal/adapters/docker`가 통과했습니다. 실제 HTTP fake의 action별 12개 결과 사례는 각각 호출 1회이며 금지 사례는 호출 0회입니다. `golangci-lint`는 전체 backend에서 0 issues였습니다. 관측·HTTP·Holo·bootstrap·contract package 시험도 경로 이관 후 통과했습니다.

secret-free 기본 Compose fixture와 실제 security overlay의 `docker compose config --format json`을 확인했습니다. 생성 command와 policy hash가 적용되고 extends가 해소되며 관리자 네트워크는 `admin-docker-proxy-net`과 `hololive-net`입니다. 컨테이너를 실행한 검증은 아니며 실제 socket-proxy deny/allow와 no-deps/cutover 검증은 남아 있습니다.

## 후속 artifact 발견

local `admin-dashboard:security-6ba2ede14`의 image ID는 T01에서 중앙 실행 이미지로 확인한 `sha256:17b56c3a48b055fdab3d60691ec40e90de1f32e65691e1be0718b316b84e4fb7`와 일치했습니다. arm64, revision `6ba2ede142501eb5bc436f9d1af2587a3f1d81b0`입니다. pinned `wollomatic/socket-proxy:1.12.3@sha256:74e770f5ed3cfc9ecb6350e177d2aa55873568c85bc953079834e68607dbf71b`도 local amd64 image로 존재합니다. image inspect에서 ID/architecture/revision만 읽었습니다. T09의 archive export·hash·실제 embedded old bundle 확보는 아직 수행하지 않았습니다.

## 타입이 있는 Holo 경계

`adapters/holo/members.go`, `calendar.go`, `httpapi/holo_reads.go`에서 두 GET을 타입이 있는 응답 projection과 query allowlist로 연결했습니다. 큰 ID·필수 값 부재·null 배열/항목·알 수 없는 응답 필드·월/연도 범위를 검사합니다. `domain/org_member.go`의 optional Aliases와 GetAllAliases의 빈 목록 의미에 따라 별명 자체의 부재만 빈 별명으로 투영합니다. 별명 객체가 있으면 ko/ja 컬렉션과 항목의 형식을 요구합니다. 범용 ID 재작성 helper와 그 전용 시험은 삭제하고 실제 typed 조회 시험으로 대체했습니다.

Holo `Proxy`/`ProxyResponse`는 private request/upstreamResponse로 바꾸었고 Gin factory는 제거했습니다. 10개 GET과 13개 mutation 모두 고정 upstream 경로와 타입이 있는 request/response projection을 사용합니다. 중립 오류는 `contract/errors.go`가 소유하여 adapters/observations가 Gin을 import하지 않습니다. Holo와 Docker의 redirect를 차단하여 POST 후 다른 목적지로 요청이나 API key를 전달하지 않습니다. 307 주입 시험에서 원래 호출 1회·목적지 호출 0회가 확인되었습니다. 잘못된 upstream 상태는 선언한 BFF 오류로 처리합니다.

`go test -race -count=1 ./admin-dashboard/backend/internal/adapters/... ./admin-dashboard/backend/internal/httpapi`가 통과했습니다. `npm run test:contract`는 HTTP DTO와 Holo typed 응답을 각각 Go fixture로 받아 같은 generated validator로 검증했고 6+4개가 통과했습니다. Go test의 분할된 JSON 로그는 package별로 합쳐 검사합니다.

AddMember의 upstream 201은 BFF 200으로 투영합니다. DeleteAlarm은 실제 roomId/channelId 범위와 removed를 보존하며 [계약 정정](upstream-contract-reconciliation.md)에 따라 미지원 user 필드와 UI 사용자 그룹을 제거했습니다. Settings는 저장값을 확인하고 optional runtime 필드의 부재·false를 구분하며 apply/publish 부분 효과를 보존합니다. publish 원문 오류는 고정 문구로 바꾸고 모순된 결과·null·범위 이탈은 거부합니다. SetACL의 명세 밖 message와 모든 미소유 upstream 필드는 투영하지 않습니다. upstream·업무 DB 소스는 변경하지 않았습니다.

`httpapi/holo_mutations_test.go`는 13개 정상 변경의 실제 method/path/body와 upstream 호출 1회를 확인합니다. 잘못된 입력·null·알 수 없는 필드·큰 ID 초과·query 혼입은 호출 0회입니다. `corepack npm run test:contract`의 6+4개 시험이 현재 generation `6ea296ecb3de7bd7878e9396de16bd8384474f81bea873ba0fc3b460e5eadbb2`에서 통과했습니다. HTTP·Holo DTO에 같은 110개 generated validators를 적용합니다. `npm` 직접 실행은 설치된 npm 11과 프로젝트 npm 12의 차이로 실행 전 거부되었고, 이미 설치된 Corepack의 pinned npm 12.0.2로 검증했습니다.

## WebSocket과 종료

`observations.Streams`는 필수 Origin/session/error policy를 주입받아 생성됩니다. `admin-stats.<contractGeneration>` 하나만 허용하고 generation → Origin → 인증 순으로 검사한 후 업그레이드합니다. 기존 프로세스 16개·family 4개 상한, 60초 pong/54초 ping, 1초 family 폐기 감시, history 30개를 보존합니다. Close는 새 연결을 막고 소켓·peer reader·family watcher·구독을 모두 회수합니다. 실제 WebSocket 시험에서 협상 전 history 전달 없음, 잘못된 세대/Origin에서 인증 호출 0회, 종료 후 새 연결 거부와 감시/구독 회수를 확인했습니다.

HTTP admission은 metadata·health·정적 파일 외 등록 API와 docs/WS의 신규 진입을 닫습니다. 종료 시 requestId·operation·mutation·시각과 전송 시도 여부를 snapshot으로 남깁니다. 이미 허용된 요청은 이후 전송될 수 있으므로 `not_dispatched`는 해당 snapshot 시점의 관찰일 뿐 취소 증명이 아닙니다. adapter I/O 직전의 기록은 receipt나 수신 확인이 아닙니다. 전송 후 5xx/취소는 감사 로그의 `outcome_unknown`으로 남기며 재실행하지 않습니다.

Runtime은 25초 총 예산 안에서 admission 차단·in-flight 분류 → WS/hub 정리 → 최대 20초까지 HTTP drain → 초과 연결 강제 종료·handler 정리 → sampler·Docker/Holo client·limiter/store 순으로 종료합니다. 실제 HTTP store 호출을 막은 시험에서 정상 drain과 timeout 강제 종료 후 handler의 store 사용 종료를 확인했습니다. 운영 SIGTERM·실제 구형 artifact 전환 리허설을 대신하는 결과는 아닙니다.

Gin 외 오류도 공용 `ginjson.JSON` renderer의 HTML escaping과 같은 오류 envelope를 사용합니다. 실패한 JSON 쓰기 뒤 plain text 오류를 덧붙이던 경로를 제거했습니다. 보안/종료 시험 이관 후 Go lint는 0 issues였고 전체 Go gate는 별도 최종 결과를 아래에 기록합니다.

## 최종 검증과 후속 범위

`bash scripts/ci/admin-dashboard-go-ci.sh` exit 0: gofmt, workspace/tidy drift, vet, staticcheck, golangci-lint 0 issues, NilAway, build, 전체 test·race, govulncheck가 통과했습니다. 로그는 `/tmp/hololive-admin-t04-go-ci.log`입니다. govulncheck 호출 코드·import package 취약점은 0개이며 모듈 수준 1개는 T03에서 기록한 미사용 openpgp advisory와 같습니다. NilAway가 지적한 URL query slice의 직접 인덱싱은 `Values.Get`으로 바꾸고 WS 거부 응답의 nil 확인을 추가했습니다. 검사를 제외하지 않았습니다.

실제 socket-proxy 실행, 구형/후보 artifact cutover·rollback은 T09/T10의 검증이며 아직 PASS가 아닙니다. 프런트엔드 WS 연결·세션 소유권·세대 중단은 T05, 전체 UI의 관측 실패 표시와 retirement source/import/bundle/image 대조는 T07/T08에 남아 있습니다.

Fallback delta: 실패한 JSON 쓰기 뒤 plain text 응답을 덧붙이는 경로를 제거했습니다. 새로운 변경 재시도·대체 provider·호환 이름은 없습니다. 기존 조회 cache의 취소된 leader 재조회, WS 재연결 예산은 확대하지 않았습니다.
