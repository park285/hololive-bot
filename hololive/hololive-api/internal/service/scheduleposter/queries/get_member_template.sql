SELECT COALESCE(t.template, $2)
FROM members m
LEFT JOIN member_schedule_card_templates t ON t.member_id = m.id
WHERE m.id = $1
