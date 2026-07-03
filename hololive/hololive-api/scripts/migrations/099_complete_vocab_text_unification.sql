-- 099_complete_vocab_text_unification.sql
-- 098이 보류한 잔여 어휘 컬럼의 TEXT 전환 완결(CONVENTIONS.md ① — 값이 계약, 폭 상한 제거).
-- varchar→TEXT는 테이블 무-rewrite지만 해당 컬럼이 걸린 partial index들이 ACCESS EXCLUSIVE로
-- 재빌드된다 — 배포 창에서 적용된다는 전제(핫테이블 status 인덱스 11종).

ALTER TABLE members ALTER COLUMN status TYPE TEXT;
ALTER TABLE notification_delivery_outbox ALTER COLUMN status TYPE TEXT;
ALTER TABLE youtube_notification_outbox ALTER COLUMN status TYPE TEXT;
ALTER TABLE youtube_notification_delivery ALTER COLUMN status TYPE TEXT;
ALTER TABLE youtube_live_sessions ALTER COLUMN status TYPE TEXT;
ALTER TABLE youtube_content_alarm_tracking ALTER COLUMN delivery_status TYPE TEXT;
ALTER TABLE youtube_community_shorts_alarm_states ALTER COLUMN delivery_status TYPE TEXT;
ALTER TABLE youtube_notification_delivery_telemetry ALTER COLUMN alarm_type TYPE TEXT;
