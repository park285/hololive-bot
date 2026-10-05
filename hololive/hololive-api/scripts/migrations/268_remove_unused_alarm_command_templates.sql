-- 봇 API의 AlarmNotification·AlarmNotificationGroup formatter를 지워 세 key를 렌더하는 경로가 없다.
-- 방송 알림은 alarm-worker가 ALARM_DISPATCH_NOTIFICATION·ALARM_DISPATCH_NOTIFICATION_GROUP으로 렌더한다.
-- 채널 override와 revision 이력(ON DELETE CASCADE)까지 함께 지운다. 재적용해도 지울 행이 없다.
DELETE FROM notification_templates
WHERE template_key IN ('CMD_ALARM_NOTIFICATION', 'CMD_ALARM_LIVE_STARTED', 'CMD_ALARM_NOTIFICATION_GROUP');
