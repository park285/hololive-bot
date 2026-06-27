-- Migration: 알림 헤더 구분 명확화
-- Date: 2026-01-23
-- Description: 영상 등록 알림 vs 방송 예정 알림 구분을 위해 헤더 텍스트 변경
--   - 방송 예정 알림: '🔔 [채널] 방송 알림' → '⏰ [채널] 방송 예정'

-- CMD_ALARM_NOTIFICATION 템플릿 업데이트 (방송 예정 알림)
UPDATE notification_templates
SET body = '⏰ {{.ChannelName}} 방송 예정
{{- if le .MinutesUntil 0}}
🔔 곧 시작합니다!
{{- end}}
{{- if .ScheduleMessage}}
📅 {{.ScheduleMessage}}
{{- end}}
📺 {{.Title}}
🔗 {{.URL}}',
    updated_at = NOW()
WHERE template_key = 'CMD_ALARM_NOTIFICATION'
  AND channel_id IS NULL;

COMMENT ON TABLE notification_templates IS '알림 메시지 템플릿 - v2: 영상/방송 구분 헤더 적용';
