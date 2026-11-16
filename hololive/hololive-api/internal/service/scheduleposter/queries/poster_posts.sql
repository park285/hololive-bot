-- 철회·되살리기 가능 여부는 revoke_post.sql·restore_post.sql과 같다. 조치할 수 있는 글(relevant, matched, match_failed, revoked)은
-- 최근 $2건, 나머지는 최근 $3건이다. 판별만 한 글이 많아도 철회·되살릴 글이 목록에서 밀려나지 않게 하기 위해서다.
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
    WHERE p.id = $1 AND /* post_window */
) listed
WHERE n <= CASE WHEN actionable THEN $2::bigint ELSE $3::bigint END
ORDER BY posted_at DESC, post_id DESC
