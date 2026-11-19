# 예약 알림 발송과 실패 정산

사용자는 방송 전·시작·일정 변경, 새 커뮤니티 글·쇼츠 알림을 받고, 운영자는 보내지 못한 건의 근거와 조치를 조회한다.

## 하위 기능

주기적 방송 검사와 구독 방 선정, 예정 후보 저장·만료, LIVE catchup, 방별 묶음/개별 발행, 선점과 발송, 재시도·격리·종단 상태, handoff 확인, 실패 분류와 운영자 복구. 축하와 X 스페이스는 별도 입력 경로다.

## 사용자 경로

[`alarm-worker`](../current/services/alarm-worker.md)는 발송 대기열을 소비해 Iris에 보낸다. YouTube 알림 outbox와 일반 알람 dispatch는 별도 원장이다. Console → `/api/holo/dispatch` 아래 `GET /summary`, `/failures`, `/deliveries`, `/deliveries/:id`, `/deliveries/:id/actions`; `POST /deliveries/:id/requeue`와 `/settle`은 감사 이력에 남는 상태 변경이다.

## hololive-runtime.sh로 구동

```bash
p=$(scripts/verify/hololive-runtime.sh run --request GET /api/holo/dispatch/summary 200 '')
cat "$p/extra-1.json"
```

새 DB에서 관리자 집계의 실제 HTTP 응답만 읽는다. 합성 알림·실패 receipt를 만들지 않으므로 발송·재대기·정산의 성공 증거로 사용하지 않는다. 경합은 [AlarmDispatchClaim의 코드 대응과 반례 상태도](../../verification/tla/AlarmDispatchClaim/COVERAGE.md#결함-경로-상태도)를 별도로 본다.

## 함정

선점은 발송이 아니고 202는 방 도착이 아니다. 불명 상태에 재발행을 반복하면 안 된다. 모델의 두 소비자 반례와 단일 worker 배포 조건을 구분한다. 이 모델은 YouTube의 별도 원장·handoff 전체를 검증하지 않는다.
