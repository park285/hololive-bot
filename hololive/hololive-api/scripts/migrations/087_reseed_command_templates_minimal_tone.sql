-- 026/028/029/031의 DO NOTHING/NOT-EXISTS 시드를 이 파일의 upsert가 승계한다(이후 본문 SSOT는 이 파일).
-- 변수 셋은 기존 본문과 동일하게 유지해야 한다(template_sample_data·formatter 계약, MESSAGE_STYLE_GUIDE.md §10).

BEGIN;

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_HELP', NULL, '홀로라이브 봇 명령어

[방송]
  {{.Prefix}}라이브 - 방송 중 목록
  {{.Prefix}}라이브 [멤버명] - 멤버 라이브 확인
  {{.Prefix}}예정 - 예정 방송 목록
  {{.Prefix}}예정 [멤버명] - 멤버 예정 방송
  {{.Prefix}}멤버 [이름] - 일주일 이내 방송 일정

[멤버]
  {{.Prefix}}멤버 - 전체 멤버 목록
  {{.Prefix}}정보 [멤버명] - 프로필 조회

[알람]
  {{.Prefix}}알람 추가 [멤버명]
  {{.Prefix}}알람 제거 [멤버명]
  {{.Prefix}}알람 목록
  {{.Prefix}}알람 초기화

[뉴스]
  {{.Prefix}}뉴스 - 주간 뉴스 요약
  {{.Prefix}}뉴스알림 켜기 / 끄기 / 상태

[행사]
  {{.Prefix}}행사 - 행사 알림 상태
  {{.Prefix}}행사 켜기 / 끄기

[기념일]
  {{.Prefix}}기념일 - 이번 달 생일·주년
  {{.Prefix}}기념일 다음달 / 저번달

[통계]
  {{.Prefix}}구독자 [멤버명] - 현재 구독자 수
  {{.Prefix}}구독자순위 - 10일간 증가 TOP 10
  {{.Prefix}}구독자순위 [기간]')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_LIVE_STREAMS', NULL, '{{- if eq .Count 0 -}}
🔴 방송 중인 스트림이 없습니다.
{{- else -}}
🔴 라이브 ({{.Count}})
{{range $index, $stream := .Streams}}
{{- if gt $index 0}}

