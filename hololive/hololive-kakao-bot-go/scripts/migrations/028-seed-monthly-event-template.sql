-- Migration: 월간 행사 요약 템플릿 + 기존 weekly LLMSummary 분기 추가
-- Date: 2026-02-14
-- Idempotent: ON CONFLICT DO NOTHING, channel_id IS NULL 조건

-- 1) 월간 요약 템플릿 추가
INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MAJOR_EVENT_MONTHLY_SUMMARY', NULL, '📅 이번 달 행사 요약 ({{.Count}}개)
{{- if .LLMSummary}}

{{.LLMSummary}}

---
{{- end}}
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
{{- end}}')
ON CONFLICT DO NOTHING;

-- 2) 기존 weekly 기본 템플릿에 LLMSummary 분기 추가 (channel override 보호)
UPDATE notification_templates
SET body = '📅 이번 주 행사 알림 ({{.Count}}개)
{{- if .LLMSummary}}

{{.LLMSummary}}

---
{{- end}}
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
{{- end}}',
    updated_at = NOW()
WHERE template_key = 'CMD_MAJOR_EVENT_WEEKLY_SUMMARY'
  AND channel_id IS NULL
  AND body NOT LIKE '%LLMSummary%';

-- Rollback:
-- DELETE FROM notification_templates WHERE template_key = 'CMD_MAJOR_EVENT_MONTHLY_SUMMARY' AND channel_id IS NULL;
-- UPDATE notification_templates SET body = '...(원본)...' WHERE template_key = 'CMD_MAJOR_EVENT_WEEKLY_SUMMARY' AND channel_id IS NULL;
