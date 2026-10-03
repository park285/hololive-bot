# internal/app

`hololive-api` bot plane의 bootstrap 및 HTTP 구성 helper입니다.

- `bootstrap/`은 provider·service·서버 구성을 소유합니다.
- `http/`는 router·middleware·route 및 shortlink handler를 소유합니다.
- 시작·종료와 HTTP server helper는 bot plane의 `runtime/`이 소유합니다.
- 얇은 중복 wrapper와 그것만 검증하는 시험을 추가하지 않습니다.

현재 경계는 저장소의 `docs/current/architecture/app-bootstrap-boundary-guide.md`를 따릅니다.
