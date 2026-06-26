-- Migration: member news 명령어 템플릿 추가
-- Date: 2026-02-16

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MEMBER_NEWS_DIGEST', NULL, '{{- if .Headline -}}
{{.Headline}}
{{- else -}}
🗞️ 구독 멤버 뉴스
{{- end -}}
{{- if eq (len .TopItems) 0 }}
- 표시할 뉴스가 없습니다.
{{- else }}
{{range $index, $item := .TopItems}}
{{- if gt $index 0 }}

{{- end -}}
{{add $index 1}}. [{{$item.DateText}}] {{$item.Member}} | {{$item.Category}}
   📌 {{$item.Title}}
   🔗 {{$item.SourceURL}}
{{- end}}
{{- if .MoreSummary }}

{{.MoreSummary}}
{{- end }}
{{- end }}'),

('CMD_MEMBER_NEWS_NO_MEMBERS', NULL, '🗞️ 뉴스 대상 멤버가 없습니다.
먼저 {{.Prefix}}알람 추가 [멤버명] 으로 멤버를 등록해주세요.'),

('CMD_MEMBER_NEWS_SUBSCRIBED', NULL, '✅ 뉴스 알림을 켰습니다.
매주 월요일 09:00 KST에 자동 발송됩니다.'),

('CMD_MEMBER_NEWS_UNSUBSCRIBED', NULL, '✅ 뉴스 알림을 껐습니다.'),

('CMD_MEMBER_NEWS_ALREADY_SUB', NULL, '🔔 뉴스 알림이 이미 켜져 있습니다.'),

('CMD_MEMBER_NEWS_NOT_SUB', NULL, 'ℹ️ 뉴스 알림이 이미 꺼져 있습니다.'),

('CMD_MEMBER_NEWS_STATUS', NULL, '🔔 뉴스 알림 상태: {{if .IsSubscribed}}ON{{else}}OFF{{end}}
{{- if .IsSubscribed}}
- 자동 발송: 매주 월요일 09:00 KST
- 해제: {{.Prefix}}뉴스알림 끄기
{{- else}}
- 설정: {{.Prefix}}뉴스알림 켜기
{{- end}}')
ON CONFLICT DO NOTHING;
