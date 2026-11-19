# 방별 알람 구독과 해제

방에서 원하는 멤버와 알림 종류를 선택한다. 추가와 삭제는 다른 방의 구독을 바꾸지 않아야 한다.

## 하위 기능

방별 조회·추가·일부 종류 제거·전체 초기화, 공유 채널 내 대상 구분, DB 저장 후 캐시 반영, 관리자 구독 조회·삭제.

## 사용자 경로

- `!알람 추가 미코 방송`, `!알람 목록`, `!알람 제거 미코 방송`, `!알람 초기화`. 접두 명령 `알림`·`alarm`과 붙여 쓰는 `알람/알림` + `설정`, `추가`, `목록`, `리스트`, `제거`, `삭제`, `초기화`, `리셋`, `해제`를 인식한다. 하위 동사 `설정/set/add`, `삭제/remove/del/delete`, `list/show`, `clear/reset`도 있다. 종류는 방송/라이브/live, 커뮤니티/community, 쇼츠/shorts, 전체/all.
- 관리자 `GET`·`DELETE /api/holo/alarms`. API가 worker의 `/internal/alarm` 아래 `POST /add`, `POST /remove`, `GET /room/:id/view`, `POST /clear`, `GET /keys`, `GET /count`를 사용한다. 이름·알림 시점은 [설정](rooms.md)을 본다.

## hololive-runtime.sh로 구동

```bash
p=$(scripts/verify/hololive-runtime.sh run)
cat "$p"/{before.json,add.request,add.json,view.json,persisted,remove.json,after.json,removed,cleanup,exit-code}
```

`doctor-auth.status`는 키 없는 조회 401, `ready.json`은 키 있는 조회 0건이다. `c3-room`에 `c3-channel/LIVE` 한 건을 등록해 `view`와 `public.alarms`에서 각각 확인한다. 제거 후 목록의 근거가 되는 전체 개수와 별도 SQL 재조회가 모두 0이어야 한다.

## 함정

`added:true`만으로 저장을 판정하지 않는다. 음성 대조에서는 저장소 사본에서 `persistAddAlarmMutation`의 실제 쓰기만 생략했더니 등록 응답은 여전히 참이었지만 방 조회 응답은 `{"success":true}`여서 종료 1이었다. worker의 합성 방 ID로 검증했으며 채팅 명령·웹훅 인증, 다른 종류 조합, 실제 알림 송신은 이 실행에 포함되지 않는다.

```text
view: expected .success == true and (.data | length) == 1 and .data[0].ChannelID == "c3-channel" and .data[0].AlarmTypes == ["LIVE"], got {"success":true}
```
