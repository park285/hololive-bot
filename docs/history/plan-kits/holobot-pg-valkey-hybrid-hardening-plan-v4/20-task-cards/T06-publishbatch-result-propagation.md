# T06. PublishBatchResult 상위 전달

## 목적

전환 중 inserted/duplicate/hash conflict를 관측할 수 있게 합니다.

## 작업 대상

- `hololive/hololive-shared/pkg/service/alarm/queue/publisher.go`
- `hololive/hololive-alarm-worker/internal/service/alarm/checker/notifier.go`
- metrics package

## 작업

1. `PublishBatch()`가 `PublishBatchResult, error`를 반환하도록 변경합니다.
2. `Publish()`는 batch size 1 wrapper로 result를 반환합니다.
3. notifier는 result를 metric/log에 전달합니다.
4. 기존 caller compatibility를 최소화합니다.

## 완료 기준

- requested/inserted/duplicate/hash conflict count가 metric으로 노출됩니다.
- duplicate가 많아도 성공/실패 판단이 명확합니다.
- wakeup sent/suppressed/failed count도 metric으로 나옵니다.

## LLM 프롬프트

queue publisher API에서 `PublishBatchResult`가 사라지지 않게 상위 notifier까지 전달하십시오. 기존 단일 Publish 경로는 wrapper로 유지하십시오.
