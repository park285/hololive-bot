-- 042_add_youtube_community_published_at.sql
-- 유튜브 커뮤니티 실제 게시 시각 저장 컬럼 추가

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'youtube_community_posts'
          AND column_name = 'published_at'
    ) THEN
        ALTER TABLE youtube_community_posts ADD COLUMN published_at TIMESTAMPTZ;
    END IF;
END $$;

COMMENT ON COLUMN youtube_community_posts.published_at IS '유튜브 커뮤니티 실제 게시 시각 (UTC 정규화)';
