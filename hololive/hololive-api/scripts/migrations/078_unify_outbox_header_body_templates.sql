UPDATE notification_templates
SET body = E'{{if eq .Kind "LIVE_STREAM"}}📺 {{.MemberName}} 방송 알림{{else}}📺 {{.MemberName}} 새 영상{{end}}\n' || body
WHERE template_key = 'OUTBOX_VIDEO' AND body NOT LIKE '{{if eq .Kind%';

UPDATE notification_templates
SET body = E'📱 {{.MemberName}} 쇼츠 알림\n' || body
WHERE template_key = 'OUTBOX_SHORTS' AND body NOT LIKE '📱 {{.MemberName}} 쇼츠 알림%';

UPDATE notification_templates
SET body = E'📝 {{.MemberName}} 커뮤니티 알림\n' || body
WHERE template_key = 'OUTBOX_COMMUNITY' AND body NOT LIKE '📝 {{.MemberName}} 커뮤니티 알림%';

UPDATE notification_templates
SET body = E'🎉 {{.MemberName}} 마일스톤 알림\n' || body
WHERE template_key = 'OUTBOX_MILESTONE' AND body NOT LIKE '🎉 {{.MemberName}} 마일스톤 알림%';

UPDATE notification_templates
SET body = E'{{if eq .Kind "LIVE_STREAM"}}📺 {{.MemberName}} 방송 알림 ({{.Count}}개){{else if eq .Kind "NEW_VIDEO"}}📺 {{.MemberName}} 새 영상 ({{.Count}}개){{else}}🔔 {{.MemberName}} 알림 ({{.Count}}개){{end}}\n' || body
WHERE template_key = 'OUTBOX_VIDEO_GROUP' AND body NOT LIKE '{{if eq .Kind%';

UPDATE notification_templates
SET body = E'📱 {{.MemberName}} 쇼츠 알림 ({{.Count}}개)\n' || body
WHERE template_key = 'OUTBOX_SHORTS_GROUP' AND body NOT LIKE '📱 {{.MemberName}} 쇼츠 알림 (%';

UPDATE notification_templates
SET body = E'📝 {{.MemberName}} 커뮤니티 알림 ({{.Count}}개)\n' || body
WHERE template_key = 'OUTBOX_COMMUNITY_GROUP' AND body NOT LIKE '📝 {{.MemberName}} 커뮤니티 알림 (%';
