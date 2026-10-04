
		ON CONFLICT (post_id) DO UPDATE
		SET last_seen_at = EXCLUDED.last_seen_at,
		    published_at = COALESCE(p.published_at, EXCLUDED.published_at),
		    like_count = EXCLUDED.like_count,
		    comment_count = EXCLUDED.comment_count
		-- 같은 게시물을 다시 관측해 카운터와 published_at 보강값이 그대로면 새 행 버전을 만들지 않는다.
		-- last_seen_at은 이 테이블에서 읽는 곳이 없으므로 마지막 값 변화 시각으로 남아도 된다.
		WHERE (p.like_count, p.comment_count) IS DISTINCT FROM (EXCLUDED.like_count, EXCLUDED.comment_count)
		   OR (p.published_at IS NULL AND EXCLUDED.published_at IS NOT NULL)
	