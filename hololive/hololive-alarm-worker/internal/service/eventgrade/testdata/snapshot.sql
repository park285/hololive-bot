SELECT array_agg(rank ORDER BY rank),array_agg(score ORDER BY rank),min(observed_at),max(observed_at)
FROM event_grade_borders
WHERE (region,event_id,chapter_id)=($1,$2,$3) AND board_kind='marathon_score' AND is_final=$4
