-- 006-base-runtime-tables.sql
-- 목적: prod base 런타임 테이블을 pre-007 상태로 멱등 재구성한다.
--
-- 배경: 001~006 migration이 부재하며, prod의 base 테이블(members, alarms,
-- youtube_milestones, youtube_stats_history, youtube_stats_changes,
-- youtube_notification_outbox)은 레거시 GORM AutoMigrate / init-db 스크립트로
-- 생성되었고 manifest SSOT에는 포함되지 않았다. 그 결과 manifest 전체를 빈 DB에
-- 그대로 재생하면 008(members 인덱스), 010(youtube_notification_outbox ALTER) 등에서
-- "relation does not exist"로 실패한다. 이 파일이 그 base를 manifest 최초 단계에서
-- 복원하여 전체 chain이 빈 DB에서 clean하게 재생되도록 한다.
--
-- 불변식: 여기서 만드는 컬럼 집합은 007 직전(pre-007) 상태이며, 현재 GORM struct가
-- 가진 컬럼에서 007+ migration이 ADD COLUMN으로 추가하는 컬럼을 제외한 것이다.
-- 007+가 동일 컬럼을 ADD하므로 base가 full 스키마를 만들면 이후 ALTER가 충돌한다.
--
-- prod 안전성: prod에는 이 테이블들이 이미 존재한다. 모든 DDL은 CREATE TABLE/INDEX
-- IF NOT EXISTS 및 constraint guard로 작성되어 prod에서는 완전한 no-op이다.
-- DROP/파괴적 ALTER 없음.

-- ============================================================================
-- members: 멤버 마스터. 레거시 GORM AutoMigrate가 생성.
-- pre-007 컬럼 = GORM member.Model 컬럼 − {photo,photo_updated_at(009),
-- org,suborg,sync_source(016), chzzk_channel_id(017), twitch_user_id(018),
-- short_korean_name(062), birthday,debut_date(063)}.
-- ============================================================================
CREATE TABLE IF NOT EXISTS members (
    id            SERIAL PRIMARY KEY,
    slug          VARCHAR(100) NOT NULL,
    channel_id    VARCHAR(64),
    english_name  VARCHAR(200) NOT NULL,
    japanese_name VARCHAR(200),
    korean_name   VARCHAR(200),
    status        VARCHAR(20) NOT NULL DEFAULT 'active',
    is_graduated  BOOLEAN NOT NULL DEFAULT false,
    aliases       JSONB
);

-- 008이 DROP IF EXISTS 후 재생성하는 인덱스의 base 형태(008이 prod base에서 정리하던 대상).
CREATE UNIQUE INDEX IF NOT EXISTS idx_members_slug ON members (slug);
CREATE INDEX IF NOT EXISTS idx_members_status ON members (status);
CREATE INDEX IF NOT EXISTS idx_members_channel_id ON members (channel_id);
CREATE INDEX IF NOT EXISTS idx_members_english_name ON members (english_name);
CREATE INDEX IF NOT EXISTS idx_members_name_search ON members (english_name);

-- ============================================================================
-- alarms: 사용자별 방송 알람 구독 (Valkey 영속 백업). init-db 03-create-alarms-table.sql.
-- pre-007 컬럼 = 현재 도메인 Alarm − {alarm_types(010)}.
-- ============================================================================
CREATE TABLE IF NOT EXISTS alarms (
    id          SERIAL PRIMARY KEY,
    room_id     VARCHAR(64) NOT NULL,
    user_id     VARCHAR(64) NOT NULL,
    channel_id  VARCHAR(64) NOT NULL,
    member_name VARCHAR(200),
    room_name   VARCHAR(200),
    user_name   VARCHAR(200),
    created_at  TIMESTAMPTZ DEFAULT NOW(),
    CONSTRAINT alarms_unique UNIQUE (room_id, user_id, channel_id)
);

CREATE INDEX IF NOT EXISTS idx_alarms_room_user ON alarms (room_id, user_id);
CREATE INDEX IF NOT EXISTS idx_alarms_channel ON alarms (channel_id);

