BEGIN;

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('OUTBOX_VIDEO', NULL, '{{if eq .Kind "LIVE_STREAM"}}🔴 **{{mdsafe .MemberName}}** 방송 시작{{else}}🔔 **{{mdsafe .MemberName}}** 새 영상{{end}}
{{- if and .Title .URL}}
[{{mdsafe (truncate 50 .Title)}}]({{.URL}})
{{- else if .Title}}
{{mdsafe (truncate 50 .Title)}}
{{- else if .URL}}
{{.URL}}
{{- end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('OUTBOX_SHORTS', NULL, '🔔 **{{mdsafe .MemberName}}** 새 쇼츠
{{- if and .Title .URL}}
[{{mdsafe (truncate 50 .Title)}}]({{.URL}})
{{- else if .Title}}
{{mdsafe (truncate 50 .Title)}}
{{- else if .URL}}
{{.URL}}
{{- end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('OUTBOX_COMMUNITY', NULL, '🔔 **{{mdsafe .MemberName}}** 커뮤니티 글
{{- if .ContentText}}
{{mdsafe (truncate 100 .ContentText)}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('OUTBOX_MILESTONE', NULL, '🎉 **{{mdsafe .MemberName}}** {{.Milestone}} 달성')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('OUTBOX_VIDEO_GROUP', NULL, '## {{if eq .Kind "LIVE_STREAM"}}🔴 {{mdsafe .MemberName}} 방송 시작 ({{.Count}}){{else if eq .Kind "NEW_VIDEO"}}🔔 {{mdsafe .MemberName}} 새 영상 ({{.Count}}){{else}}🔔 {{mdsafe .MemberName}} 알림 ({{.Count}}){{end}}
{{- range $idx, $item := .Items}}
{{- if and $item.Title $item.URL}}
{{add $idx 1}}. [{{mdsafe (truncate 40 $item.Title)}}]({{$item.URL}})
{{- else if $item.Title}}
{{add $idx 1}}. {{mdsafe (truncate 40 $item.Title)}}
{{- else if $item.URL}}
{{add $idx 1}}. {{$item.URL}}
{{- end}}
{{- end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('OUTBOX_SHORTS_GROUP', NULL, '## 🔔 {{mdsafe .MemberName}} 새 쇼츠 ({{.Count}})
{{- range $idx, $item := .Items}}
{{- if and $item.Title $item.URL}}
{{add $idx 1}}. [{{mdsafe (truncate 40 $item.Title)}}]({{$item.URL}})
{{- else if $item.Title}}
{{add $idx 1}}. {{mdsafe (truncate 40 $item.Title)}}
{{- else if $item.URL}}
{{add $idx 1}}. {{$item.URL}}
{{- end}}
{{- end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('OUTBOX_COMMUNITY_GROUP', NULL, '## 🔔 {{mdsafe .MemberName}} 커뮤니티 글 ({{.Count}})
{{- range $idx, $item := .Items}}
{{- if $item.ContentText}}
{{add $idx 1}}. {{mdsafe (truncate 40 $item.ContentText)}}
{{- if $item.URL}}
   {{$item.URL}}
{{- end}}
{{- else if $item.URL}}
{{add $idx 1}}. {{$item.URL}}
{{- end}}
{{- end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('ALARM_DISPATCH_NOTIFICATION', NULL, '{{if .IsStarting}}🔴 **{{mdsafe .MemberName}}** 방송 시작{{else if .IsScheduled}}⏰ **{{mdsafe .MemberName}}** 방송 예정{{else}}⏰ **{{mdsafe .MemberName}}** 방송 {{.MinutesUntil}}분 전{{end}}
{{- if .Title}}
- {{mdsafe .Title}}
{{- end}}
{{- if .ScheduleMessage}}
- {{mdsafe .ScheduleMessage}}
{{- end}}
{{- if .URL}}
- {{.URL}}
{{- end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('ALARM_DISPATCH_NOTIFICATION_GROUP', NULL, '## {{if .IsStarting}}🔴 방송 시작{{else}}⏰ 방송 {{.MinutesUntil}}분 전{{end}}
{{- range .Entries}}

{{if .IsStarting}}🔴 **{{mdsafe .MemberName}}** 방송 시작{{else if .IsScheduled}}⏰ **{{mdsafe .MemberName}}** 방송 예정{{else}}⏰ **{{mdsafe .MemberName}}** 방송 {{.MinutesUntil}}분 전{{end}}
{{- if .Title}}
- {{mdsafe .Title}}
{{- end}}
{{- if .ScheduleMessage}}
- {{mdsafe .ScheduleMessage}}
{{- end}}
{{- if .URL}}
- {{.URL}}
{{- end}}
{{- end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_ALARM_NOTIFICATION', NULL, '⏰ **{{mdsafe .ChannelName}}** 방송 예정
{{- if .ScheduledTimeKST}}
- {{.ScheduledTimeKST}} 시작
{{- else}}
- 곧 시작
{{- end}}
{{- if .ScheduleMessage}}
- {{mdsafe .ScheduleMessage}}
{{- end}}
{{- if .Title}}
- {{mdsafe .Title}}
{{- end}}
{{- if .URL}}

{{.URL}}
{{- end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_ALARM_LIVE_STARTED', NULL, '🔴 **{{mdsafe .ChannelName}}** 방송 시작
{{- if .ScheduledTimeKST}}
- {{.ScheduledTimeKST}} 시작
{{- end}}
{{- if .Title}}
- {{mdsafe .Title}}
{{- end}}
{{- if .URL}}

{{.URL}}
{{- end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_ALARM_NOTIFICATION_GROUP', NULL, '## 🔔 방송 알림 ({{.Count}})
{{if le .MinutesUntil 0}}방송이 시작되었습니다.{{else if eq (len .ScheduledTimes) 0}}곧 시작합니다.{{else if eq (len .ScheduledTimes) 1}}⏰ {{index .ScheduledTimes 0}}{{else}}⏰ {{join .ScheduledTimes ", "}}{{end}}
{{- range .Entries}}
{{.Index}}. **{{mdsafe (default "알 수 없는 채널" .ChannelName)}}**{{if .ScheduledKST}} ({{.ScheduledKST}}){{end}}
{{- if and .Title .URL}}
   [{{mdsafe .Title}}]({{.URL}})
{{- else if .Title}}
   {{mdsafe .Title}}
{{- else if .URL}}
   {{.URL}}
{{- end}}
{{- end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MILESTONE_ACHIEVED', NULL, '🎉 **{{mdsafe .MemberName}}** 구독자 {{.Milestone}}명 달성!')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MILESTONE_APPROACHING', NULL, '📊 **{{mdsafe .MemberName}}** 구독자 {{.Milestone}}명까지 {{.Remaining}}명 남았습니다.')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CELEBRATION_BIRTHDAY', NULL, '🎂 **{{mdsafe .MemberName}}**{{if gt .Ordinal 0}} {{.Ordinal}}번째{{end}} 생일 축하합니다!{{if .ChannelID}}
https://youtube.com/channel/{{.ChannelID}}{{end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CELEBRATION_ANNIVERSARY', NULL, '🎉 **{{mdsafe .MemberName}}** 데뷔 {{.Years}}주년 축하합니다!{{if .ChannelID}}
https://youtube.com/channel/{{.ChannelID}}{{end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CELEBRATION_BIRTHDAY_STREAM', NULL, '🎂 **{{mdsafe .MemberName}}** 생일 방송 일정이 잡혔습니다!
{{- if and .StreamTitle .StreamURL}}
- [{{mdsafe .StreamTitle}}]({{.StreamURL}})
{{- else if .StreamTitle}}
- {{mdsafe .StreamTitle}}
{{- else if .StreamURL}}
- {{.StreamURL}}
{{- end}}
{{- if .ScheduledStartKST}}
- ⏰ {{.ScheduledStartKST}}
{{- end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

COMMIT;
