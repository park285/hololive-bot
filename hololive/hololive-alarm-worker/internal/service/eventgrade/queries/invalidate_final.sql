DELETE FROM event_grade_borders
WHERE (region,event_id,chapter_id)=($1,$2,$3) AND board_kind='marathon_score' AND is_final
  AND (started_at < $4::timestamptz OR observed_at > $4+interval '24 hours')
