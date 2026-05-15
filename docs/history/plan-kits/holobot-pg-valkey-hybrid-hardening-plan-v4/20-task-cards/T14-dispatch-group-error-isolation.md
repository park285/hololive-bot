# T14. Dispatch group error isolation

## 목적

한 room group 오류가 같은 batch의 다른 group 처리를 취소하지 않게 합니다.

## 현재 위험

`errgroup.WithContext`는 한 goroutine이 error를 반환하면 sibling context를 cancel합니다. room group은 독립이므로 sibling cancel이 중복/미완료 상태를 늘릴 수 있습니다.

## 작업 대상

- `hololive/hololive-dispatcher-go/internal/dispatch/dispatcher.go`
- dispatch tests

## 작업

1. parent ctx cancellation은 존중합니다.
2. group별 persistence/send error는 수집만 하고 sibling을 cancel하지 않습니다.
3. 모든 group이 종료된 뒤 error를 aggregate해서 반환합니다.
4. 이미 external send 중인 group의 MarkSent/Quarantine 흐름을 방해하지 않습니다.

## 완료 기준

- group A MarkSent 실패가 group B send/mark를 취소하지 않습니다.
- parent ctx cancel 시에는 전체 중단됩니다.
- 테스트가 sibling isolation을 확인합니다.

## LLM 프롬프트

`dispatchGroups()`를 독립 group 처리 구조로 바꾸십시오. 한 group의 error가 다른 group context를 취소하지 않게 하되, parent context cancellation은 유지하십시오.
