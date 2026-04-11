-- 043_add_youtube_video_published_at.sql
-- 유튜브 쇼츠 실제 게시 시각 및 canonical sent_at 의미 고정

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'youtube_videos'
          AND column_name = 'published_at'
    ) THEN
        ALTER TABLE youtube_videos ADD COLUMN published_at TIMESTAMPTZ;
    END IF;
END $$;

COMMENT ON COLUMN youtube_videos.published_at IS '유튜브 영상/쇼츠 실제 게시 시각 (UTC canonical)';
COMMENT ON COLUMN youtube_notification_outbox.sent_at IS '유튜브 알림 최초 성공 발송 시각 (UTC canonical)';
COMMENT ON COLUMN youtube_notification_delivery.sent_at IS '유튜브 room 단위 알림 최초 성공 발송 시각 (UTC canonical)';
