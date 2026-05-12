# T09. Set-based delivery insert

## 목적

delivery insert가 delivery 수만큼 SQL statement를 실행하지 않게 합니다.

## 작업 대상

- `dispatchoutbox/repository.go`
- `30-sql/002-set-based-delivery-insert.sql`
- integration tests

## 작업

1. delivery input arrays를 set으로 만듭니다.
2. event_key로 event_id를 join합니다.
3. `ON CONFLICT (dedupe_key) DO NOTHING`을 유지합니다.
4. inserted/duplicate count를 반환합니다.
5. optional bounded classification query로 terminal/shadow duplicate를 분류합니다.

## 완료 기준

- 1,000 deliveries insert가 1,000 SQL statements를 실행하지 않습니다.
- duplicate delivery는 skip됩니다.
- terminal row는 pending으로 돌아가지 않습니다.
- shadow row는 자동 promotion되지 않습니다.

## LLM 프롬프트

delivery insert를 set-based로 바꾸십시오. correctness source는 `UNIQUE(dedupe_key)`입니다. duplicate를 update하지 마십시오.
