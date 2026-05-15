# Glossary

## event

room과 무관한 logical alarm payload입니다. 예: 특정 stream의 10분 전 알림.

## delivery

event를 특정 room으로 보내는 상태 row입니다.

## wakeup

PG에 처리할 row가 생겼음을 dispatcher에게 알려주는 payload 없는 Valkey token입니다.

## shadowed

legacy Valkey queue 발행 후 PG에 관측용으로 남긴 row입니다. PG consumer claim 대상이 아닙니다.

## leased

dispatcher가 PG row를 claim했지만 external send를 시작하지 않은 상태입니다.

## sending

external Iris send 직전/중/직후 상태입니다. 결과 불명 가능성이 있으므로 Iris idempotency 전에는 stale sending을 quarantine합니다.

## quarantine

중복 위험 때문에 자동 retry하지 않는 보류 상태입니다.
