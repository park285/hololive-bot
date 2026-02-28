-- Migration: 모든 템플릿을 DB에 시드 (defaultTemplate 폴백 제거)
-- Date: 2026-01-22

-- 기존 기본 템플릿 삭제 후 전체 재삽입
DELETE FROM notification_templates WHERE channel_id IS NULL;

-- 16개 템플릿 시드 (OUTBOX 4개 + CMD 12개)
INSERT INTO notification_templates(template_key, channel_id, body) VALUES

-- OUTBOX 템플릿 (YouTube 알림)
('OUTBOX_SHORTS', NULL, '{{.Title | truncate 50}}
{{.URL}}'),

('OUTBOX_COMMUNITY', NULL, '{{.ContentText | truncate 100}}
{{.URL}}'),

('OUTBOX_VIDEO', NULL, '{{.Title | truncate 50}}
{{.URL}}'),

('OUTBOX_MILESTONE', NULL, '{{.MemberName}} {{.Milestone}} 돌파!'),

-- CMD 템플릿 (명령어 응답)
('CMD_ALARM_LIST', NULL, '{{- if eq .Count 0 -}}
🔔 설정된 알람이 없습니다.

💡 사용법: {{.Prefix}}알람 추가 [멤버명]
예) {{.Prefix}}알람 추가 페코라
{{- else -}}
🔔 설정된 알람 ({{.Count}}개)
{{range $index, $alarm := .Alarms}}
{{add $index 1}}. {{$alarm.MemberName}}{{if $alarm.TypesLabel}} ({{$alarm.TypesLabel}}){{end}}
{{- if $alarm.NextStream}}
{{- if eq $alarm.NextStream.Status "live"}}
   🔴 현재 방송 중!
{{- if $alarm.NextStream.Title}}
   📺 {{$alarm.NextStream.Title}}
{{- end}}
{{- if $alarm.NextStream.URL}}
   🔗 {{$alarm.NextStream.URL}}
{{- end}}
{{- else if eq $alarm.NextStream.Status "upcoming"}}
   ⏰ {{if $alarm.NextStream.StartingSoon}}곧 시작합니다!{{else}}다음 방송: {{$alarm.NextStream.ScheduledKST}}{{if $alarm.NextStream.TimeDetail}} ({{$alarm.NextStream.TimeDetail}}){{end}}{{end}}
{{- if $alarm.NextStream.Title}}
   📺 {{$alarm.NextStream.Title}}
{{- end}}
{{- if $alarm.NextStream.URL}}
   🔗 {{$alarm.NextStream.URL}}
{{- end}}
{{- end}}
{{- end}}
{{- end}}

💡 {{.Prefix}}알람 제거 [멤버명] 으로 알람 해제
{{- end -}}'),

('CMD_ALARM_NOTIFICATION', NULL, '🔔 {{.ChannelName}} 방송 알림
{{- if le .MinutesUntil 0}}
⏰ 곧 시작합니다!
{{- end}}
{{- if .ScheduleMessage}}
📅 {{.ScheduleMessage}}
{{- end}}
📺 {{.Title}}
🔗 {{.URL}}'),

('CMD_LIVE_STREAMS', NULL, '{{- if eq .Count 0 -}}
🔴 현재 방송 중인 스트림이 없습니다.
{{- else -}}
🔴 현재 라이브 중 ({{.Count}}개)
{{range $index, $stream := .Streams}}
{{- if gt $index 0}}

{{end -}}
📺 {{$stream.ChannelName}}{{if gt $stream.ViewerCount 0}} (👥 {{formatNumberKR $stream.ViewerCount}}명){{end}}
   🎬 {{$stream.Title}}
   🔗 {{$stream.URL}}
{{- end -}}
{{- end -}}'),

('CMD_UPCOMING_STREAMS', NULL, '{{- if eq .Count 0 -}}
📅 {{.Hours}}시간 이내 예정된 방송이 없습니다.
{{- else -}}
📅 예정된 방송 ({{.Hours}}시간 이내, {{.Count}}개)
{{range $index, $stream := .Streams}}
{{- if gt $index 0}}

{{end -}}
📺 {{$stream.ChannelName}}
   🎬 {{$stream.Title}}
   ⏰ {{$stream.TimeInfo}}
   🔗 {{$stream.URL}}
{{- end -}}
{{- end -}}'),

('CMD_HELP', NULL, '🌸 홀로라이브 카카오톡 봇

📺 방송 확인
  {{.Prefix}}라이브 - 현재 라이브 중인 방송
  {{.Prefix}}라이브 [멤버명] - 특정 멤버 라이브 확인
  {{.Prefix}}예정 - 24시간 이내 예정 방송
  {{.Prefix}}예정 [멤버명] - 특정 멤버 예정 방송
  {{.Prefix}}멤버 [이름] - 일주일 이내의 방송일정을 조회

👤 멤버 정보
  {{.Prefix}}정보 [멤버명] - 멤버 프로필 조회

🔔 알람 설정
  {{.Prefix}}알람 추가 [멤버명]
  {{.Prefix}}알람 제거 [멤버명]
  {{.Prefix}}알람 목록
  {{.Prefix}}알람 초기화

📊 통계 
  {{.Prefix}}구독자 [멤버명] - 특정 멤버의 현재 구독자 수
  {{.Prefix}}구독자순위 - 최근 10일간 구독자 증가 순위 TOP 10
  {{.Prefix}}구독자순위 [기간]'),

('CMD_MEMBER_DIRECTORY', NULL, '{{- if eq (len .Groups) 0 -}}
👤 등록된 멤버 정보를 찾을 수 없습니다.
{{- else -}}
👤 멤버 목록 ({{.Total}}명)
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
{{- end -}}'),

('CMD_CHANNEL_SCHEDULE', NULL, '{{- if not .ChannelName -}}
❌ 채널 정보를 찾을 수 없습니다.
{{- else if eq .Count 0 -}}
📅 {{.ChannelName}}
{{.Days}}일 이내 예정된 방송이 없습니다.
{{- else -}}
📅{{if .ChannelName}} {{.ChannelName}}{{end}} 일정 ({{.Days}}일 이내, {{.Count}}개)
{{range $index, $entry := .Streams}}
{{- if gt $index 0}}

{{- end}}
{{- if $entry.IsLive}}
🔴 LIVE {{$entry.Title}}
   지금 방송 중
{{- else}}
⏰ {{$entry.Title}}
   {{$entry.TimeInfo}}
{{- end}}
   {{$entry.URL}}
{{- end -}}
{{- end -}}'),

('CMD_ALARM_ADDED', NULL, '{{- if .Added -}}
✅ {{.MemberName}} 알람이 설정되었습니다!
{{- if .NextStream}}
{{- if eq .NextStream.Status "live"}}
   🔴 현재 방송 중!
{{- if .NextStream.Title}}
   📺 {{.NextStream.Title}}
{{- end}}
{{- if .NextStream.URL}}
   🔗 {{.NextStream.URL}}
{{- end}}
{{- else if eq .NextStream.Status "upcoming"}}
   ⏰ {{if .NextStream.StartingSoon}}곧 시작합니다!{{else}}다음 방송: {{.NextStream.ScheduledKST}}{{if .NextStream.TimeDetail}} ({{.NextStream.TimeDetail}}){{end}}{{end}}
{{- if .NextStream.Title}}
   📺 {{.NextStream.Title}}
{{- end}}
{{- if .NextStream.URL}}
   🔗 {{.NextStream.URL}}
{{- end}}
{{- end}}
{{end}}
방송 시작 5분 전에 알림을 받습니다.
{{- else -}}
ℹ️ {{.MemberName}} 알람이 이미 설정되어 있습니다.
{{- end -}}'),

('CMD_ALARM_REMOVED', NULL, '{{- if .Removed -}}
✅ {{.MemberName}} 알람이 해제되었습니다.
{{- else -}}
❌ {{.MemberName}} 알람이 설정되어 있지 않습니다.
{{- end -}}'),

('CMD_ALARM_CLEARED', NULL, '{{- if eq .Count 0 -}}
🔔 설정된 알람이 없습니다.
{{- else -}}
✅ {{.Count}}개의 알람이 모두 해제되었습니다.
{{- end -}}'),

('CMD_MILESTONE_ACHIEVED', NULL, '🎉 {{.MemberName}}님이 구독자 {{.Milestone}}명을 달성했습니다!
축하합니다! 🎊'),

('CMD_MILESTONE_APPROACHING', NULL, '📍 {{.MemberName}}님이 구독자 {{.Milestone}}명까지 {{.Remaining}}명 남았습니다!');

-- 코멘트 업데이트
COMMENT ON TABLE notification_templates IS '알림 메시지 템플릿 - DB가 SSOT, 폴백 없음';
