# Prompt: dispatcher hot path 개선

## 목표

dispatcher PG consumer hot path에서 불필요한 PG 부하와 sibling cancellation을 제거합니다.

## 대상 파일

- `hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/consumer.go`
- `hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/repository.go`
- `hololive/hololive-dispatcher-go/internal/dispatch/dispatcher.go`
- `hololive/hololive-dispatcher-go/internal/app/runtime.go`

## 요구사항

1. reconciliation throttle.
2. retry/DLQ/quarantine batch update.
3. dispatch group error isolation.
4. drain budget.
5. wakeup fallback 유지.

## 테스트

- recovery interval 안에서는 recovery query 미실행.
- 50개 quarantine이 batch update 1회 또는 소수로 처리.
- group A error가 group B를 cancel하지 않음.
- wakeup timeout 후 PG scan.
