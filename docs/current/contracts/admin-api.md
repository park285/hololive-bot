# Contract: admin.api

## Summary

hololive-api admin plane의 `/api/holo/*` 관리 HTTP API 요청·응답 schema는 생산자인 hololive-api가 소유합니다.
정본은 [admin-api.openapi.json](admin-api.openapi.json)(OpenAPI 3.1)이고, 이 파일에는 생산자 시험이 실제 handler
요청·응답으로 검증한 operation만 둡니다. 현재 범위는 방·ACL 관리(`GET/POST/DELETE /api/holo/rooms`,
`GET /api/holo/rooms/joined`, `POST /api/holo/rooms/acl`)와 주간 일정 포스터 검토(`/api/holo/schedule-posters*` 13개)입니다. iris-console은 이 범위의 schema를 고정한
hololive-bot revision에서 가져와 gateway 검증용 생성 사본을 만들고, 자기 공개 경로(`/admin/api/holo/*`)와
gateway 고유 header·오류 envelope를 소유합니다. 범위 밖 `/api/holo/*` operation의 schema는 생산자 시험이 붙기 전까지
iris-console `contracts/hololive/openapi.json`이 소유합니다.

- 인증: 모든 operation은 `X-API-Key` header를 요구합니다(없으면 401, 틀리면 403).
- 오류: 성공이 아닌 응답은 `{"error": "<message>"}`이고, 호출자가 분기해야 하는 거절은 안정 `code`를 함께 둡니다
  (예: 방 변경의 `409 acl_mode_changed`). 생산자 오류 문구는 계약이 아닙니다.
- 목록 응답(`rooms`·`alarms`·`members`)은 페이지 없이 전체 목록입니다. 생산자는 응답 크기 상한을 두지 않으며,
  표시·본문 상한은 소비자가 소유합니다.
- 방 변경(`POST/DELETE /api/holo/rooms`)은 필수 `mode`를 받고, 이미 원하는 상태면 `200 changed:false`입니다.
  상세 의미는 [settings 계약](settings.md)을 따릅니다.
- 일정 포스터 operation은 목록·요약·상세·번역본 읽기, 수집 예약(`POST /collect`), 확정·주 범위 수정·거절·재번역
  (`POST /{id}/confirm|week|reject|translate`), 일정표 행만 다시 뽑기(`POST /{id}/rows`), 글·변경 목록
  (`GET /{id}/changes`), 글 단위 변경 철회와 되살리기(`POST /{id}/changes/revoke|restore`)입니다. 성공 응답은 모두
  200이고 경로 변수는 포스터 `{id}` 하나뿐이라 철회하거나 되살릴 글 ID는 본문 `postId`에 둡니다. 포스터 응답의
  `rowsStatus`·`rowsError`·`rowsExtractedAt`·`rowsCount`는 확정본에서만 뜻이 있습니다. 행 재추출은 revision을 올리지
  않습니다. 글·변경 목록, 철회, 되살리기는 같은 글 범위(같은 멤버, 포스터 게시 뒤, 주 첫날 00:00 JST부터 주 마지막 날
  다음 날 00:00 JST 전까지)를 다루고, 범위 밖 글의 철회·되살리기는 404입니다. 목록은 조치할 수 있는 상태의 글 최근
  200건과 나머지 상태의 글 최근 50건이라 최대 250건입니다. 철회와 되살리기는 확정본 포스터에만 되고(아니면 409
  `invalid_state`), 되살린 글은 다음 변경 감지 주기에 다시 대응합니다. 상태 조건과 409 `code`는 [admin-api.openapi.json](admin-api.openapi.json)의 schema 설명을 따릅니다.

## Contract ID

- `admin.api`

## Provider

- Service: `hololive-api` admin plane (`internal/planes/admin`)

## Consumers

- `iris-console` gateway(`internal/hololive`)와 Android 앱

## Producer verification

- `hololive/hololive-api/internal/planes/admin/runtime/openapi_contract_test.go`가 process 조립과 같은 router로 실제
  handler를 호출합니다. 응답 본문은 그 status의 응답 schema를 지켜야 하고, 요청 본문은 생산자가 받아들였으면
  requestBody schema를 지키고 400으로 거절했으면 schema도 거절해야 합니다.
- operation을 이 파일에 추가할 때는 같은 커밋에서 생산자 원본 응답(정규화 전)으로 이 시험을 넓힙니다.

## Generated copy (iris-console)

console은 고정한 hololive-bot revision의 `docs/current/contracts/admin-api.openapi.json`을 위 범위 operation의
입력으로 `contracts/hololive/generate_test.go` 생성물을 만듭니다. 생산자 경로 `/api/holo/X`는 console 공개 경로
`/admin/api/holo/X`에 대응합니다. 생산자 schema를 바꾸는 변경은 이 파일과 생산자 시험을 같은 커밋에서 고치고,
console은 다음 생성에서 그 revision을 고정합니다.
응답 schema에 `required` 필드를 더한 revision은 hololive-api를 먼저 배포한 뒤 gateway와 Android 앱을 함께 배포합니다.
새 필드를 요구하는 gateway는 옛 hololive-api 응답을 502로 거절합니다.
