SELECT count(*) > 0,
       CASE WHEN $4 THEN array_agg(rank ORDER BY rank)=$6::integer[] AND array_agg(score ORDER BY rank)=$7::bigint[]
            ELSE max(observed_at)<$5::timestamptz END IS TRUE
FROM event_grade_borders
WHERE (region,event_id,chapter_id)=($1,$2,$3) AND board_kind='marathon_score' AND is_final=$4
