-- 097_integrity_and_type_unification.sql
-- apply-all.sh는 파일 단위 psql autocommit으로 실행하므로 CONCURRENTLY가 유효하다.
-- 파일 중간 실패 시 ledger 미기록으로 파일 전체가 재적용되므로 모든 문장은 멱등이어야 한다.

-- members.slug UNIQUE 복원. 006이 만들고 008이 "미사용 인덱스"로 drop한 뒤 members의 유일성이
-- PK(id)뿐이라, 시드의 대상 없는 ON CONFLICT DO NOTHING이 아무것도 막지 못했다.
-- 중복이 이미 있으면 CONCURRENTLY 빌드가 invalid index로 실패하므로 먼저 명시적으로 검출한다.
DO $$
DECLARE dup_slugs TEXT;
BEGIN
    SELECT string_agg(slug || ' x' || cnt, ', ')
    INTO dup_slugs
    FROM (SELECT slug, count(*) AS cnt FROM members GROUP BY slug HAVING count(*) > 1) d;
    IF dup_slugs IS NOT NULL THEN
        RAISE EXCEPTION 'members.slug 중복 존재 — 수동 병합 후 재적용 필요: %', dup_slugs;
    END IF;
END $$;

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_members_slug ON members (slug);

-- auth 두 테이블만 naive TIMESTAMP(022). 기록 경로가 전부 time.Now().UTC()이고 DB 서버 TZ도 UTC라
-- 저장값은 UTC wall-clock — AT TIME ZONE 'UTC'가 값 보존 변환이다. 재적용 시 이중 변환을 막기 위해
-- 아직 naive인 컬럼에만 실행한다.
DO $$
DECLARE col RECORD;
BEGIN
    FOR col IN
        SELECT c.table_name, c.column_name
        FROM information_schema.columns c
        WHERE c.table_schema = 'public'
          AND c.data_type = 'timestamp without time zone'
          AND (c.table_name, c.column_name) IN (
              ('auth_users', 'created_at'),
              ('auth_users', 'updated_at'),
              ('auth_password_reset_tokens', 'expires_at'),
              ('auth_password_reset_tokens', 'used_at'),
              ('auth_password_reset_tokens', 'created_at'))
    LOOP
        EXECUTE format(
            'ALTER TABLE %I ALTER COLUMN %I TYPE TIMESTAMPTZ USING %I AT TIME ZONE ''UTC''',
            col.table_name, col.column_name, col.column_name);
    END LOOP;
END $$;

DELETE FROM auth_password_reset_tokens t
WHERE NOT EXISTS (SELECT 1 FROM auth_users u WHERE u.id = t.user_id);

ALTER TABLE auth_password_reset_tokens
    DROP CONSTRAINT IF EXISTS auth_password_reset_tokens_user_id_fkey;

ALTER TABLE auth_password_reset_tokens
    ADD CONSTRAINT auth_password_reset_tokens_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES auth_users(id) ON DELETE CASCADE NOT VALID;

ALTER TABLE auth_password_reset_tokens
    VALIDATE CONSTRAINT auth_password_reset_tokens_user_id_fkey;

-- 059의 DROP 대상 이름이 058의 실제 제약명과 달라(_notification_ 추가) 구 CHECK가 잔존해 왔다.
ALTER TABLE alarm_dispatch_events
    DROP CONSTRAINT IF EXISTS alarm_dispatch_events_payload_room_agnostic_check;

-- 식별자 폭 표준화: channel_id=64, room_id=100 (096 핫패스 정규화의 잔여분).
DO $$
DECLARE bad TEXT;
BEGIN
    SELECT string_agg(DISTINCT src, ', ') INTO bad FROM (
        SELECT 'youtube_channel_latest_stats.channel_id' AS src
        FROM youtube_channel_latest_stats WHERE length(channel_id) > 64
        UNION ALL
        SELECT 'major_event_subscriptions.room_id'
        FROM major_event_subscriptions WHERE length(room_id) > 100
        UNION ALL
        SELECT 'member_news_subscriptions.room_id'
        FROM member_news_subscriptions WHERE length(room_id) > 100
    ) s;
    IF bad IS NOT NULL THEN
        RAISE EXCEPTION '표준 폭 초과 값 존재 — 수동 정리 후 재적용 필요: %', bad;
    END IF;
