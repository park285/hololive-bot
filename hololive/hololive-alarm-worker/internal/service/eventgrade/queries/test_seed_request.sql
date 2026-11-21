INSERT INTO event_grade_requests(room_id,user_id,region,event_id,chapter_id,board_kind,score)
SELECT $1,$2,region,event_id,chapter_id,'marathon_score',$3
FROM event_grade_chapters WHERE region=$4 AND event_id=$5 AND chapter_id=$6
RETURNING id
