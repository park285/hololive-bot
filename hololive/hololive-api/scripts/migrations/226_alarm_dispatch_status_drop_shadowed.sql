-- v3 handoff(off/shadow/cutover)를 DEC-20260926-hololive-outbox-v3-convergence로 삭제해 비교 전용 shadowed 행을 기록하는
-- 경로(PublishShadowDispatchBatch)가 없다. 2026-09-26 T18에서 운영 alarm_dispatch_deliveries의 shadowed 행 0건을 확인했으므로
-- 상태 CHECK에서 shadowed를 뺀다. 제약 삭제와 NOT VALID 재생성은 한 트랜잭션으로 묶어 CHECK가 없는 창을 두지 않고,
-- 재실행하면 같은 이름의 제약을 다시 만든다.
-- VALIDATE가 실패하면 shadowed 행이 남아 있다는 뜻이다. 행을 조사해 정리한 뒤 다시 적용한다(pending으로 승격하지 않는다).
-- 번호는 225 다음이다(PLN-20260926-stack-audit-refactoring T19, live-evidence 218~220과 부재 증거 보존 221 뒤로 재번호).
BEGIN;

ALTER TABLE alarm_dispatch_deliveries
    DROP CONSTRAINT IF EXISTS alarm_dispatch_deliveries_status_check;

ALTER TABLE alarm_dispatch_deliveries
    ADD CONSTRAINT alarm_dispatch_deliveries_status_check
    CHECK (status = ANY (ARRAY['pending'::text, 'retry'::text, 'leased'::text, 'sending'::text, 'sent'::text, 'dlq'::text, 'quarantined'::text, 'cancelled'::text]))
    NOT VALID;

COMMIT;

ALTER TABLE alarm_dispatch_deliveries
    VALIDATE CONSTRAINT alarm_dispatch_deliveries_status_check;
