-- 112_major_events_status_not_null.sql
-- 022:70이 status NULL을 허용하고 098의 vocab CHECK는 NULL을 통과시킨다(SQL 3-valued logic).
-- 백필 → CHECK NOT VALID → VALIDATE → SET NOT NULL → CHECK DROP 레시피(CONVENTIONS.md).

UPDATE major_events
SET status = 'active'
WHERE status IS NULL;

ALTER TABLE major_events
    DROP CONSTRAINT IF EXISTS chk_major_events_status_nn;
ALTER TABLE major_events
    ADD CONSTRAINT chk_major_events_status_nn
    CHECK (status IS NOT NULL) NOT VALID;
ALTER TABLE major_events
    VALIDATE CONSTRAINT chk_major_events_status_nn;
ALTER TABLE major_events
    ALTER COLUMN status SET NOT NULL;
ALTER TABLE major_events
    DROP CONSTRAINT IF EXISTS chk_major_events_status_nn;
