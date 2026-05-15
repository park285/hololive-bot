# T04. 동일 batch 내부 event hash conflict 검출

## 목적

같은 batch 안에 동일 `event_key`와 다른 `payload_hash`가 들어오는 버그를 조용히 지나치지 않게 합니다.

## 현재 위험

`InsertBatch()`가 map으로 event를 dedupe할 때 기존 key가 있으면 비교하지 않고 skip할 수 있습니다. 그러면 같은 batch 내부 payload mismatch를 DB conflict 전에 놓칠 수 있습니다.

## 작업 대상

- `hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/repository.go`
- repository unit/integration tests

## 작업

1. events map에 같은 event_key가 이미 있으면 payload_hash를 비교합니다.
2. 다르면 `HashConflictEvents`를 증가시키고 error를 반환합니다.
3. error message에는 event_key만 넣고 payload 원문은 로그에 남기지 않습니다.

## 완료 기준

- 같은 batch 안의 conflict 테스트가 실패/에러를 확인합니다.
- 같은 event_key + 같은 hash는 정상 dedupe됩니다.
- DB 기존 row와의 hash conflict도 계속 잡힙니다.

## LLM 프롬프트

`InsertBatch()` 입력 전처리에서 동일 event_key/hash conflict를 검출하십시오. payload 원문은 로그/에러에 출력하지 마십시오.
