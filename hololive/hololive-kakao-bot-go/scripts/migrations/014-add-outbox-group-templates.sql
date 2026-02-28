-- Migration: 그룹 알림 템플릿 추가 (여러 항목 묶어서 발송)
-- Date: 2026-01-23

INSERT INTO notification_templates(template_key, channel_id, body) VALUES

('OUTBOX_VIDEO_GROUP', NULL, '{{range $idx, $item := .Items}}{{if gt $idx 0}}

{{end}}{{add $idx 1}}. {{$item.Title | truncate 40}}
   {{$item.URL}}{{end}}'),

('OUTBOX_SHORTS_GROUP', NULL, '{{range $idx, $item := .Items}}{{if gt $idx 0}}

{{end}}{{add $idx 1}}. {{$item.Title | truncate 40}}
   {{$item.URL}}{{end}}'),

('OUTBOX_COMMUNITY_GROUP', NULL, '{{range $idx, $item := .Items}}{{if gt $idx 0}}

{{end}}{{add $idx 1}}. {{$item.ContentText | truncate 40}}
   {{$item.URL}}{{end}}');
