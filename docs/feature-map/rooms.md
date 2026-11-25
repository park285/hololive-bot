# 방 접근 제어와 알림 설정

관리자는 참여 방과 명령을 허용할 방을 구별하고 이름과 사전 알림 시간을 설정한다.

## 하위 기능

가입 방 표시, 화이트리스트·블랙리스트와 ACL 활성화, 방 표시 이름, 설정 조회·수정과 worker 적용.

## 사용자 경로

- `/api/holo/rooms`에서 `GET`, `POST`, `DELETE`; `GET /api/holo/rooms/joined`는 Iris 가입 방, `POST /api/holo/rooms/acl`은 정책 전환이다. `GET /api/holo/rooms/delivery-pauses`는 봇 계정이 없어 발송을 멈춘 방 목록이고 `DELETE /api/holo/rooms/delivery-pauses/{roomId}`는 그 방의 발송을 재개한다. `POST /api/holo/names/room`은 방 이름을 저장한다.
- `GET`·`POST /api/holo/settings`로 `alarmAdvanceMinutes`를 읽고 고친다. 내부 worker 연결은 `PUT /internal/alarm/settings`, `PUT /internal/alarm/room-name`. 경로·역할은 `internal/planes/admin/internal/httpapi/routes.go`와 worker의 `internal/service/alarm/httpapi/handler.go`에서 대조한다.

## hololive-runtime.sh로 구동

```bash
p=$(scripts/verify/hololive-runtime.sh run --request GET /api/holo/rooms 200 '' --request GET /api/holo/settings 200 '')
jq '{rooms,aclEnabled,aclMode}' "$p/extra-1.json"
jq '.settings' "$p/extra-2.json"
```

합성 채팅 ID `930003`으로 ACL을 초기화한 개발 인스턴스다. 목록과 설정을 서로 다른 응답에서 읽는다. 런타임 기본 여정이 가입 방 가져오기나 접근 차단을 검증하지는 않는다.

## 함정

ACL 식별자는 보이는 방 이름이 아니라 정규화된 signed 64-bit 채팅 ID다. 가입 방과 허용 방은 동일 집합이 아니다. 파일 저장 성공만으로 worker 설정 적용을 판정하지 않는다. 실제 방 이름·가입 목록을 증거에 넣지 않는다.
