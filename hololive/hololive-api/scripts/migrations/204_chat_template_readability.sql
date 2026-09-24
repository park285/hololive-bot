-- 카카오 일반채팅은 목록 내부의 빈 줄을 합치므로 번호·가운뎃점 문단을 사용합니다.
-- 제목과 URL을 분리하고 기본 시청자 수 표시만 제거합니다(변수 계약 유지).
-- 기존 표준 본문과 정확히 같은 전역 기본값만 갱신해 운영자 수정과 채널별 본문을 보존합니다.
UPDATE notification_templates AS target
SET body = seed.new_body,
    updated_at = now()
FROM (VALUES
    ('CMD_LIVE_STREAMS', $old${{- if eq .Count 0 -}}
🔴 방송 중인 스트림이 없습니다.
{{- else -}}
## 🔴 라이브 ({{.Count}})
{{range .Streams}}
- **{{mdsafe .ChannelName}}**{{if gt .ViewerCount 0}} ({{formatNumberKR .ViewerCount}}명){{end}}
{{- if and .Title .URL}}
  [{{mdsafe .Title}}]({{.URL}})
{{- else if .Title}}
  {{mdsafe .Title}}
{{- else if .URL}}
  {{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$old$, $new${{- if eq .Count 0 -}}
🔴 방송 중인 스트림이 없습니다.
{{- else -}}
🔴 방송 중 · {{.Count}}개
{{- range $i, $stream := .Streams}}
{{- if gt $i 0}}

──────────
{{- end}}

{{add $i 1}} · {{mdsafe .ChannelName}}
{{- if .Title}}
{{mdsafe (truncate 64 (trim (replace (replace (replace .Title "\r\n" " ") "\r" " ") "\n" " ")))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$new$),
    ('CMD_UPCOMING_STREAMS', $old${{- if eq .Count 0 -}}
📅 {{.Hours}}시간 이내 예정된 방송이 없습니다.
{{- else -}}
## 📅 예정 방송 ({{.Hours}}시간 이내, {{.Count}})
{{range .Streams}}
- **{{mdsafe .ChannelName}}**
  ⏰ {{.TimeInfo}}
{{- if and .Title .URL}}
  [{{mdsafe .Title}}]({{.URL}})
{{- else if .Title}}
  {{mdsafe .Title}}
{{- else if .URL}}
  {{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$old$, $new${{- if eq .Count 0 -}}
📅 {{.Hours}}시간 이내 예정된 방송이 없습니다.
{{- else -}}
📅 예정 방송 · {{.Count}}개
{{.Hours}}시간 이내
{{- range $i, $stream := .Streams}}
{{- if gt $i 0}}

──────────
{{- end}}

{{add $i 1}} · {{mdsafe .ChannelName}}
⏰ {{mdsafe .TimeInfo}}
{{- if .Title}}
{{mdsafe (truncate 64 (trim (replace (replace (replace .Title "\r\n" " ") "\r" " ") "\n" " ")))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$new$),
    ('CMD_CHANNEL_SCHEDULE', $old${{- if not .ChannelName -}}
❌ 채널 정보를 찾을 수 없습니다.
{{- else if eq .Count 0 -}}
📅 **{{mdsafe .ChannelName}}**
{{.Days}}일 이내 예정된 방송이 없습니다.
{{- else -}}
## 📅 {{mdsafe .ChannelName}} 일정 ({{.Days}}일 이내, {{.Count}})
{{range .Streams}}
{{- if .IsLive}}
- 🔴 방송 중
{{- else}}
- ⏰ {{.TimeInfo}}
{{- end}}
{{- if and .Title .URL}}
  [{{mdsafe .Title}}]({{.URL}})
{{- else if .Title}}
  {{mdsafe .Title}}
{{- else if .URL}}
  {{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$old$, $new${{- if not .ChannelName -}}
❌ 채널 정보를 찾을 수 없습니다.
{{- else if eq .Count 0 -}}
📅 {{mdsafe .ChannelName}}
{{.Days}}일 이내 예정된 방송이 없습니다.
{{- else -}}
📅 {{mdsafe .ChannelName}} 일정
{{.Days}}일 이내 · {{.Count}}개
{{- range $i, $stream := .Streams}}
{{- if gt $i 0}}

──────────
{{- end}}

{{add $i 1}} · {{if .IsLive}}🔴 방송 중{{else}}⏰ {{mdsafe .TimeInfo}}{{end}}
{{- if .Title}}
{{mdsafe (truncate 64 (trim (replace (replace (replace .Title "\r\n" " ") "\r" " ") "\n" " ")))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$new$),
    ('CMD_ALARM_NOTIFICATION', $old$⏰ **{{mdsafe .ChannelName}}** 방송 예정
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
{{- end}}$old$, $new$⏰ {{mdsafe .ChannelName}} 방송 예정
{{if .ScheduledTimeKST}}{{mdsafe .ScheduledTimeKST}} 시작{{else}}곧 시작{{end}}
{{- if .ScheduleMessage}}
{{mdsafe .ScheduleMessage}}
{{- end}}
{{- if .Title}}
{{mdsafe (truncate 64 (trim (replace (replace (replace .Title "\r\n" " ") "\r" " ") "\n" " ")))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$new$),
    ('CMD_ALARM_LIVE_STARTED', $old$🔴 **{{mdsafe .ChannelName}}** 방송 시작
{{- if .ScheduledTimeKST}}
- {{.ScheduledTimeKST}} 시작
{{- end}}
{{- if .Title}}
- {{mdsafe .Title}}
{{- end}}
{{- if .URL}}

{{.URL}}
{{- end}}$old$, $new$🔴 {{mdsafe .ChannelName}} 방송 시작
{{- if .ScheduledTimeKST}}
{{mdsafe .ScheduledTimeKST}} 시작
{{- end}}
{{- if .Title}}
{{mdsafe (truncate 64 (trim (replace (replace (replace .Title "\r\n" " ") "\r" " ") "\n" " ")))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$new$),
    ('CMD_ALARM_NOTIFICATION_GROUP', $old$## 🔔 방송 알림 ({{.Count}})
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
{{- end}}$old$, $new$🔔 방송 알림 · {{.Count}}개
{{if le .MinutesUntil 0}}방송이 시작되었습니다.{{else if eq (len .ScheduledTimes) 0}}곧 시작합니다.{{else if eq (len .ScheduledTimes) 1}}⏰ {{mdsafe (index .ScheduledTimes 0)}}{{else}}⏰ {{mdsafe (join .ScheduledTimes ", ")}}{{end}}
{{- range $i, $entry := .Entries}}
{{- if gt $i 0}}

──────────
{{- end}}

{{.Index}} · {{mdsafe (default "알 수 없는 채널" .ChannelName)}}{{if .ScheduledKST}} ({{mdsafe .ScheduledKST}}){{end}}
{{- if .Title}}
{{mdsafe (truncate 64 (trim (replace (replace (replace .Title "\r\n" " ") "\r" " ") "\n" " ")))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end}}$new$),
    ('OUTBOX_VIDEO', $old${{if eq .Kind "LIVE_STREAM"}}🔴 **{{mdsafe .MemberName}}** 방송 시작{{else if .IsUpcomingPremiere}}🔔 **{{mdsafe .MemberName}}** {{.MinutesUntilPremiere}}분 후 공개 예정{{else if .IsPremiere}}🔔 **{{mdsafe .MemberName}}** 최초공개{{else}}🔔 **{{mdsafe .MemberName}}** 새 영상{{end}}
{{- if and .Title .URL}}
[{{mdsafe (truncate 50 .Title)}}]({{.URL}})
{{- else if .Title}}
{{mdsafe (truncate 50 .Title)}}
{{- else if .URL}}
{{.URL}}
{{- end}}$old$, $new${{if eq .Kind "LIVE_STREAM"}}🔴 {{mdsafe .MemberName}} 방송 시작{{else if .IsUpcomingPremiere}}🔔 {{mdsafe .MemberName}} {{.MinutesUntilPremiere}}분 후 공개 예정{{else if .IsPremiere}}🔔 {{mdsafe .MemberName}} 최초공개{{else}}🔔 {{mdsafe .MemberName}} 새 영상{{end}}
{{- if .Title}}
{{mdsafe (truncate 64 (trim (replace (replace (replace .Title "\r\n" " ") "\r" " ") "\n" " ")))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$new$),
    ('OUTBOX_SHORTS', $old$🔔 **{{mdsafe .MemberName}}** 새 쇼츠
{{- if and .Title .URL}}
[{{mdsafe (truncate 50 .Title)}}]({{.URL}})
{{- else if .Title}}
{{mdsafe (truncate 50 .Title)}}
{{- else if .URL}}
{{.URL}}
{{- end}}$old$, $new$🔔 {{mdsafe .MemberName}} 새 쇼츠
{{- if .Title}}
{{mdsafe (truncate 64 (trim (replace (replace (replace .Title "\r\n" " ") "\r" " ") "\n" " ")))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$new$),
    ('OUTBOX_COMMUNITY', $old$🔔 **{{mdsafe .MemberName}}** 커뮤니티 글
{{- if .ContentText}}
{{mdsafe (truncate 100 .ContentText)}}
{{- end}}
{{- if .URL}}
[커뮤니티 글 보기]({{.URL}})
{{- end}}$old$, $new$🔔 {{mdsafe .MemberName}} 커뮤니티 글
{{- if .ContentText}}
{{mdsafe (truncate 100 (trim (replace (replace (replace .ContentText "\r\n" " ") "\r" " ") "\n" " ")))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$new$),
    ('CELEBRATION_BIRTHDAY_STREAM', $old$🎂 **{{mdsafe .MemberName}}** 생일 방송 일정이 잡혔습니다!
{{- if and .StreamTitle .StreamURL}}
- [{{mdsafe .StreamTitle}}]({{.StreamURL}})
{{- else if .StreamTitle}}
- {{mdsafe .StreamTitle}}
{{- else if .StreamURL}}
- {{.StreamURL}}
{{- end}}
{{- if .ScheduledStartKST}}
- ⏰ {{.ScheduledStartKST}}
{{- end}}$old$, $new$🎂 {{mdsafe .MemberName}} 생일 방송
{{- if .ScheduledStartKST}}
⏰ {{mdsafe .ScheduledStartKST}}
{{- end}}
{{- if .StreamTitle}}
{{mdsafe (truncate 64 (trim (replace (replace (replace .StreamTitle "\r\n" " ") "\r" " ") "\n" " ")))}}
{{- end}}
{{- if .StreamURL}}
{{.StreamURL}}
{{- end}}$new$),
    ('X_SPACE_STARTED', $old$## 🔴 **{{mdsafe .MemberName}}** 스페이스 시작
{{- if .Title}}
[{{mdsafe .Title}}]({{.URL}})
{{- else}}
{{.URL}}
{{- end}}$old$, $new$🔴 {{mdsafe .MemberName}} 스페이스 시작
{{- if .Title}}
{{mdsafe (truncate 64 (trim (replace (replace (replace .Title "\r\n" " ") "\r" " ") "\n" " ")))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$new$)
) AS seed(template_key, old_body, new_body)
WHERE target.template_key = seed.template_key
  AND target.channel_id IS NULL
  AND target.body = seed.old_body
  AND target.body IS DISTINCT FROM seed.new_body;
