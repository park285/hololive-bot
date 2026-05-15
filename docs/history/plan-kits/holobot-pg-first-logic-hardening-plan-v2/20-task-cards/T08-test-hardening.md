# T08. Test Hardening

## 목표

계약 유지와 로직 개선을 test로 보증합니다.

## PATH

```text
hololive/hololive-alarm-worker/internal/app/*_test.go
hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/*_test.go
hololive/hololive-dispatcher-go/internal/app/*_test.go
```

## 필수 테스트

- post-send quarantine.
- pre-send retry.
- mark-sent failure no retry.
- PG idle waiter.
- wakeup fallback.
- max batches per wake.
- consumer config parity.
- retention cleanup.
- mode pair validation.
- duplicate dedupe key.
- stale leased recovery.
- stale sending quarantine.

## 완료 기준

테스트 이름이 failure 시점을 드러내야 합니다.

좋은 예:

```text
TestAlarmDispatchRunnerQuarantinesPGSendFailureAfterMarkSending
TestAlarmDispatchRunnerRetriesRenderFailureBeforeMarkSending
TestPGIdleWaiterFallsBackToPollingWhenWakeupUnavailable
```
