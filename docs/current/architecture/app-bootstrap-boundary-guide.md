# App Bootstrap Boundary Guide

`hololive-api` bot plane의 현재 bootstrap·HTTP·runtime 소유 경계입니다.

## 현재 경계 — 2026-10-02

| 경로 (`hololive/hololive-api/` 기준) | 책임 |
|---|---|
| `internal/planes/bot/runtime/` | bot 구성 조립, 시작·종료 순서, durable ingress/reply, runtime 시험 |
| `internal/planes/bot/internal/app/bootstrap/` | provider·service·서버 구성과 bootstrap helper |
| `internal/planes/bot/internal/app/http/` | router·middleware·route 및 shortlink handler |

HTTP 서버 시작·종료 helper는 `internal/planes/bot/runtime/http_server_helpers.go`가 소유하며, 실제 runtime이
같은 package의 private helper를 직접 사용합니다. raw HTTP/3의 정상 종료·listener 소실·진행 중 요청 처리 방식을
바꾸는 작업은 별도 행동 변경입니다.

## 현재 자원과 구성 소유권

Bot·admin plane이 빌려 쓰는 공통 서비스(ACL·Holodex·공식 일정) 생성은 `internal/app`, API 설정은 `internal/config`가
소유합니다. plane별 pool(`bot`·`admin`·`llm_scheduler`·`youtube_plane`)은 따로 두고, Valkey client·member cache·ACL·Holodex는
process가 하나만 두며 plane은 빌려 씁니다.
Bot의 획득 자원은 즉시 rollback owner에 등록하고 성공 시 같은 owner를 runtime에 이전합니다.
Orchestration의 readiness 포트에는 Close가 없습니다. Holodex·member cache·Valkey와 `bot`·`admin`·`llm_scheduler` pool의 회수는
`internal/app` process 조립(`apiInfrastructure.close`)이 맡고, `youtube_plane` pool은 그 pool을 연 `internal/planes/youtube/runtime`이 회수합니다.
Durable sampler·certificate reload 등 background task를 cancel/join한 뒤 member-cache 작업을 끝내고
PG/cache를 해제합니다. `CloseContext`가 공유된 남은 종료 예산을 사용하며, join 미완료에서 자원을 닫거나
동일 cleanup을 병렬로 다시 시작하지 않습니다. Fatal/drain/cleanup 오류는 합쳐서 보존합니다.

## 검증

저장소 루트에서 실제 소비자와 bootstrap 회귀를 실행합니다.

```bash
go test ./hololive/hololive-api/internal/planes/bot/internal/app/...
go test -race ./hololive/hololive-api/internal/planes/bot/runtime \
  -run 'TestBotRuntime(StartHTTPServer|ShutdownHTTPServer|CloseContext|CloseBeforeStart|ClosePreservesFatal)'
```

서버 기동 오류 전파, nil 구성, shortlink listener drain 등 제품 동작으로 판단합니다.
package 이름·파일 수나 제거된 경로의 문자열 부재를 별도 gate로 만들지 않습니다.
