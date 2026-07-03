-- apply-all.sh는 파일 단위 psql autocommit이라 파일 중간 실패 시 파일 전체가 재적용된다 → 모든 문장은 멱등이다.
-- 스키마 타입 철학: 내부 생성 어휘는 CHECK로 값 집합을 못박는다. TEXT 전환은 kind 전 컬럼+major_events.status까지만이고
-- 잔여 status/delivery_status/alarm_type은 varchar(20)+CHECK로 남긴다(전면 TEXT는 099 후속 — status partial index 재빌드 동반).
-- VALIDATE는 프로덕션에 구세대 값이 잔존하면 실패한다 — 의도된 fail-loud이므로 데이터 정리 후 파일을 재적용한다(dbtest fresh에서는 전량 통과).
ALTER TABLE alarms ALTER COLUMN member_name TYPE TEXT;
ALTER TABLE youtube_stats_changes ALTER COLUMN member_name TYPE TEXT;
ALTER TABLE youtube_stats_history ALTER COLUMN member_name TYPE TEXT;

ALTER TABLE notification_delivery_outbox ALTER COLUMN kind TYPE TEXT;
ALTER TABLE youtube_notification_outbox ALTER COLUMN kind TYPE TEXT;
ALTER TABLE youtube_content_alarm_tracking ALTER COLUMN kind TYPE TEXT;
ALTER TABLE youtube_community_shorts_alarm_states ALTER COLUMN kind TYPE TEXT;
ALTER TABLE youtube_community_shorts_source_posts ALTER COLUMN kind TYPE TEXT;

ALTER TABLE youtube_notification_delivery_telemetry ALTER COLUMN dedupe_key TYPE TEXT;

ALTER TABLE major_events ALTER COLUMN status TYPE TEXT;

ALTER TABLE alarms ALTER COLUMN room_name TYPE VARCHAR(255);

ALTER TABLE notification_delivery_outbox
    DROP CONSTRAINT IF EXISTS chk_notification_delivery_outbox_status_vocab;
ALTER TABLE notification_delivery_outbox
    ADD CONSTRAINT chk_notification_delivery_outbox_status_vocab
    CHECK (status IN ('PENDING', 'SENDING', 'SENT', 'FAILED')) NOT VALID;
ALTER TABLE notification_delivery_outbox
    VALIDATE CONSTRAINT chk_notification_delivery_outbox_status_vocab;

ALTER TABLE notification_delivery_outbox
    DROP CONSTRAINT IF EXISTS chk_notification_delivery_outbox_kind_vocab;
ALTER TABLE notification_delivery_outbox
    ADD CONSTRAINT chk_notification_delivery_outbox_kind_vocab
    CHECK (kind IN ('MAJOR_EVENT_WEEKLY', 'MAJOR_EVENT_MONTHLY', 'MEMBER_NEWS_WEEKLY', 'MEMBER_NEWS_MONTHLY')) NOT VALID;
ALTER TABLE notification_delivery_outbox
    VALIDATE CONSTRAINT chk_notification_delivery_outbox_kind_vocab;

ALTER TABLE youtube_notification_outbox
    DROP CONSTRAINT IF EXISTS chk_youtube_notification_outbox_status_vocab;
ALTER TABLE youtube_notification_outbox
    ADD CONSTRAINT chk_youtube_notification_outbox_status_vocab
    CHECK (status IN ('PENDING', 'SENT', 'FAILED')) NOT VALID;
ALTER TABLE youtube_notification_outbox
    VALIDATE CONSTRAINT chk_youtube_notification_outbox_status_vocab;

ALTER TABLE youtube_notification_outbox
    DROP CONSTRAINT IF EXISTS chk_youtube_notification_outbox_kind_vocab;
ALTER TABLE youtube_notification_outbox
    ADD CONSTRAINT chk_youtube_notification_outbox_kind_vocab
    CHECK (kind IN ('NEW_VIDEO', 'NEW_SHORT', 'LIVE_STREAM', 'COMMUNITY_POST', 'MILESTONE')) NOT VALID;
ALTER TABLE youtube_notification_outbox
    VALIDATE CONSTRAINT chk_youtube_notification_outbox_kind_vocab;

ALTER TABLE youtube_notification_delivery
    DROP CONSTRAINT IF EXISTS chk_youtube_notification_delivery_status_vocab;
ALTER TABLE youtube_notification_delivery
    ADD CONSTRAINT chk_youtube_notification_delivery_status_vocab
    CHECK (status IN ('PENDING', 'SENDING', 'SENT', 'FAILED', 'QUARANTINED')) NOT VALID;
ALTER TABLE youtube_notification_delivery
    VALIDATE CONSTRAINT chk_youtube_notification_delivery_status_vocab;

ALTER TABLE youtube_content_alarm_tracking
    DROP CONSTRAINT IF EXISTS chk_youtube_content_alarm_tracking_kind_vocab;
