# App Bootstrap Boundary Guide

`hololive-api` bot plane의 현재 bootstrap·HTTP·runtime 소유 경계입니다.

## 현재 경계 — 2026-10-02

| 경로 (`hololive/hololive-api/` 기준) | 책임 |
|---|---|
| `internal/planes/bot/runtime/` | bot 구성 조립, 시작·종료 순서, durable ingress/reply, runtime 시험 |
| `internal/planes/bot/internal/app/bootstrap/` | provider·service·서버 구성과 bootstrap helper |
| `internal/planes/bot/internal/app/http/` | router·middleware·route 및 shortlink handler |

단일 소비자였던 `internal/planes/bot/internal/app/runtime/http_server.go`는
`internal/planes/bot/runtime/http_server_helpers.go`로 흡수했습니다. 실제 runtime이 같은 package의
private helper를 직접 사용하며, 이전 package의 alias나 forwarding 함수는 남기지 않습니다.

이 이동은 nil 처리, 서버별 로그 prefix, 오류 wrapping과 종료 호출 순서를 보존합니다.
raw HTTP/3의 정상 종료·listener 소실·진행 중 요청 처리 방식을 바꾸는 작업은 별도 행동 변경입니다.

## 현재 자원과 구성 소유권

[통합 리팩토링 계획](../../design/2026-10-02-hololive-api-refactoring.md)에 따라 공통 서비스 생성은 접근 가능한
`internal/apifoundation`, API 설정은 `internal/config`가 소유합니다. 각 plane의 DB pool·인스턴스는 별도로 유지합니다.
Bot의 획득 자원은 즉시 rollback owner에 등록하고 성공 시 같은 owner를 runtime에 이전합니다.
Orchestration의 readiness 포트에는 Close가 없으며 DB/cache/Holodex 수명은 plane이 관리합니다.
Durable sampler·certificate reload 등 background task를 cancel/join한 뒤 member-cache 작업을 끝내고
PG/cache를 해제합니다. `CloseContext`가 공유된 남은 종료 예산을 사용하며, join 미완료에서 자원을 닫거나
동일 cleanup을 병렬로 다시 시작하지 않습니다. Fatal/drain/cleanup 오류는 합쳐서 보존합니다.

과거 `internal/app` façade·`internal/app/internal/botruntime`·wiring 배치는 현재 경로 계약으로 사용하지 않습니다.

## 검증

저장소 루트에서 실제 소비자와 bootstrap 회귀를 실행합니다.

```bash
go test ./hololive/hololive-api/internal/planes/bot/internal/app/...
go test -race ./hololive/hololive-api/internal/planes/bot/runtime \
  -run 'TestBotRuntime(StartHTTPServer|ShutdownHTTPServer|CloseContext|CloseBeforeStart|ClosePreservesFatal)'
```

서버 기동 오류 전파, nil 구성, shortlink listener drain 등 제품 동작으로 판단합니다.
package 이름·파일 수나 제거된 경로의 문자열 부재를 별도 gate로 만들지 않습니다.
