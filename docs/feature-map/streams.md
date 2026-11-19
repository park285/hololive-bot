# 지금 하는 방송과 다음 일정 찾기

방송 중인 채널을 찾거나 특정 멤버가 언제 방송할지 확인한다.

## 하위 기능

라이브·예정 목록, 멤버 필터, 전체·표시 개수, 멤버별 기간 일정, 관리자 조직별 목록을 구분한다. 일정 조회와 [이미지 포스터](posters.md)는 다른 경로다.

## 사용자 경로

- `!라이브` (`live`, `방송중`, `생방송`), `!예정` (`upcoming`)에 멤버 이름을 줄 수 있다. `!예정 전체`와 숫자 제한은 `message_parser_stream.go`에서 해석한다.
- `!일정 미코 7`, `!스케줄 미코`, `!schedule 미코`, `!멤버 미코`, `!member 미코`: 기본 7일, 지정값은 1~30일. 인자 없는 `!멤버`는 프로필 조회로 넘어간다.
- Console → H3 `GET /api/holo/streams/live`, `GET /api/holo/streams/upcoming` (`org` 선택). 관리자 API와 bot의 데이터 조합·표시는 같지 않을 수 있다.

## Go 핸들러 테스트로 구동

```bash
go test -p 2 -parallel 2 ./hololive/hololive-api/internal/planes/bot/internal/command/handlers -run '^Test(LiveCommand_Execute_AllLiveSendsText|ScheduleCommand_Execute_GoldenPath)$'
go test -p 2 -parallel 2 ./hololive/hololive-api/internal/planes/admin/internal/server/api -run '^TestGetStreamsOrgAllPartialFailureRespondsInternalErrorWithoutPartialBody$'
```

첫 명령은 주입된 방송·일정으로 메시지 내용을 확인한다. 두 번째는 조직 전체 조회에서 일부 실패를 정상적인 전체 목록으로 돌려주지 않는지 본다.

## 함정

런타임에는 Holodex·공식 일정 응답 데이터가 없다. 실제 방송이 없다는 결론을 빈 응답이나 네트워크 오류에서 도출하지 않는다. [수집 리스 모델](../../verification/tla/CollectionJobLease/COVERAGE.md#상태도)은 슬롯 소유권만 다루며 표시된 시간의 정확성은 보장하지 않는다.