-- ============================================================================
-- youtube_milestones: 구독자 마일스톤 달성 기록. init-db 04-create-milestones-table.sql.
-- ============================================================================
CREATE TABLE IF NOT EXISTS youtube_milestones (
    id          SERIAL PRIMARY KEY,
    channel_id  VARCHAR(24) NOT NULL,
    member_name TEXT NOT NULL,
    type        VARCHAR(20) NOT NULL,
    value       BIGINT NOT NULL,
    achieved_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    notified    BOOLEAN NOT NULL DEFAULT false,
    CONSTRAINT youtube_milestones_unique UNIQUE (channel_id, type, value)
);

CREATE INDEX IF NOT EXISTS idx_milestones_achieved_at ON youtube_milestones (achieved_at DESC);
CREATE INDEX IF NOT EXISTS idx_milestones_channel ON youtube_milestones (channel_id);
CREATE INDEX IF NOT EXISTS idx_milestones_unnotified ON youtube_milestones (notified) WHERE notified = false;

-- ============================================================================
-- youtube_stats_history: 채널 통계 시계열. init-db 02-create-youtube-stats-table.sql.
-- ============================================================================
CREATE TABLE IF NOT EXISTS youtube_stats_history (
    time        TIMESTAMPTZ NOT NULL,
    channel_id  VARCHAR(64) NOT NULL,
    member_name VARCHAR(100),
    subscribers BIGINT,
    videos      BIGINT,
    views       BIGINT,
    CONSTRAINT youtube_stats_history_pkey PRIMARY KEY (time, channel_id)
);

CREATE INDEX IF NOT EXISTS idx_youtube_stats_history_channel_time ON youtube_stats_history (channel_id, time DESC);
CREATE INDEX IF NOT EXISTS idx_youtube_stats_history_time ON youtube_stats_history (time DESC);

-- ============================================================================
-- youtube_stats_changes: 통계 변화 감지 기록. 레거시 GORM AutoMigrate가 생성.
-- 컬럼은 stats_repository INSERT/SELECT(channel_id..detected_at) + notified 플래그.
-- 008이 idx_changes_channel_detected를 추가하고 idx_changes_detected/_unnotified를
-- 기존으로 가정하므로 base에 둔다.
-- ============================================================================
CREATE TABLE IF NOT EXISTS youtube_stats_changes (
    id                SERIAL PRIMARY KEY,
    channel_id        VARCHAR(64) NOT NULL,
    member_name       VARCHAR(100),
    subscriber_change BIGINT NOT NULL DEFAULT 0,
    video_change      BIGINT NOT NULL DEFAULT 0,
    view_change       BIGINT NOT NULL DEFAULT 0,
    previous_subs     BIGINT,
    current_subs      BIGINT,
    previous_videos   BIGINT,
    current_videos    BIGINT,
    detected_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    notified          BOOLEAN NOT NULL DEFAULT false
);

CREATE INDEX IF NOT EXISTS idx_changes_detected ON youtube_stats_changes (detected_at DESC);
CREATE INDEX IF NOT EXISTS idx_changes_unnotified ON youtube_stats_changes (notified) WHERE notified = false;

-- ============================================================================
-- youtube_notification_outbox: 알림 Outbox. 레거시 GORM AutoMigrate가 생성.
-- 011이 동일 테이블을 CREATE IF NOT EXISTS로 다시 정의하지만, 010이 011보다 먼저
-- 실행되며 이 테이블에 ALTER ADD COLUMN(attempt_count,next_attempt_at)을 수행한다.
-- 따라서 base는 011 컬럼 − {attempt_count,next_attempt_at}(010이 추가)로 둔다.
-- ============================================================================
CREATE TABLE IF NOT EXISTS youtube_notification_outbox (
    id          BIGSERIAL PRIMARY KEY,
    kind        VARCHAR(20) NOT NULL,
    channel_id  VARCHAR(50) NOT NULL,
    content_id  VARCHAR(50) NOT NULL,
    payload     JSONB NOT NULL,
    status      VARCHAR(20) NOT NULL DEFAULT 'PENDING',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    locked_at   TIMESTAMPTZ,
    sent_at     TIMESTAMPTZ,
    error       TEXT
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_yno_kind_content ON youtube_notification_outbox (kind, content_id);
CREATE INDEX IF NOT EXISTS idx_yno_status_created ON youtube_notification_outbox (status, created_at);
