-- Migration: 알람 알림 방송 시각 절대시간 표기 변경
-- Date: 2026-02-12
-- Description:
--   - 알림 메시지 ScheduledTimeKST 표시 문구를 절대시간 시작 안내로 변경
--   - StartScheduled가 있으면 "HH:MM에 시작합니다" 표시
--   - StartScheduled가 없으면 "곧 시작합니다!" fallback

-- CMD_ALARM_NOTIFICATION 템플릿 업데이트 (기본 템플릿만)
UPDATE notification_templates
SET body = '⏰ {{.ChannelName}} 방송 예정
{{- if .ScheduledTimeKST}}
🔔 {{.ScheduledTimeKST}}에 시작합니다
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

-- Rollback (023 템플릿 문구로 복원):
-- UPDATE notification_templates
-- SET body = '⏰ {{.ChannelName}} 방송 예정
-- {{- if .ScheduledTimeKST}}
-- 🔔 {{.ScheduledTimeKST}} 방송예정
-- {{- else}}
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
