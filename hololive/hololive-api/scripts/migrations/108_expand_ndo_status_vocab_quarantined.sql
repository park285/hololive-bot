-- 108_expand_ndo_status_vocab_quarantined.sql
-- stale-SENDING 회수(결과 불명)를 확정 실패(FAILED)와 분리하기 위해 QUARANTINED를
-- notification_delivery_outbox status 어휘에 추가한다. rearm(enqueue upsert의
-- WHERE status='FAILED')은 FAILED 전용으로 유지되어 QUARANTINED는 재발송되지 않는다.

ALTER TABLE notification_delivery_outbox
    DROP CONSTRAINT IF EXISTS chk_notification_delivery_outbox_status_vocab;
ALTER TABLE notification_delivery_outbox
    ADD CONSTRAINT chk_notification_delivery_outbox_status_vocab
    CHECK (status IN ('PENDING', 'SENDING', 'SENT', 'FAILED', 'QUARANTINED')) NOT VALID;
ALTER TABLE notification_delivery_outbox
    VALIDATE CONSTRAINT chk_notification_delivery_outbox_status_vocab;
