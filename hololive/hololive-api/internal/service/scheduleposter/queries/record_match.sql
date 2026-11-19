UPDATE schedule_poster_posts d
SET status = $4, reason = $5, matched_poster_id = $2, matched_rows_at = $3
WHERE d.post_id = $1 AND d.status IN ('relevant', 'matched', 'match_failed') AND d.status = $6
  AND (d.matched_poster_id, d.matched_rows_at) IS NOT DISTINCT FROM ($7::bigint, $8::timestamptz)
  AND (d.matched_poster_id, d.matched_rows_at) IS DISTINCT FROM ($2::bigint, $3::timestamptz)
  AND EXISTS (
      SELECT 1 FROM schedule_posters p
      WHERE p.id = $2 AND p.channel_id = d.channel_id AND p.review_status = 'confirmed' AND p.rows_extracted_at = $3
        AND /* fragment:post_window */
  )
