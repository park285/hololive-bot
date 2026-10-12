-- send unit 도입(141) 전에 만든 delivery 행을 지우고 send_unit_id를 NOT NULL로 둔다. worker는 141 이후 모든 행을
-- send unit과 함께 넣고 224 CHECK가 활성 상태의 NULL을 거부하므로, NULL 행은 종료된 legacy 행뿐이다.
-- 2026-10-12 운영 조회: sent 294행(최신 sent_at 2026-08-10), cancelled 14행(최신 cancelled_at 2026-09-21),
-- 이 행을 가리키는 관리자 작업 0건. 90일 보존 삭제(2026-12-20 완료 예정)를 기다리지 않고 지운다.
-- alarm_dispatch_admin_actions.delivery_id는 보존 삭제와 같이 ON DELETE SET NULL이 적용된다.
-- dlq·quarantined NULL 행은 운영자가 처리할 실패 기록이므로 지우지 않고 실패한다. NOT NULL이 224 CHECK의 조건을
-- 포함하므로 그 CHECK는 지운다. 표가 작아(운영 1,294행) 처음부터 ACCESS EXCLUSIVE를 잡고 한 트랜잭션에서
-- NOT VALID CHECK를 VALIDATE한 뒤 SET NOT NULL한다(280 선례). 모든 문장은 멱등이다.
BEGIN;
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '60s';
LOCK TABLE public.alarm_dispatch_deliveries IN ACCESS EXCLUSIVE MODE;
DO $migration$
BEGIN
    IF EXISTS (
        SELECT 1 FROM public.alarm_dispatch_deliveries
        WHERE send_unit_id IS NULL AND status NOT IN ('sent', 'cancelled')
    ) THEN
        RAISE EXCEPTION 'alarm_dispatch_deliveries has unsettled rows without send unit; rows kept';
    END IF;
END
$migration$;
DELETE FROM public.alarm_dispatch_deliveries
WHERE send_unit_id IS NULL AND status IN ('sent', 'cancelled');
ALTER TABLE public.alarm_dispatch_deliveries DROP CONSTRAINT IF EXISTS alarm_dispatch_deliveries_send_unit_id_nn;
ALTER TABLE public.alarm_dispatch_deliveries
    ADD CONSTRAINT alarm_dispatch_deliveries_send_unit_id_nn CHECK (send_unit_id IS NOT NULL) NOT VALID;
ALTER TABLE public.alarm_dispatch_deliveries VALIDATE CONSTRAINT alarm_dispatch_deliveries_send_unit_id_nn;
ALTER TABLE public.alarm_dispatch_deliveries ALTER COLUMN send_unit_id SET NOT NULL;
ALTER TABLE public.alarm_dispatch_deliveries DROP CONSTRAINT IF EXISTS alarm_dispatch_deliveries_send_unit_id_nn;
ALTER TABLE public.alarm_dispatch_deliveries DROP CONSTRAINT IF EXISTS alarm_dispatch_deliveries_active_send_unit_check;
COMMIT;
