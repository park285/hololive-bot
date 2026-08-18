BEGIN;

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('ALARM_DISPATCH_NOTIFICATION', NULL, '{{if .IsStarting}}🔔 {{.MemberName}} 방송 시작!{{else if .IsScheduled}}⏰ {{.MemberName}} 방송 예정{{else}}⏰ {{.MemberName}} 방송 {{.MinutesUntil}}분 전{{end}}
📺 {{.Title}}{{if .ScheduleMessage}}
📅 {{.ScheduleMessage}}{{end}}{{if .URL}}
🔗 {{.URL}}{{end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('ALARM_DISPATCH_NOTIFICATION_GROUP', NULL, '{{if .IsStarting}}🔔 방송 시작 알림{{else}}⏰ 방송 {{.MinutesUntil}}분 전 알림{{end}}{{range .Entries}}

{{if .IsStarting}}🔔 {{.MemberName}} 방송 시작!{{else if .IsScheduled}}⏰ {{.MemberName}} 방송 예정{{else}}⏰ {{.MemberName}} 방송 {{.MinutesUntil}}분 전{{end}}
📺 {{.Title}}{{if .ScheduleMessage}}
📅 {{.ScheduleMessage}}{{end}}{{if .URL}}
🔗 {{.URL}}{{end}}{{end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO message_strings(namespace, key, value) VALUES
  ('misc','alarm_unknown_member','알 수 없는 멤버'),
  ('misc','alarm_no_title','제목 없음'),
  ('misc','alarm_no_stream','방송 정보 없음')
ON CONFLICT (namespace, key) DO UPDATE SET value = EXCLUDED.value, updated_at = now();

COMMIT;
