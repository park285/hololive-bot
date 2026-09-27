-- 활성 delivery(pending·retry·leased·sending)는 저장된 send unit이 있어야 한다. migration 141 이전 행(send_unit_id NULL)을
-- 먼저 claim하던 legacy_head를 지운 뒤(PLN-20260926-stack-audit-refactoring T17) 이런 행은 claim되지 않아 조용히 멈추므로,
-- 쓰기 시점에 거절해 위반을 드러낸다. 2026-09-26 T18에서 send_unit_id NULL 행 437건이 모두 종단(sent 423, cancelled 14)이고
-- 활성 0건임을 확인했다. 종단 행과 send unit 없이 기록하는 shadowed 행은 대상이 아니므로 retention 소거를 기다리지 않는다.
-- VALIDATE가 실패하면 활성 NULL 행이 남아 있다는 뜻이다. 행을 조사해 종단으로 정리한 뒤 다시 적용한다.
-- 전체 NOT NULL 승격은 종단 NULL 행이 retention으로 소멸하고 shadowed 기록 방식이 정리된 뒤 따로 검토한다.
-- 번호는 운영 적용된 live-evidence 218~220과 부재 증거 보존 221 다음(222·223 뒤)이다.
DO $migration$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'alarm_dispatch_deliveries_active_send_unit_check'
          AND conrelid = 'alarm_dispatch_deliveries'::regclass
    ) THEN
        ALTER TABLE alarm_dispatch_deliveries
            ADD CONSTRAINT alarm_dispatch_deliveries_active_send_unit_check
            CHECK (send_unit_id IS NOT NULL OR status NOT IN ('pending', 'retry', 'leased', 'sending'))
            NOT VALID;
    END IF;
END
$migration$;

ALTER TABLE alarm_dispatch_deliveries
    VALIDATE CONSTRAINT alarm_dispatch_deliveries_active_send_unit_check;
