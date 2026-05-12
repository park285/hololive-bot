# T10. Publisher chunk semantics 정리

## 목적

batch chunk partial success가 dedup claim release와 metric을 망가뜨리지 않게 합니다.

## 현재 위험

publisher가 여러 chunk를 순서대로 insert하다가 후반 chunk에서 실패하면, 앞 chunk는 이미 PG pending으로 commit됐을 수 있습니다. 그런데 상위 notifier가 전체 batch 실패처럼 claim을 모두 release할 수 있습니다.

## 작업

1. notifier에서 prepared notifications를 chunk로 나눕니다.
2. chunk 단위로 `PublishBatch()`를 호출합니다.
3. 성공한 chunk는 markPublished 처리합니다.
4. 실패한 chunk만 claim release합니다.
5. result를 chunk별로 aggregate합니다.

## 완료 기준

- chunk 1 성공, chunk 2 실패 상황에서 chunk 1 claim을 release하지 않습니다.
- inserted row가 있는데 caller가 전체 failed로 집계하지 않습니다.
- chunk별 metric이 정확합니다.

## LLM 프롬프트

publisher chunk partial success를 안전하게 처리하십시오. 이미 PG에 들어간 delivery의 claim을 실패 chunk와 함께 release하지 마십시오.