ALTER TABLE youtube_content_alarm_tracking
    ADD CONSTRAINT chk_youtube_content_alarm_tracking_kind_vocab
    CHECK (kind IN ('NEW_VIDEO', 'NEW_SHORT', 'LIVE_STREAM', 'COMMUNITY_POST', 'MILESTONE')) NOT VALID;
ALTER TABLE youtube_content_alarm_tracking
    VALIDATE CONSTRAINT chk_youtube_content_alarm_tracking_kind_vocab;

ALTER TABLE youtube_content_alarm_tracking
    DROP CONSTRAINT IF EXISTS chk_youtube_content_alarm_tracking_delivery_status_vocab;
ALTER TABLE youtube_content_alarm_tracking
    ADD CONSTRAINT chk_youtube_content_alarm_tracking_delivery_status_vocab
    CHECK (delivery_status IN ('PENDING', 'SENT')) NOT VALID;
ALTER TABLE youtube_content_alarm_tracking
    VALIDATE CONSTRAINT chk_youtube_content_alarm_tracking_delivery_status_vocab;

ALTER TABLE youtube_community_shorts_alarm_states
    DROP CONSTRAINT IF EXISTS chk_youtube_community_shorts_alarm_states_kind_vocab;
ALTER TABLE youtube_community_shorts_alarm_states
    ADD CONSTRAINT chk_youtube_community_shorts_alarm_states_kind_vocab
    CHECK (kind IN ('NEW_VIDEO', 'NEW_SHORT', 'LIVE_STREAM', 'COMMUNITY_POST', 'MILESTONE')) NOT VALID;
ALTER TABLE youtube_community_shorts_alarm_states
    VALIDATE CONSTRAINT chk_youtube_community_shorts_alarm_states_kind_vocab;

ALTER TABLE youtube_community_shorts_alarm_states
    DROP CONSTRAINT IF EXISTS chk_youtube_community_shorts_alarm_states_delivery_status_vocab;
ALTER TABLE youtube_community_shorts_alarm_states
    ADD CONSTRAINT chk_youtube_community_shorts_alarm_states_delivery_status_vocab
    CHECK (delivery_status IN ('DETECTED', 'ENQUEUED', 'SENT')) NOT VALID;
ALTER TABLE youtube_community_shorts_alarm_states
    VALIDATE CONSTRAINT chk_youtube_community_shorts_alarm_states_delivery_status_vocab;

ALTER TABLE youtube_community_shorts_source_posts
    DROP CONSTRAINT IF EXISTS chk_youtube_community_shorts_source_posts_kind_vocab;
ALTER TABLE youtube_community_shorts_source_posts
    ADD CONSTRAINT chk_youtube_community_shorts_source_posts_kind_vocab
    CHECK (kind IN ('NEW_VIDEO', 'NEW_SHORT', 'LIVE_STREAM', 'COMMUNITY_POST', 'MILESTONE')) NOT VALID;
ALTER TABLE youtube_community_shorts_source_posts
    VALIDATE CONSTRAINT chk_youtube_community_shorts_source_posts_kind_vocab;

ALTER TABLE youtube_live_sessions
    DROP CONSTRAINT IF EXISTS chk_youtube_live_sessions_status_vocab;
ALTER TABLE youtube_live_sessions
    ADD CONSTRAINT chk_youtube_live_sessions_status_vocab
    CHECK (status IN ('UPCOMING', 'LIVE', 'ENDED')) NOT VALID;
ALTER TABLE youtube_live_sessions
    VALIDATE CONSTRAINT chk_youtube_live_sessions_status_vocab;

ALTER TABLE youtube_notification_delivery_telemetry
    DROP CONSTRAINT IF EXISTS chk_youtube_notification_delivery_telemetry_alarm_type_vocab;
ALTER TABLE youtube_notification_delivery_telemetry
    ADD CONSTRAINT chk_youtube_notification_delivery_telemetry_alarm_type_vocab
    CHECK (alarm_type IN ('LIVE', 'COMMUNITY', 'SHORTS', 'BIRTHDAY', 'ANNIVERSARY')) NOT VALID;
ALTER TABLE youtube_notification_delivery_telemetry
    VALIDATE CONSTRAINT chk_youtube_notification_delivery_telemetry_alarm_type_vocab;

ALTER TABLE major_events
    DROP CONSTRAINT IF EXISTS chk_major_events_status_vocab;
ALTER TABLE major_events
    ADD CONSTRAINT chk_major_events_status_vocab
    CHECK (status IN ('active', 'ended', 'canceled')) NOT VALID;
ALTER TABLE major_events
    VALIDATE CONSTRAINT chk_major_events_status_vocab;

ALTER TABLE members
    DROP CONSTRAINT IF EXISTS chk_members_status_vocab;
ALTER TABLE members
    ADD CONSTRAINT chk_members_status_vocab
    CHECK (status IN ('active', 'graduated')) NOT VALID;
ALTER TABLE members
    VALIDATE CONSTRAINT chk_members_status_vocab;
