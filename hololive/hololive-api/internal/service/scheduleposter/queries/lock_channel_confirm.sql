-- 같은 멤버의 확정·거절은 이 잠금 아래에서만 해, 자동 검토가 확인한 확정본이 확정·만료 전에 바뀌지 않게 한다.
-- READ COMMITTED에서 다음 문장이 잠금 뒤의 snapshot을 보도록 따로 실행한다.
SELECT pg_advisory_xact_lock(hashtextextended(concat_ws(E'\\x1f', 'schedule_poster_confirm', channel_id), 0))
FROM schedule_posters
WHERE id = $1
