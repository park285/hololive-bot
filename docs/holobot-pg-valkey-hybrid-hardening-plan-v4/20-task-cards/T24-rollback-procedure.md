# T24. Rollback procedure

## 목적

pg_first 전환 중 문제가 생겼을 때 중복 발송을 최소화하며 되돌립니다.

## 원칙

1. publisher와 consumer를 같은 창에서 되돌립니다.
2. PG pending row를 legacy Valkey queue로 자동 replay하지 않습니다.
3. sending/quarantined row는 자동 retry하지 않습니다.
4. manual requeue는 audit + duplicate risk ack 필요.

## 작업

- rollback env 명시.
- PG row 상태별 처리표 추가.
- legacy queue residue 처리 방법 추가.

## LLM 프롬프트

rollback runbook을 작성하십시오. 자동 replay 금지, duplicate risk ack, 상태별 처리표를 포함하십시오.
