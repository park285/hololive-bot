# 멤버 찾기와 등록 정보 관리

관리자가 등록한 이름·채널·별칭은 멤버 검색과 프로필의 기준이 된다.

## 하위 기능

기본 정보 등록·조회, 한/일 별칭 추가·삭제, 개명·채널 변경·졸업 표시, 프로필 출력과 사진 동기화. 카드 디자인은 [주간 일정표](posters.md)에서 다룬다.

## 사용자 경로

- `!프로필 미코` (`profile`, `정보`, `info`); 인자 없는 `!멤버`, `!member`도 이 경로다. 이름이 붙은 `!멤버 미코`는 [일정 명령](streams.md)이 앞선다.
- `/api/holo` 아래 `GET /members`, `POST /members`, `POST`·`DELETE /members/:id/aliases`, `PATCH /members/:id/graduation`, `/channel`, `/name`(모두 같은 멤버 ID 접두사).
- 관리자 photo sync가 켜진 경우 공급자에서 사진을 동기화한다. 조회·수정 등록은 `internal/planes/admin/internal/httpapi/routes.go`가 소유한다.

## hololive-runtime.sh로 구동

```bash
p=$(scripts/verify/hololive-runtime.sh run --request POST /api/holo/members 201 '{"name":"C3 Member","channelId":"UC_c3"}' --request GET /api/holo/members 200 '')
jq -e '.members[] | select(.name == "C3 Member" and .channelId == "UC_c3")' "$p/extra-2.json"
```

DB는 매번 새로 만든다. 등록 응답만 보지 말고 목록에서 새 이름을 찾는다.

## 함정

공유 채널을 쓰는 두 멤버는 동일인이 아니다. 사진 동기화와 Kakao 프로필 수신은 드라이버에서 확인하지 않는다. 실제 데이터가 담긴 Console에 검증용 멤버를 추가하지 않는다.
