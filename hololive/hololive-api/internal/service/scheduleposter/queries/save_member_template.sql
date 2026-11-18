INSERT INTO member_schedule_card_templates (member_id, template) VALUES ($1, $2)
ON CONFLICT (member_id) DO UPDATE SET template = EXCLUDED.template
