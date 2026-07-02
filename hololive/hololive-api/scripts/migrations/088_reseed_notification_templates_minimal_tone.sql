BEGIN;

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('OUTBOX_VIDEO', NULL, '{{if eq .Kind "LIVE_STREAM"}}🔴 {{.MemberName}} 방송 시작{{else}}🔔 {{.MemberName}} 새 영상{{end}}
{{.Title | truncate 50}}
{{.URL}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('OUTBOX_SHORTS', NULL, '🔔 {{.MemberName}} 새 쇼츠
{{.Title | truncate 50}}
{{.URL}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('OUTBOX_COMMUNITY', NULL, '🔔 {{.MemberName}} 커뮤니티 글
{{.ContentText | truncate 100}}
{{.URL}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('OUTBOX_MILESTONE', NULL, '🎉 {{.MemberName}} {{.Milestone}} 달성')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('OUTBOX_VIDEO_GROUP', NULL, '{{if eq .Kind "LIVE_STREAM"}}🔴 {{.MemberName}} 방송 시작 ({{.Count}}){{else if eq .Kind "NEW_VIDEO"}}🔔 {{.MemberName}} 새 영상 ({{.Count}}){{else}}🔔 {{.MemberName}} 알림 ({{.Count}}){{end}}
{{range $idx, $item := .Items}}{{if gt $idx 0}}

{{end}}{{add $idx 1}}. {{$item.Title | truncate 40}}
   {{$item.URL}}{{end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('OUTBOX_SHORTS_GROUP', NULL, '🔔 {{.MemberName}} 새 쇼츠 ({{.Count}})
{{range $idx, $item := .Items}}{{if gt $idx 0}}

{{end}}{{add $idx 1}}. {{$item.Title | truncate 40}}
   {{$item.URL}}{{end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('OUTBOX_COMMUNITY_GROUP', NULL, '🔔 {{.MemberName}} 커뮤니티 글 ({{.Count}})
{{range $idx, $item := .Items}}{{if gt $idx 0}}

{{end}}{{add $idx 1}}. {{$item.ContentText | truncate 40}}
   {{$item.URL}}{{end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('ALARM_DISPATCH_NOTIFICATION', NULL, '{{if .IsStarting}}🔴 {{.MemberName}} 방송 시작{{else if .IsScheduled}}⏰ {{.MemberName}} 방송 예정{{else}}⏰ {{.MemberName}} 방송 {{.MinutesUntil}}분 전{{end}}
  {{.Title}}{{if .ScheduleMessage}}
  {{.ScheduleMessage}}{{end}}{{if .URL}}
  {{.URL}}{{end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('ALARM_DISPATCH_NOTIFICATION_GROUP', NULL, '{{if .IsStarting}}🔴 방송 시작{{else}}⏰ 방송 {{.MinutesUntil}}분 전{{end}}{{range .Entries}}

{{if .IsStarting}}🔴 {{.MemberName}} 방송 시작{{else if .IsScheduled}}⏰ {{.MemberName}} 방송 예정{{else}}⏰ {{.MemberName}} 방송 {{.MinutesUntil}}분 전{{end}}
  {{.Title}}{{if .ScheduleMessage}}
  {{.ScheduleMessage}}{{end}}{{if .URL}}
  {{.URL}}{{end}}{{end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CELEBRATION_BIRTHDAY', NULL, '🎂 {{.MemberName}}{{if gt .Ordinal 0}} {{.Ordinal}}번째{{end}} 생일 축하합니다!{{if .ChannelID}}
https://youtube.com/channel/{{.ChannelID}}{{end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CELEBRATION_ANNIVERSARY', NULL, '🎉 {{.MemberName}} 데뷔 {{.Years}}주년 축하합니다!{{if .ChannelID}}
https://youtube.com/channel/{{.ChannelID}}{{end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

COMMIT;