{{end -}}
{{$stream.ChannelName}}{{if gt $stream.ViewerCount 0}} ({{formatNumberKR $stream.ViewerCount}}명){{end}}
  {{$stream.Title}}
  {{$stream.URL}}
{{- end -}}
{{- end -}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_UPCOMING_STREAMS', NULL, '{{- if eq .Count 0 -}}
📅 {{.Hours}}시간 이내 예정된 방송이 없습니다.
{{- else -}}
📅 예정 방송 ({{.Hours}}시간 이내, {{.Count}})
{{range $index, $stream := .Streams}}
{{- if gt $index 0}}

{{end -}}
{{$stream.ChannelName}}
  ⏰ {{$stream.TimeInfo}}
  {{$stream.Title}}
  {{$stream.URL}}
{{- end -}}
{{- end -}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_CHANNEL_SCHEDULE', NULL, '{{- if not .ChannelName -}}
❌ 채널 정보를 찾을 수 없습니다.
{{- else if eq .Count 0 -}}
📅 {{.ChannelName}}
{{.Days}}일 이내 예정된 방송이 없습니다.
{{- else -}}
📅 {{.ChannelName}} 일정 ({{.Days}}일 이내, {{.Count}})
{{range $index, $entry := .Streams}}
{{- if gt $index 0}}

{{- end}}
{{- if $entry.IsLive}}
🔴 방송 중
  {{$entry.Title}}
{{- else}}
⏰ {{$entry.TimeInfo}}
  {{$entry.Title}}
{{- end}}
  {{$entry.URL}}
{{- end -}}
{{- end -}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MEMBER_DIRECTORY', NULL, '{{- if eq (len .Groups) 0 -}}
👤 등록된 멤버가 없습니다.
{{- else -}}
👤 멤버 목록 ({{.Total}})
{{range $index, $group := .Groups}}
{{- if gt $index 0}}

{{- end}}[{{$group.GroupName}}]
{{- range $member := $group.Members}}
{{- if $member.ShowBoth}}
- {{$member.Primary}} ({{$member.Secondary}})
{{- else if $member.Primary}}
- {{$member.Primary}}
{{- else if $member.Secondary}}
- {{$member.Secondary}}
{{- end}}
{{- end}}
{{- end -}}
{{- end -}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_PROFILE', NULL, '{{- if eq (len .Names) 0 -}}
👤 멤버 정보
{{- else -}}
👤 {{index .Names 0}}{{if gt (len .Names) 1}} ({{join (slice .Names 1) " / "}}){{end}}
{{- end}}
{{- if .Catchphrase}}
"{{.Catchphrase}}"
{{- end}}
{{- if .Summary}}
{{.Summary}}
{{- end}}
{{- if .Highlights}}

[하이라이트]
{{- range .Highlights}}
- {{.}}
{{- end}}
{{- end}}
{{- if .DataRows}}

[프로필]
{{- range .DataRows}}
{{- if .Multiline}}
- {{.Label}}:
{{.Value}}
{{- else}}
- {{.Label}}: {{.Value}}
{{- end}}
{{- end}}
{{- end}}
{{- if .SocialLinks}}

[링크]
{{- range .SocialLinks}}
- {{.Label}}: {{.URL}}
{{- end}}
{{- end}}
{{- if .OfficialURL}}

공식 프로필: {{.OfficialURL}}
{{- end -}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_ALARM_LIST', NULL, '{{- if eq .Count 0 -}}
🔔 설정된 알람이 없습니다.
예) {{.Prefix}}알람 추가 페코라
{{- else -}}
🔔 알람 ({{.Count}})
{{range $index, $alarm := .Alarms}}
{{add $index 1}}. {{$alarm.MemberName}}{{if $alarm.TypesLabel}} ({{$alarm.TypesLabel}}){{end}}
{{- if $alarm.NextStream}}
{{- if eq $alarm.NextStream.Status "live"}}
   🔴 방송 중
{{- if $alarm.NextStream.Title}}
   {{$alarm.NextStream.Title}}
{{- end}}
{{- if $alarm.NextStream.URL}}
   {{$alarm.NextStream.URL}}
{{- end}}
{{- else if eq $alarm.NextStream.Status "upcoming"}}
   ⏰ {{if $alarm.NextStream.StartingSoon}}곧 시작{{else}}{{$alarm.NextStream.ScheduledKST}}{{if $alarm.NextStream.TimeDetail}} ({{$alarm.NextStream.TimeDetail}}){{end}}{{end}}
{{- if $alarm.NextStream.Title}}
   {{$alarm.NextStream.Title}}
{{- end}}
{{- if $alarm.NextStream.URL}}
   {{$alarm.NextStream.URL}}
{{- end}}
{{- end}}
{{- end}}
{{- end}}
{{- end -}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_ALARM_ADDED', NULL, '{{- if .Added -}}
✅ {{.MemberName}} 알람을 설정했습니다. 방송 시작 5분 전에 알립니다.
{{- if .NextStream}}
{{- if eq .NextStream.Status "live"}}
  🔴 방송 중
{{- if .NextStream.Title}}
  {{.NextStream.Title}}
{{- end}}
{{- if .NextStream.URL}}
  {{.NextStream.URL}}
{{- end}}
{{- else if eq .NextStream.Status "upcoming"}}
  ⏰ {{if .NextStream.StartingSoon}}곧 시작{{else}}{{.NextStream.ScheduledKST}}{{if .NextStream.TimeDetail}} ({{.NextStream.TimeDetail}}){{end}}{{end}}
{{- if .NextStream.Title}}
  {{.NextStream.Title}}
{{- end}}
{{- if .NextStream.URL}}
  {{.NextStream.URL}}
{{- end}}
{{- end}}
{{- end}}
{{- else -}}
ℹ️ {{.MemberName}} 알람이 이미 설정되어 있습니다.
{{- end -}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_ALARM_REMOVED', NULL, '{{- if .Removed -}}
✅ {{.MemberName}} 알람을 해제했습니다.
{{- else -}}
ℹ️ {{.MemberName}} 알람이 설정되어 있지 않습니다.
{{- end -}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_ALARM_CLEARED', NULL, '{{- if eq .Count 0 -}}
🔔 설정된 알람이 없습니다.
{{- else -}}
✅ 알람 {{.Count}}개를 모두 해제했습니다.
{{- end -}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_ALARM_NOTIFICATION', NULL, '⏰ {{.ChannelName}} 방송 예정
{{- if .ScheduledTimeKST}}
  {{.ScheduledTimeKST}} 시작
{{- else}}
  곧 시작
{{- end}}
{{- if .ScheduleMessage}}
  {{.ScheduleMessage}}
{{- end}}
  {{.Title}}
  {{.URL}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_ALARM_LIVE_STARTED', NULL, '🔴 {{.ChannelName}} 방송 시작
{{- if .ScheduledTimeKST}}
  {{.ScheduledTimeKST}} 시작
{{- end}}
  {{.Title}}
  {{.URL}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_ALARM_NOTIFICATION_GROUP', NULL, '🔔 방송 알림 ({{.Count}})
{{if le .MinutesUntil 0}}방송이 시작되었습니다.{{else if eq (len .ScheduledTimes) 0}}곧 시작합니다.{{else if eq (len .ScheduledTimes) 1}}⏰ {{index .ScheduledTimes 0}}{{else}}⏰ {{join .ScheduledTimes ", "}}{{end}}

{{range $i, $e := .Entries}}{{if $i}}
{{end}}{{$e.Index}}. {{$e.ChannelName | default "알 수 없는 채널"}}{{if $e.ScheduledKST}} ({{$e.ScheduledKST}}){{end}}
{{if $e.Title}}   {{$e.Title}}
{{end}}{{if $e.URL}}   {{$e.URL}}
{{end}}{{end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MILESTONE_ACHIEVED', NULL, '🎉 {{.MemberName}} 구독자 {{.Milestone}}명 달성!')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MILESTONE_APPROACHING', NULL, '📊 {{.MemberName}} 구독자 {{.Milestone}}명까지 {{.Remaining}}명 남았습니다.')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_STATS_COUNT', NULL, '📊 {{.MemberName}} 구독자 {{.Subscribers}}명')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_STATS_GAINERS', NULL, '📊 구독자 증가 순위{{if .Period}} ({{.Period}}){{end}}
{{range .Gainers}}
{{.Rank}}. {{.MemberName}} +{{.Delta}}명{{if .Current}} (현재 {{.Current}}명){{end}}
{{- end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_CALENDAR', NULL, '{{if eq .Count 0}}📅 {{.Year}}년 {{.Month}}월 등록된 기념일이 없습니다.{{else}}📅 {{.Year}}년 {{.Month}}월 기념일 ({{.Count}})
{{range $i, $day := .Days}}{{if $i}}
{{end}}
[{{printf "%02d/%02d" $day.Month $day.Day}}]
{{range $day.Entries}}{{if .IsBirthday}}  🎂 {{.Name}} 생일{{else}}  🎉 {{.Name}} 데뷔 {{.Years}}주년{{end}}
{{end}}{{end}}{{end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MEMBER_NOT_LIVE', NULL, '{{.MemberName}}은(는) 현재 방송 중이 아닙니다.')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MEMBER_NO_UPCOMING', NULL, '{{.MemberName}}은(는) {{.Hours}}시간 이내 예정된 방송이 없습니다.')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MEMBER_NOT_FOUND', NULL, '❌ ''{{.MemberName}}'' 멤버를 찾을 수 없습니다.')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_AMBIGUOUS_MEMBER', NULL, '동일한 이름의 멤버가 여러 명 있습니다.
{{range .Candidates}}{{.Index}}. {{.Name}}
{{end}}
예) {{.Prefix}}{{.CommandExample}} {{.FirstName}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MAJOR_EVENT_WEEKLY_SUMMARY', NULL, '📅 이번 주 행사 ({{.Count}})
{{- if .LLMSummary}}

{{.LLMSummary}}
{{- end}}
{{range $index, $event := .Events}}
{{- if gt $index 0}}

{{- end}}
{{add $index 1}}. {{$event.Title}}
{{- if $event.DateStr}}
   ⏰ {{$event.DateStr}}
{{- end}}
{{- if $event.Members}}
   {{$event.Members}}
{{- end}}
{{- if $event.Link}}
   {{$event.Link}}
{{- end}}
{{- end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MAJOR_EVENT_MONTHLY_SUMMARY', NULL, '📅 이번 달 행사 ({{.Count}})
{{- if .LLMSummary}}

{{.LLMSummary}}
{{- end}}
{{range $index, $event := .Events}}
{{- if gt $index 0}}

{{- end}}
{{add $index 1}}. {{$event.Title}}
{{- if $event.DateStr}}
   ⏰ {{$event.DateStr}}
{{- end}}
{{- if $event.Members}}
   {{$event.Members}}
{{- end}}
{{- if $event.Link}}
   {{$event.Link}}
{{- end}}
{{- end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MAJOR_EVENT_SUBSCRIBED', NULL, '✅ 행사 알림을 켰습니다.
매주 행사 요약이 발송됩니다.')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MAJOR_EVENT_UNSUBSCRIBED', NULL, '✅ 행사 알림을 껐습니다.')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MAJOR_EVENT_ALREADY_SUB', NULL, 'ℹ️ 행사 알림이 이미 켜져 있습니다.')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MAJOR_EVENT_NOT_SUB', NULL, 'ℹ️ 행사 알림이 꺼져 있습니다.
- 설정: {{.Prefix}}행사 켜기')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MAJOR_EVENT_STATUS', NULL, '{{if .IsSubscribed}}🔔{{else}}🔕{{end}} 행사 알림: {{if .IsSubscribed}}켜짐{{else}}꺼짐{{end}}
{{- if .IsSubscribed}}
- 해제: {{.Prefix}}행사 끄기
{{- else}}
- 설정: {{.Prefix}}행사 켜기
{{- end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MAJOR_EVENT_USAGE', NULL, '🔔 행사 알림 명령어
  {{.Prefix}}행사 켜기 / 끄기 / 상태')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MEMBER_NEWS_DIGEST', NULL, '{{- if .Headline -}}
{{.Headline}}
{{- else -}}
📰 멤버 뉴스
{{- end -}}
{{- if eq (len .TopItems) 0 }}
표시할 뉴스가 없습니다.
{{- else }}
{{range $index, $item := .TopItems}}
{{- if gt $index 0 }}

{{- end -}}
{{add $index 1}}. [{{$item.DateText}}] {{$item.Member}} · {{$item.Category}}
   {{$item.Title}}
   {{- if $item.Summary}}
   {{$item.Summary}}
   {{- end}}
   {{$item.SourceURL}}
{{- end}}
{{- if .MoreSummary }}

{{.MoreSummary}}
{{- end }}
{{- end }}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MEMBER_NEWS_NO_MEMBERS', NULL, '📰 뉴스 대상 멤버가 없습니다.
예) {{.Prefix}}알람 추가 페코라')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MEMBER_NEWS_SUBSCRIBED', NULL, '✅ 뉴스 알림을 켰습니다.
매주 월요일 09:00 KST에 발송됩니다.')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MEMBER_NEWS_UNSUBSCRIBED', NULL, '✅ 뉴스 알림을 껐습니다.')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MEMBER_NEWS_ALREADY_SUB', NULL, 'ℹ️ 뉴스 알림이 이미 켜져 있습니다.')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MEMBER_NEWS_NOT_SUB', NULL, 'ℹ️ 뉴스 알림이 이미 꺼져 있습니다.')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MEMBER_NEWS_STATUS', NULL, '{{if .IsSubscribed}}🔔{{else}}🔕{{end}} 뉴스 알림: {{if .IsSubscribed}}켜짐{{else}}꺼짐{{end}}
{{- if .IsSubscribed}}
- 발송: 매주 월요일 09:00 KST
- 해제: {{.Prefix}}뉴스알림 끄기
{{- else}}
- 설정: {{.Prefix}}뉴스알림 켜기
{{- end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

COMMIT;
