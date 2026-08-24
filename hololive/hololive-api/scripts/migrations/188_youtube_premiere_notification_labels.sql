INSERT INTO message_strings (namespace, key, value)
VALUES
    ('karing', 'outbox_title_video_premiere', '%d분 후 공개 예정'),
    ('karing', 'outbox_time_video_premiere', '%d분 후 공개'),
    ('karing', 'status_video_premiere', '최초공개')
ON CONFLICT (namespace, key) DO NOTHING;

UPDATE notification_templates
SET body = $template${{if eq .Kind "LIVE_STREAM"}}🔴 **{{mdsafe .MemberName}}** 방송 시작{{else if .IsUpcomingPremiere}}🔔 **{{mdsafe .MemberName}}** {{.MinutesUntilPremiere}}분 후 공개 예정{{else if .IsPremiere}}🔔 **{{mdsafe .MemberName}}** 최초공개{{else}}🔔 **{{mdsafe .MemberName}}** 새 영상{{end}}
{{- if and .Title .URL}}
[{{mdsafe (truncate 50 .Title)}}]({{.URL}})
{{- else if .Title}}
{{mdsafe (truncate 50 .Title)}}
{{- else if .URL}}
{{.URL}}
{{- end}}$template$,
    updated_at = now()
WHERE template_key = 'OUTBOX_VIDEO'
  AND channel_id IS NULL
  AND body = $template${{if eq .Kind "LIVE_STREAM"}}🔴 **{{mdsafe .MemberName}}** 방송 시작{{else}}🔔 **{{mdsafe .MemberName}}** 새 영상{{end}}
{{- if and .Title .URL}}
[{{mdsafe (truncate 50 .Title)}}]({{.URL}})
{{- else if .Title}}
{{mdsafe (truncate 50 .Title)}}
{{- else if .URL}}
{{.URL}}
{{- end}}$template$;
