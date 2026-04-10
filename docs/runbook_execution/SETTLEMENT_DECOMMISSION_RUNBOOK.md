# Settlement Decommission Runbook

## 목적

운영 DB에서 settlement 잔재 테이블을 안전하게 제거합니다.

## 원칙

- 이 절차는 수동 실행입니다.
- 일반 bootstrap 또는 deploy 파이프라인에 포함하지 않습니다.
- 먼저 export 또는 backup 후 drop 합니다.

## 절차

1. 대상 DB에서 settlement 관련 테이블 row count를 확인합니다.
2. 필요하면 CSV 또는 table-level `pg_dump` export를 수행합니다.
3. maintenance window를 확보합니다.
4. `hololive/hololive-kakao-bot-go/scripts/migrations/manual/settlement_drop.sql`을 수동 실행합니다.
5. drop 이후 schema 검증 SQL을 실행합니다.
6. release note와 운영 로그에 decommission 완료를 기록합니다.

## 검증 SQL

```sql
SELECT to_regclass('public.settlement_members');
SELECT to_regclass('public.settlement_cycles');
SELECT to_regclass('public.settlement_payments');
SELECT to_regclass('public.settlement_room_configs');
SELECT to_regclass('public.settlement_member_terms');
SELECT to_regclass('public.settlement_cycles_v2');
SELECT to_regclass('public.settlement_payments_v2');
SELECT to_regclass('public.settlement_payment_events_v2');
```
