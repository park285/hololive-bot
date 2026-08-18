INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_ALARM_NOTIFICATION_GROUP', NULL, '🔔 방송 알림 ({{.Count}}개)

{{if le .MinutesUntil 0}}⏰ 여러 방송이 시작되었습니다.{{else if eq (len .ScheduledTimes) 0}}⏰ 여러 방송이 곧 시작됩니다.{{else if eq (len .ScheduledTimes) 1}}⏰ {{index .ScheduledTimes 0}} 방송예정{{else}}⏰ 방송예정: {{join .ScheduledTimes ", "}}{{end}}

{{range $i, $e := .Entries}}{{if $i}}
{{end}}{{$e.Index}}. {{$e.ChannelName | default "알 수 없는 채널"}}{{if $e.ScheduledKST}}{{if gt $.MinutesUntil 0}} ({{$e.ScheduledKST}} 방송예정){{else}} ({{$e.ScheduledKST}} 방송 시작){{end}}{{else}}{{if gt $.MinutesUntil 0}} (방송예정){{end}}{{end}}
{{if $e.Title}}   {{$e.Title}}
{{end}}{{if $e.URL}}   {{$e.URL}}
{{end}}{{end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CELEBRATION_BIRTHDAY', NULL, '🎂 {{.MemberName}}{{if gt .Ordinal 0}} {{.Ordinal}}번째{{end}} 생일 축하합니다!{{if .ChannelID}}
🔗 https://youtube.com/channel/{{.ChannelID}}{{end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CELEBRATION_ANNIVERSARY', NULL, '🎉 {{.MemberName}} 데뷔 {{.Years}}주년 축하합니다!{{if .ChannelID}}
🔗 https://youtube.com/channel/{{.ChannelID}}{{end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();
