# T17. Retention index refinement

## 목적

terminal row cleanup이 bounded index scan으로 동작하게 합니다.

## 작업

1. sent retention partial index 추가.
2. dlq retention partial index 추가.
3. quarantined retention partial index 추가.
4. orphan event cleanup용 created_at index 추가.
5. `(status, created_at)` 인덱스 필요성을 재검토합니다.

## 완료 기준

- retention query가 terminal timestamp index를 사용합니다.
- write-heavy insert/update 비용이 과도하지 않습니다.
- index 목적이 migration comment/runbook에 명확합니다.

## LLM 프롬프트

retention query에 필요한 partial index를 추가하십시오. 운영 조회용 인덱스는 실제 사용처가 없으면 제거 또는 문서화하십시오.
