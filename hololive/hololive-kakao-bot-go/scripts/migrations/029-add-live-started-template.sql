-- Migration: live catch-up 전용 템플릿 추가
-- Date: 2026-02-16

UPDATE notification_templates
SET
    body = '🔔 {{.ChannelName}} 방송 시작됨
{{- if .ScheduledTimeKST}}
⏰ {{.ScheduledTimeKST}}에 시작했습니다
{{- else}}
⏰ 이미 시작했습니다
{{- end}}
📺 {{.Title}}
🔗 {{.URL}}',
    updated_at = NOW()
WHERE template_key = 'CMD_ALARM_LIVE_STARTED'
  AND channel_id IS NULL;

INSERT INTO notification_templates(template_key, channel_id, body)
SELECT
    'CMD_ALARM_LIVE_STARTED',
    NULL,
    '🔔 {{.ChannelName}} 방송 시작됨
{{- if .ScheduledTimeKST}}
⏰ {{.ScheduledTimeKST}}에 시작했습니다
{{- else}}
⏰ 이미 시작했습니다
{{- end}}
📺 {{.Title}}
🔗 {{.URL}}'
WHERE NOT EXISTS (
    SELECT 1
    FROM notification_templates
    WHERE template_key = 'CMD_ALARM_LIVE_STARTED'
      AND channel_id IS NULL
);

COMMENT ON TABLE notification_templates IS '알림 메시지 템플릿 - live catch-up 전용 템플릿 추가';
