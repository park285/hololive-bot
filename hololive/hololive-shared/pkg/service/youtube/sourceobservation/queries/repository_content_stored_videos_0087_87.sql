SELECT EXISTS (
    SELECT 1
    FROM youtube_videos
    WHERE channel_id = $1
      AND is_short = $2
)
