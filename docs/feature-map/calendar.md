# 기념일 달력과 축하 알림

월별 생일·데뷔 기념일을 달력으로 보고 해당 날짜에 구독 방에서 축하 알림을 받을 수 있다.

## 하위 기능

월 이동·연도 경계, 이미지 달력과 텍스트 대체, 멤버의 생일/데뷔일 조회, 지정 KST 시각의 worker 축하 작업.

## 사용자 경로

`!달력`, `!기념일`, `!calendar`에 `다음달`, `저번달` 또는 1~12를 붙인다. Console은 `GET /api/holo/members/calendar?year=2028&month=2`를 호출한다. `hololive-alarm-worker/internal/service/celebration`의 예약 작업은 채팅 명령 없이 알림을 만든다.

## hololive-runtime.sh로 구동

```bash
p=$(scripts/verify/hololive-runtime.sh run --request GET '/api/holo/members/calendar?year=2028&month=2' 200 '')
jq -e '.year == 2028 and .month == 2 and (.entries | type) == "array"' "$p/extra-1.json"
```

새 DB의 멤버로 달력 한 달을 읽는다. 해당 월의 정확한 개수를 오늘 날짜에서 추정하지 않는다.

## 함정

관리 API가 달력 행을 반환한 것과 특정 KST 시각에 방으로 발송된 것은 별개다. 기본 검증은 축하 runner를 켜지 않는다. 생성·정산·도착까지 확인하려면 해당 날짜의 대상과 발송 영수증 대역이 필요하다.
