SELECT post_id, posted_at, excerpt, status, reason, is_current, revocable, restorable
FROM (
    SELECT d.post_id, d.posted_at, left(d.body, 280) AS excerpt, d.status, d.reason,
           COALESCE((d.matched_poster_id, d.matched_rows_at) = (p.id, p.rows_extracted_at), false) AS is_current,
           p.review_status = 'confirmed' AND d.status IN ('relevant', 'matched', 'match_failed') AS revocable,
           p.review_status = 'confirmed' AND d.status = 'revoked' AS restorable,
           d.status IN ('relevant', 'matched', 'match_failed', 'revoked') AS actionable,
           row_number() OVER (PARTITION BY d.status IN ('relevant', 'matched', 'match_failed', 'revoked')
                              ORDER BY d.posted_at DESC, d.post_id DESC) AS n
    FROM schedule_posters p
    JOIN schedule_poster_posts d ON d.channel_id = p.channel_id
    WHERE p.id = $1 AND /* fragment:post_window */
) listed
WHERE n <= CASE WHEN actionable THEN $2::bigint ELSE $3::bigint END
ORDER BY posted_at DESC, post_id DESC
