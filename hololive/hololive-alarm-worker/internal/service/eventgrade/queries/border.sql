INSERT INTO event_grade_borders(region,event_id,chapter_id,board_kind,rank,score,started_at,observed_at,is_final)
SELECT $1,$2,$3,'marathon_score',r.rank,r.score,$4,$5,$6
FROM unnest($7::integer[],$8::bigint[]) r(rank,score)
