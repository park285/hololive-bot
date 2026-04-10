BEGIN;

DROP TABLE IF EXISTS settlement_payment_events_v2 CASCADE;
DROP TABLE IF EXISTS settlement_payments_v2 CASCADE;
DROP TABLE IF EXISTS settlement_cycles_v2 CASCADE;
DROP TABLE IF EXISTS settlement_member_terms CASCADE;
DROP TABLE IF EXISTS settlement_room_configs CASCADE;

DROP TABLE IF EXISTS settlement_payments CASCADE;
DROP TABLE IF EXISTS settlement_cycles CASCADE;
DROP TABLE IF EXISTS settlement_members CASCADE;

COMMIT;
