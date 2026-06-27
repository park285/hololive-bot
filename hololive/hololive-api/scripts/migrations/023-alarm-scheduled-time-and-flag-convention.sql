-- Migration: 알람 알림 방송 시각 표시 + 플래그 규약
-- Date: 2026-02-12
-- Description:
--   - 알림 메시지에 방송 시각(ScheduledTimeKST) 조건부 표시 추가
--   - MinutesUntil > 0: "HH:MM 방송예정" 표시
--   - MinutesUntil <= 0 (live catchup): "곧 시작합니다!" fallback

-- CMD_ALARM_NOTIFICATION 템플릿 업데이트 (기본 템플릿만)
UPDATE notification_templates
SET body = '⏰ {{.ChannelName}} 방송 예정
{{- if .ScheduledTimeKST}}
🔔 {{.ScheduledTimeKST}} 방송예정
{{- else}}
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

COMMENT ON TABLE notification_templates IS '알림 메시지 템플릿 - v3: 방송 시각 표시 + 플래그 규약 적용';

-- Rollback (이전 템플릿 복원):
-- UPDATE notification_templates
-- SET body = '⏰ {{.ChannelName}} 방송 예정
-- {{- if le .MinutesUntil 0}}
-- 🔔 곧 시작합니다!
-- {{- end}}
-- {{- if .ScheduleMessage}}
-- 📅 {{.ScheduleMessage}}
-- {{- end}}
-- 📺 {{.Title}}
-- 🔗 {{.URL}}',
--     updated_at = NOW()
-- WHERE template_key = 'CMD_ALARM_NOTIFICATION'
--   AND channel_id IS NULL;
