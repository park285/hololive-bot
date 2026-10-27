# Contract: admin.api

## Summary

hololive-api admin plane의 `/api/holo/*` 관리 HTTP API 요청·응답 schema는 생산자인 hololive-api가 소유합니다.
정본은 [admin-api.openapi.json](admin-api.openapi.json)(OpenAPI 3.1)입니다. iris-console은 이 파일을 고정한
hololive-bot revision에서 가져와 gateway 검증용 생성 사본을 만들고, 자기 공개 경로(`/admin/api/holo/*`)와
gateway 고유 header·오류 envelope만 소유합니다.

- 인증: 모든 operation은 `X-API-Key` header를 요구합니다(없으면 401, 틀리면 403).
- 오류: 성공이 아닌 응답은 `{"error": "<message>"}`이고, 호출자가 분기해야 하는 거절은 안정 `code`를 함께 둡니다
  (예: 방 변경의 `409 acl_mode_changed`). 생산자 오류 문구는 계약이 아닙니다.
- 목록 응답(`rooms`·`alarms`·`members`)은 페이지 없이 전체 목록입니다. 생산자는 응답 크기 상한을 두지 않으며,
  표시·본문 상한은 소비자가 소유합니다.
- 방 변경(`POST/DELETE /api/holo/rooms`)은 필수 `mode`를 받고, 이미 원하는 상태면 `200 changed:false`입니다.
  상세 의미는 [settings 계약](settings.md)을 따릅니다.

## Contract ID

- `admin.api`

## Provider

- Service: `hololive-api` admin plane (`internal/planes/admin`)

## Consumers

- `iris-console` gateway(`internal/hololive`)와 Android 앱

## Producer verification

- `hololive/hololive-api/internal/planes/admin/runtime/openapi_contract_test.go`가 process 조립과 같은 router로 실제
  handler 응답을 이 schema로 검증합니다. 검증한 operation은 spec에 `x-producer-verified: true`로 표시합니다.
- 표시가 없는 operation의 schema는 iris-console `contracts/hololive/openapi.json`(revision `7c78e05`)의 응답 schema를
  옮긴 것입니다. console gateway는 응답을 정규화(member ID 문자열화, 빈 선택 필드 삭제 등)한 뒤 그 schema로 검증하므로,
  생산자 원본 응답과 다를 수 있습니다. 해당 operation의 생산자 검증 시험을 추가할 때 schema를 생산자 원본에 맞춥니다.

## Generated copy (iris-console)

console은 고정한 hololive-bot revision의 `docs/current/contracts/admin-api.openapi.json`을 입력으로
`contracts/hololive/generate_test.go` 생성물을 만듭니다. 생산자 경로 `/api/holo/X`는 console 공개 경로
`/admin/api/holo/X`에 대응합니다. 생산자 schema를 바꾸는 변경은 이 파일과 생산자 시험을 같은 커밋에서 고치고,
console은 다음 생성에서 그 revision을 고정합니다.
