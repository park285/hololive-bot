-- Migration: 행사 알림 템플릿 기본값 추가
-- Date: 2026-02-13
-- Description:
--   - CMD_MAJOR_EVENT_* 템플릿 누락으로 인해 렌더 실패가 발생하는 문제를 보정
--   - 기본 템플릿만 보강하며, 기존 템플릿/채널 override는 유지

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MAJOR_EVENT_WEEKLY_SUMMARY', NULL, '📅 이번 주 행사 알림 ({{.Count}}개)
{{range $index, $event := .Events}}
{{- if gt $index 0}}

{{- end}}
{{add $index 1}}. {{$event.Title}}
{{- if $event.DateStr}}
   ⏰ {{$event.DateStr}}
{{- end}}
{{- if $event.Members}}
   👥 {{$event.Members}}
{{- end}}
{{- if $event.Link}}
   🔗 {{$event.Link}}
{{- end}}
{{- end}}'),

('CMD_MAJOR_EVENT_SUBSCRIBED', NULL, '✅ 행사 알림을 구독했습니다.
매주 행사 요약을 보내드립니다.'),

('CMD_MAJOR_EVENT_UNSUBSCRIBED', NULL, '✅ 행사 알림 구독을 해제했습니다.'),

('CMD_MAJOR_EVENT_ALREADY_SUB', NULL, 'ℹ️ 이미 행사 알림을 구독 중입니다.'),

('CMD_MAJOR_EVENT_NOT_SUB', NULL, 'ℹ️ 현재 행사 알림을 구독하고 있지 않습니다.
{{.Prefix}}행사알림 켜기 명령으로 구독할 수 있습니다.'),

('CMD_MAJOR_EVENT_STATUS', NULL, '🔔 행사 알림 상태: {{if .IsSubscribed}}구독 중{{else}}미구독{{end}}
{{- if .IsSubscribed}}
해제: {{.Prefix}}행사알림 끄기
{{- else}}
설정: {{.Prefix}}행사알림 켜기
{{- end}}'),

('CMD_MAJOR_EVENT_USAGE', NULL, '🔔 행사 알림 명령어
{{.Prefix}}행사알림 켜기
{{.Prefix}}행사알림 끄기
{{.Prefix}}행사알림 목록')
ON CONFLICT DO NOTHING;
