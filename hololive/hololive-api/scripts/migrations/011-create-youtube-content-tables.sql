-- Migration: YouTube Content Tables for Poller/Outbox System
-- Date: 2026-01-22
-- Description: Creates tables for YouTube content polling and notification outbox
-- Related models: internal/domain/youtube_content.go

-- ============================================================================
-- 1. youtube_channel_stats_snapshots - 채널 통계 스냅샷 (구독자 그래프 원천)
-- ============================================================================
CREATE TABLE IF NOT EXISTS youtube_channel_stats_snapshots (
    channel_id       VARCHAR(50) NOT NULL,
    captured_at      TIMESTAMPTZ NOT NULL,
    subscriber_count BIGINT NOT NULL DEFAULT 0,
    view_count       BIGINT NOT NULL DEFAULT 0,
    video_count      BIGINT NOT NULL DEFAULT 0,
    joined_date      BIGINT,
    description      TEXT,
    country          VARCHAR(50),
    handle           VARCHAR(100),
    PRIMARY KEY (channel_id, captured_at)
);

CREATE INDEX IF NOT EXISTS idx_ycss_channel_time ON youtube_channel_stats_snapshots(channel_id, captured_at DESC);

-- ============================================================================
-- 2. youtube_channel_profiles - 채널 프로필 (아바타/배너)
-- ============================================================================
CREATE TABLE IF NOT EXISTS youtube_channel_profiles (
    channel_id VARCHAR(50) PRIMARY KEY,
    avatar     JSONB,
    banner     JSONB,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ============================================================================
-- 3. youtube_videos - 업로드된 영상 (일반 영상/쇼츠)
-- ============================================================================
CREATE TABLE IF NOT EXISTS youtube_videos (
    video_id       VARCHAR(20) PRIMARY KEY,
    channel_id     VARCHAR(50) NOT NULL,
    title          VARCHAR(500) NOT NULL,
    thumbnail      JSONB,
    duration       VARCHAR(20),
    published_text VARCHAR(100),
    published_at   TIMESTAMPTZ,
    is_short       BOOLEAN NOT NULL DEFAULT FALSE,
    is_live_replay BOOLEAN NOT NULL DEFAULT FALSE,
    view_count     BIGINT DEFAULT 0,
    first_seen_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_yv_channel_first_seen ON youtube_videos(channel_id, first_seen_at DESC);
CREATE INDEX IF NOT EXISTS idx_yv_channel_is_short ON youtube_videos(channel_id, is_short);

-- ============================================================================
-- 4. youtube_community_posts - 커뮤니티 포스트
-- ============================================================================
CREATE TABLE IF NOT EXISTS youtube_community_posts (
    post_id        VARCHAR(50) PRIMARY KEY,
    channel_id     VARCHAR(50) NOT NULL,
    author_name    VARCHAR(200),
    author_photo   JSONB,
    content_text   TEXT,
    published_text VARCHAR(100),
    like_count     BIGINT DEFAULT 0,
    comment_count  BIGINT DEFAULT 0,
    images         JSONB,
    attached_video VARCHAR(20),
    first_seen_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ycp_channel_first_seen ON youtube_community_posts(channel_id, first_seen_at DESC);

-- ============================================================================
-- 5. youtube_content_watermarks - 콘텐츠 워터마크 (초기 동기화 및 중복 알림 방지)
-- ============================================================================
CREATE TABLE IF NOT EXISTS youtube_content_watermarks (
    channel_id      VARCHAR(50) NOT NULL,
    watermark_type  VARCHAR(20) NOT NULL, -- 'VIDEO', 'SHORT', 'COMMUNITY_POST'
    initialized     BOOLEAN NOT NULL DEFAULT FALSE,
    last_content_id VARCHAR(50),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (channel_id, watermark_type)
);

-- ============================================================================
-- 6. youtube_notification_outbox - 알림 Outbox (전송/재시도/중복방지)
-- ============================================================================
CREATE TABLE IF NOT EXISTS youtube_notification_outbox (
    id              BIGSERIAL PRIMARY KEY,
    kind            VARCHAR(20) NOT NULL, -- 'NEW_VIDEO', 'NEW_SHORT', 'COMMUNITY_POST', 'MILESTONE'
    channel_id      VARCHAR(50) NOT NULL,
    content_id      VARCHAR(50) NOT NULL,
    payload         JSONB NOT NULL,
    status          VARCHAR(20) NOT NULL DEFAULT 'PENDING', -- 'PENDING', 'SENT', 'FAILED'
    attempt_count   INT NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    locked_at       TIMESTAMPTZ,
    sent_at         TIMESTAMPTZ,
    error           TEXT
);

-- Unique constraint for duplicate prevention (kind + content_id)
CREATE UNIQUE INDEX IF NOT EXISTS idx_yno_kind_content ON youtube_notification_outbox(kind, content_id);
CREATE INDEX IF NOT EXISTS idx_yno_status_created ON youtube_notification_outbox(status, created_at);
CREATE INDEX IF NOT EXISTS idx_yno_status_next_attempt ON youtube_notification_outbox(status, next_attempt_at) WHERE status = 'PENDING';

-- ============================================================================
-- 7. youtube_live_sessions - 라이브 세션 (UPCOMING/LIVE/ENDED)
-- ============================================================================
CREATE TABLE IF NOT EXISTS youtube_live_sessions (
    video_id             VARCHAR(20) PRIMARY KEY,
    channel_id           VARCHAR(50) NOT NULL,
    status               VARCHAR(20) NOT NULL, -- 'UPCOMING', 'LIVE', 'ENDED'
    title                VARCHAR(500),
    scheduled_start_time TIMESTAMPTZ,
    started_at           TIMESTAMPTZ,
    ended_at             TIMESTAMPTZ,
    last_seen_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_yls_channel_last_seen ON youtube_live_sessions(channel_id, last_seen_at DESC);
CREATE INDEX IF NOT EXISTS idx_yls_status_last_seen ON youtube_live_sessions(status, last_seen_at DESC);

-- ============================================================================
-- 8. youtube_live_viewer_samples - 라이브 시청자 샘플 (시계열 데이터)
-- ============================================================================
CREATE TABLE IF NOT EXISTS youtube_live_viewer_samples (
    video_id           VARCHAR(20) NOT NULL,
    captured_at        TIMESTAMPTZ NOT NULL,
    channel_id         VARCHAR(50) NOT NULL,
    concurrent_viewers INT NOT NULL DEFAULT 0,
    PRIMARY KEY (video_id, captured_at)
);

CREATE INDEX IF NOT EXISTS idx_ylvs_channel_time ON youtube_live_viewer_samples(channel_id, captured_at DESC);

-- ============================================================================
-- 9. youtube_stream_stats - 방송 집계 통계 (평균/최대 시청자)
-- ============================================================================
CREATE TABLE IF NOT EXISTS youtube_stream_stats (
    video_id               VARCHAR(20) PRIMARY KEY,
    channel_id             VARCHAR(50) NOT NULL,
    started_at             TIMESTAMPTZ,
    ended_at               TIMESTAMPTZ,
    max_concurrent_viewers INT DEFAULT 0,
    avg_concurrent_viewers INT DEFAULT 0,
    sample_count           INT NOT NULL DEFAULT 0,
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_yss_channel_ended ON youtube_stream_stats(channel_id, ended_at DESC);

-- ============================================================================
-- 10. youtube_content_alarm_tracking - 커뮤니티/쇼츠 알람 시각 추적
-- ============================================================================
CREATE TABLE IF NOT EXISTS youtube_content_alarm_tracking (
    kind                VARCHAR(20) NOT NULL, -- 'NEW_SHORT', 'COMMUNITY_POST'
    content_id          VARCHAR(50) NOT NULL,
    channel_id          VARCHAR(50) NOT NULL,
    actual_published_at TIMESTAMPTZ,
    detected_at         TIMESTAMPTZ NOT NULL,
    alarm_sent_at       TIMESTAMPTZ,
    alarm_latency_millis BIGINT,
    alarm_latency_exceeded BOOLEAN,
    delivery_status     VARCHAR(20) NOT NULL DEFAULT 'PENDING',
    latency_classification_status VARCHAR(40),
    delay_source        VARCHAR(40),
    internal_delay_cause VARCHAR(40),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (kind, content_id)
);

CREATE INDEX IF NOT EXISTS idx_ycat_detected_at ON youtube_content_alarm_tracking(detected_at DESC);
CREATE INDEX IF NOT EXISTS idx_ycat_channel_detected ON youtube_content_alarm_tracking(channel_id, detected_at DESC);
CREATE INDEX IF NOT EXISTS idx_ycat_alarm_sent_at ON youtube_content_alarm_tracking(alarm_sent_at) WHERE alarm_sent_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_ycat_delivery_status ON youtube_content_alarm_tracking(delivery_status, detected_at DESC);
