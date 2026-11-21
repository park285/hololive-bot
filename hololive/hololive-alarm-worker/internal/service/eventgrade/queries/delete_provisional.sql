DELETE FROM event_grade_borders
WHERE (region,event_id,chapter_id)=($1,$2,$3) AND board_kind='marathon_score' AND NOT is_final
