INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CELEBRATION_BIRTHDAY_STREAM', NULL, '🎂 {{.MemberName}} 생일 방송 일정이 잡혔습니다!{{if .StreamTitle}}
{{.StreamTitle}}{{end}}{{if .ScheduledStartKST}}
⏰ {{.ScheduledStartKST}}{{end}}{{if .StreamURL}}
{{.StreamURL}}{{end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();
