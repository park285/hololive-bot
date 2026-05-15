# T05. Retention Maintenance

## 목표

terminal delivery와 orphan event cleanup을 운영 자동화합니다.

## PATH

```text
hololive/hololive-alarm-worker/internal/app/alarm_dispatch_maintenance.go
hololive/hololive-alarm-worker/internal/app/build_egress.go
docker-compose.prod.yml
docs/current/runbooks/alarm-dispatch-pg-outbox-cutover.md
```

## 변경 내용

- PG advisory lock 기반 single runner.
- status별 chunk delete.
- orphan event cleanup.
- query timeout.
- metrics/logging.

## Safety

- active status 삭제 금지.
- limit max 10000.
- retention disabled option 제공.
- manual script 유지.

## 테스트

- advisory lock fail.
- sent cleanup.
- quarantined cleanup.
- pending not deleted.
- orphan event cleanup.
