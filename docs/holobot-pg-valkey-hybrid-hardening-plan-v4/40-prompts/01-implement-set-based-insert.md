# Prompt: set-based InsertBatch 구현

다음 작업을 수행하십시오.

## 목표

`dispatchoutbox.PgxRepository.InsertBatch()`가 event/delivery row별 SQL statement를 실행하지 않게 set-based insert로 변경합니다.

## 대상 파일

- `hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/repository.go`
- `hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/model.go`
- repository tests

## 참고 SQL

- `30-sql/001-set-based-event-insert.sql`
- `30-sql/002-set-based-delivery-insert.sql`

## 요구사항

1. 같은 batch 내부 event_key/hash conflict를 먼저 검출합니다.
2. DB 기존 event_key/hash conflict도 검출합니다.
3. event insert는 chunk당 set-based입니다.
4. delivery insert는 chunk당 set-based입니다.
5. duplicate delivery는 update하지 않고 skip합니다.
6. result count를 정확히 반환합니다.
7. payload 원문은 error/log에 출력하지 않습니다.

## 테스트

- 1 event + 1,000 deliveries.
- duplicate delivery.
- same event key/same hash.
- same event key/different hash in same batch.
- existing event key/different hash in DB.
