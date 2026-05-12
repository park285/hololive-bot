# T18. Bounded retention jobs

## 목적

오래된 terminal delivery와 orphan event를 안전하게 정리합니다.

## 작업

1. sent/dlq/quarantined/cancelled row를 limit 단위로 delete합니다.
2. delivery가 없는 event를 limit 단위로 delete합니다.
3. job은 반복 실행 가능해야 합니다.
4. 한 번에 전체 table을 지우는 SQL을 금지합니다.

## 완료 기준

- DELETE 쿼리에 반드시 LIMIT CTE가 있습니다.
- runbook에 batch size와 반복 방법이 있습니다.
- metric/log에 deleted rows가 남습니다.

## LLM 프롬프트

retention job을 chunked CTE 방식으로 구현하십시오. unbounded DELETE/UPDATE는 금지입니다.
