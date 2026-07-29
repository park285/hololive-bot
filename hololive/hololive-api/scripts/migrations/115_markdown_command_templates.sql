BEGIN;

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_LIVE_STREAMS', NULL, '{{- if eq .Count 0 -}}
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
{{- end -}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_UPCOMING_STREAMS', NULL, '{{- if eq .Count 0 -}}
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
{{- end -}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_CHANNEL_SCHEDULE', NULL, '{{- if not .ChannelName -}}
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
{{- end -}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MEMBER_DIRECTORY', NULL, '{{- if eq (len .Groups) 0 -}}
👤 등록된 멤버가 없습니다.
{{- else -}}
## 👤 멤버 목록 ({{.Total}})
{{- range .Groups}}

**{{mdsafe .GroupName}}**
{{- range .Members}}
{{- if .ShowBoth}}
- {{mdsafe .Primary}} ({{mdsafe .Secondary}})
{{- else if .Primary}}
- {{mdsafe .Primary}}
{{- else if .Secondary}}
- {{mdsafe .Secondary}}
{{- end}}
{{- end}}
{{- end}}
{{- end -}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_PROFILE', NULL, '{{- if eq (len .Names) 0 -}}
## 👤 멤버 정보
{{- else -}}
## 👤 {{mdsafe (index .Names 0)}}{{if gt (len .Names) 1}} ({{mdsafe (join (slice .Names 1) " / ")}}){{end}}
{{- end}}
{{- if .Catchphrase}}
"{{mdsafe .Catchphrase}}"
{{- end}}
{{- if .Summary}}
{{mdsafe .Summary}}
{{- end}}
{{- if .Highlights}}

**하이라이트**
{{- range .Highlights}}
- {{mdsafe .}}
{{- end}}
{{- end}}
{{- if .DataRows}}

**프로필**
{{- range .DataRows}}
{{- if .Multiline}}
- {{mdsafe .Label}}:
{{mdsafe .Value}}
{{- else}}
- {{mdsafe .Label}}: {{mdsafe .Value}}
{{- end}}
{{- end}}
{{- end}}
{{- if .SocialLinks}}

**링크**
{{- range .SocialLinks}}
- {{mdsafe .Label}}: {{.URL}}
{{- end}}
{{- end}}
{{- if .OfficialURL}}

공식 프로필: {{.OfficialURL}}
{{- end -}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_ALARM_LIST', NULL, '{{- if eq .Count 0 -}}
🔔 설정된 알람이 없습니다.
예) `{{.Prefix}}알람 추가 페코라`
{{- else -}}
## 🔔 알람 ({{.Count}})
{{range $index, $alarm := .Alarms}}
{{add $index 1}}. **{{mdsafe $alarm.MemberName}}**{{if $alarm.TypesLabel}} ({{mdsafe $alarm.TypesLabel}}){{end}}
{{- if $alarm.NextStream}}
{{- if eq $alarm.NextStream.Status "live"}}
   🔴 방송 중
{{- if and $alarm.NextStream.Title $alarm.NextStream.URL}}
   [{{mdsafe $alarm.NextStream.Title}}]({{$alarm.NextStream.URL}})
{{- else if $alarm.NextStream.Title}}
   {{mdsafe $alarm.NextStream.Title}}
{{- else if $alarm.NextStream.URL}}
   {{$alarm.NextStream.URL}}
{{- end}}
{{- else if eq $alarm.NextStream.Status "upcoming"}}
   ⏰ {{if $alarm.NextStream.StartingSoon}}곧 시작{{else}}{{$alarm.NextStream.ScheduledKST}}{{if $alarm.NextStream.TimeDetail}} ({{$alarm.NextStream.TimeDetail}}){{end}}{{end}}
{{- if and $alarm.NextStream.Title $alarm.NextStream.URL}}
   [{{mdsafe $alarm.NextStream.Title}}]({{$alarm.NextStream.URL}})
{{- else if $alarm.NextStream.Title}}
   {{mdsafe $alarm.NextStream.Title}}
{{- else if $alarm.NextStream.URL}}
   {{$alarm.NextStream.URL}}
{{- end}}
{{- end}}
{{- end}}
{{- end}}
{{- end -}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_ALARM_ADDED', NULL, '{{- if .Added -}}
✅ **{{mdsafe .MemberName}}** 알람을 설정했습니다. 방송 시작 5분 전에 알립니다.
{{- if .NextStream}}
{{- if eq .NextStream.Status "live"}}
- 🔴 방송 중
{{- if and .NextStream.Title .NextStream.URL}}
- [{{mdsafe .NextStream.Title}}]({{.NextStream.URL}})
{{- else if .NextStream.Title}}
- {{mdsafe .NextStream.Title}}
{{- else if .NextStream.URL}}
- {{.NextStream.URL}}
{{- end}}
{{- else if eq .NextStream.Status "upcoming"}}
- ⏰ {{if .NextStream.StartingSoon}}곧 시작{{else}}{{.NextStream.ScheduledKST}}{{if .NextStream.TimeDetail}} ({{.NextStream.TimeDetail}}){{end}}{{end}}
{{- if and .NextStream.Title .NextStream.URL}}
- [{{mdsafe .NextStream.Title}}]({{.NextStream.URL}})
{{- else if .NextStream.Title}}
- {{mdsafe .NextStream.Title}}
{{- else if .NextStream.URL}}
- {{.NextStream.URL}}
{{- end}}
{{- end}}
{{- end}}
{{- else -}}
ℹ️ **{{mdsafe .MemberName}}** 알람이 이미 설정되어 있습니다.
{{- end -}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_ALARM_REMOVED', NULL, '{{- if .Removed -}}
✅ **{{mdsafe .MemberName}}** 알람을 해제했습니다.
{{- else -}}
ℹ️ **{{mdsafe .MemberName}}** 알람이 설정되어 있지 않습니다.
{{- end -}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_STATS_COUNT', NULL, '📊 **{{mdsafe .MemberName}}** 구독자 {{.Subscribers}}명')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_STATS_GAINERS', NULL, '## 📊 구독자 증가 순위{{if .Period}} ({{.Period}}){{end}}
{{range .Gainers}}
{{.Rank}}. **{{mdsafe .MemberName}}** +{{.Delta}}명{{if .Current}} (현재 {{.Current}}명){{end}}
{{- end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_CALENDAR', NULL, '{{- if eq .Count 0 -}}
📅 {{.Year}}년 {{.Month}}월 등록된 기념일이 없습니다.
{{- else -}}
## 📅 {{.Year}}년 {{.Month}}월 기념일 ({{.Count}})
{{- range .Days}}

**{{printf "%02d/%02d" .Month .Day}}**
{{- range .Entries}}
{{- if .IsBirthday}}
- 🎂 {{mdsafe .Name}} 생일
{{- else}}
- 🎉 {{mdsafe .Name}} 데뷔 {{.Years}}주년
{{- end}}
{{- end}}
{{- end}}
{{- end -}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MEMBER_NOT_LIVE', NULL, '{{mdsafe .MemberName}}은(는) 현재 방송 중이 아닙니다.')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MEMBER_NO_UPCOMING', NULL, '{{mdsafe .MemberName}}은(는) {{.Hours}}시간 이내 예정된 방송이 없습니다.')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MEMBER_NOT_FOUND', NULL, '❌ ''{{mdsafe .MemberName}}'' 멤버를 찾을 수 없습니다.')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_AMBIGUOUS_MEMBER', NULL, '동일한 이름의 멤버가 여러 명 있습니다.
{{range .Candidates}}{{.Index}}. {{mdsafe .Name}}
{{end}}
예) `{{.Prefix}}{{.CommandExample}}` {{mdsafe .FirstName}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MAJOR_EVENT_WEEKLY_SUMMARY', NULL, '## 📅 이번 주 행사 ({{.Count}})
{{- if .LLMSummary}}

{{.LLMSummary}}
{{- end}}
{{range $index, $event := .Events}}
{{- if and $event.Title $event.Link}}
{{add $index 1}}. [{{mdsafe $event.Title}}]({{$event.Link}})
{{- else if $event.Title}}
{{add $index 1}}. {{mdsafe $event.Title}}
{{- else}}
{{add $index 1}}. {{$event.Link}}
{{- end}}
{{- if $event.DateStr}}
   ⏰ {{$event.DateStr}}
{{- end}}
{{- if $event.Members}}
   {{mdsafe $event.Members}}
{{- end}}
{{- end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MAJOR_EVENT_MONTHLY_SUMMARY', NULL, '## 📅 이번 달 행사 ({{.Count}})
{{- if .LLMSummary}}

{{.LLMSummary}}
{{- end}}
{{range $index, $event := .Events}}
{{- if and $event.Title $event.Link}}
{{add $index 1}}. [{{mdsafe $event.Title}}]({{$event.Link}})
{{- else if $event.Title}}
{{add $index 1}}. {{mdsafe $event.Title}}
{{- else}}
{{add $index 1}}. {{$event.Link}}
{{- end}}
{{- if $event.DateStr}}
   ⏰ {{$event.DateStr}}
{{- end}}
{{- if $event.Members}}
   {{mdsafe $event.Members}}
{{- end}}
{{- end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MAJOR_EVENT_NOT_SUB', NULL, 'ℹ️ 행사 알림이 꺼져 있습니다.
- 설정: `{{.Prefix}}행사 켜기`')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MAJOR_EVENT_STATUS', NULL, '{{if .IsSubscribed}}🔔{{else}}🔕{{end}} 행사 알림: **{{if .IsSubscribed}}켜짐{{else}}꺼짐{{end}}**
{{- if .IsSubscribed}}
- 해제: `{{.Prefix}}행사 끄기`
{{- else}}
- 설정: `{{.Prefix}}행사 켜기`
{{- end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MAJOR_EVENT_USAGE', NULL, '🔔 행사 알림 명령어
- `{{.Prefix}}행사 켜기 / 끄기 / 상태`')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MEMBER_NEWS_DIGEST', NULL, '{{- if .Headline -}}
## {{mdsafe .Headline}}
{{- else -}}
## 📰 멤버 뉴스
{{- end -}}
{{- if eq (len .TopItems) 0 }}
표시할 뉴스가 없습니다.
{{- else }}
{{range $index, $item := .TopItems}}
{{add $index 1}}. {{$item.DateText}} · **{{mdsafe $item.Member}}** · {{mdsafe $item.Category}}
{{- if and $item.Title $item.SourceURL}}
   [{{mdsafe $item.Title}}]({{$item.SourceURL}})
{{- else if $item.Title}}
   {{mdsafe $item.Title}}
{{- else if $item.SourceURL}}
   {{$item.SourceURL}}
{{- end}}
{{- if $item.Summary}}
   {{mdsafe $item.Summary}}
{{- end}}
{{- end}}
{{- if .MoreSummary }}

{{mdsafe .MoreSummary}}
{{- end }}
{{- end }}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MEMBER_NEWS_NO_MEMBERS', NULL, '📰 뉴스 대상 멤버가 없습니다.
예) `{{.Prefix}}알람 추가 페코라`')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MEMBER_NEWS_STATUS', NULL, '{{if .IsSubscribed}}🔔{{else}}🔕{{end}} 뉴스 알림: **{{if .IsSubscribed}}켜짐{{else}}꺼짐{{end}}**
{{- if .IsSubscribed}}
- 발송: 매주 월요일 09:00 KST
- 해제: `{{.Prefix}}뉴스알림 끄기`
{{- else}}
- 설정: `{{.Prefix}}뉴스알림 켜기`
{{- end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

COMMIT;
