EXISTS (
    SELECT
    FROM schedule_poster_posts d
    JOIN schedule_posters p ON p.id = d.matched_poster_id AND p.rows_extracted_at = d.matched_rows_at
    WHERE d.post_id = n.post_id AND d.channel_id = n.channel_id AND d.matched_poster_id = n.poster_id AND d.status = 'matched'
      AND p.review_status = 'confirmed'
      AND EXISTS (
          SELECT
          FROM schedule_poster_changes c
          WHERE c.post_id = d.post_id AND c.channel_id = d.channel_id AND c.kind NOT IN ('same', 'rejected')
      )
)