END $$;

ALTER TABLE youtube_channel_profiles ALTER COLUMN channel_id TYPE VARCHAR(64);
ALTER TABLE youtube_channel_stats_snapshots ALTER COLUMN channel_id TYPE VARCHAR(64);
ALTER TABLE youtube_community_posts ALTER COLUMN channel_id TYPE VARCHAR(64);
ALTER TABLE youtube_community_shorts_alarm_states ALTER COLUMN channel_id TYPE VARCHAR(64);
ALTER TABLE youtube_community_shorts_source_posts ALTER COLUMN channel_id TYPE VARCHAR(64);
ALTER TABLE youtube_content_alarm_tracking ALTER COLUMN channel_id TYPE VARCHAR(64);
ALTER TABLE youtube_content_watermarks ALTER COLUMN channel_id TYPE VARCHAR(64);
ALTER TABLE youtube_live_sessions ALTER COLUMN channel_id TYPE VARCHAR(64);
ALTER TABLE youtube_live_viewer_samples ALTER COLUMN channel_id TYPE VARCHAR(64);
ALTER TABLE youtube_notification_delivery_telemetry ALTER COLUMN channel_id TYPE VARCHAR(64);
ALTER TABLE youtube_notification_outbox ALTER COLUMN channel_id TYPE VARCHAR(64);
ALTER TABLE youtube_stream_stats ALTER COLUMN channel_id TYPE VARCHAR(64);
ALTER TABLE youtube_videos ALTER COLUMN channel_id TYPE VARCHAR(64);
ALTER TABLE youtube_channel_latest_stats ALTER COLUMN channel_id TYPE VARCHAR(64);
ALTER TABLE acl_rooms ALTER COLUMN room_id TYPE VARCHAR(100);
ALTER TABLE major_event_subscriptions ALTER COLUMN room_id TYPE VARCHAR(100);
ALTER TABLE member_news_subscriptions ALTER COLUMN room_id TYPE VARCHAR(100);

DROP INDEX CONCURRENTLY IF EXISTS idx_alarms_channel;
DROP INDEX CONCURRENTLY IF EXISTS idx_auth_reset_tokens_valid_lookup;

-- major_events 단일 컬럼 인덱스 6종은 전부 저선택도이고 실쿼리는 복합 필터
-- (status+type+event_start_date / link_status+link_checked_at)만 사용한다.
DROP INDEX CONCURRENTLY IF EXISTS idx_major_events_status;
DROP INDEX CONCURRENTLY IF EXISTS idx_major_events_type;
DROP INDEX CONCURRENTLY IF EXISTS idx_major_events_notified;
DROP INDEX CONCURRENTLY IF EXISTS idx_major_events_notified_month;
DROP INDEX CONCURRENTLY IF EXISTS idx_major_events_link_status;
DROP INDEX CONCURRENTLY IF EXISTS idx_major_events_link_checked_at;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_major_events_status_type_start
    ON major_events (status, type, event_start_date);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_major_events_link_check
    ON major_events (link_status, link_checked_at);

-- aliases 조회는 루트가 아닌 하위 경로(aliases->'ko', aliases->'ja')에 ?/@> 를 걸므로
-- 평범한 GIN(aliases)로는 가속되지 않는다 — 경로별 표현식 GIN이 필요하다.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_members_aliases_ko_gin
    ON members USING GIN ((aliases->'ko'));

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_members_aliases_ja_gin
    ON members USING GIN ((aliases->'ja'));
