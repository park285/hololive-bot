BEGIN;

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('ALARM_DISPATCH_NOTIFICATION', NULL, '{{if .IsStarting}}🔴 **{{mdsafe .MemberName}}** 방송 시작{{else if .IsScheduled}}⏰ **{{mdsafe .MemberName}}** 방송 예정{{else}}⏰ **{{mdsafe .MemberName}}** 방송 {{.MinutesUntil}}분 전{{end}}
{{- $url := .URL}}
{{- $parts := split $url " | "}}
{{- $youtubeURL := or (hasPrefix $url "https://www.youtube.com/watch?") (hasPrefix $url "https://youtube.com/watch?") (hasPrefix $url "https://m.youtube.com/watch?") (hasPrefix $url "https://www.youtube.com/live/") (hasPrefix $url "https://youtube.com/live/") (hasPrefix $url "https://youtu.be/")}}
{{- $trustedURL := or $youtubeURL (hasPrefix $url "https://www.twitch.tv/") (hasPrefix $url "https://twitch.tv/") (hasPrefix $url "https://chzzk.naver.com/live/")}}
{{- $delimiterSafe := and (not (contains $url "\t")) (not (contains $url "\n")) (not (contains $url "\r")) (not (contains $url "(")) (not (contains $url ")")) (not (contains $url "[")) (not (contains $url "]")) (not (contains $url "<")) (not (contains $url ">")) (not (contains $url "\\"))}}
{{- $safeURL := and $url $trustedURL $delimiterSafe (not (contains $url " ")) (not (contains $url "|"))}}
{{- $composite := and (eq (len $parts) 2) $youtubeURL (hasPrefix (index $parts 1) "https://chzzk.naver.com/live/") $delimiterSafe (not (contains (index $parts 0) " ")) (not (contains (index $parts 1) " "))}}
{{- $linkable := and .Title $safeURL}}
{{- if $linkable}}
- [{{mdsafe .Title}}]({{.URL}})
{{- else if .Title}}
- {{mdsafe .Title}}
{{- end}}
{{- if .ScheduleMessage}}
- {{mdsafe .ScheduleMessage}}
{{- end}}
{{- if and .URL (not $linkable)}}
- {{if or $safeURL $composite}}{{.URL}}{{else}}{{mdsafe (replace (replace .URL "\n" " ") "\r" " ")}}{{end}}
{{- end}}'),
('ALARM_DISPATCH_NOTIFICATION_GROUP', NULL, '## {{if .IsStarting}}🔴 방송 시작{{else}}⏰ 방송 {{.MinutesUntil}}분 전{{end}}
{{- range .Entries}}

{{if .IsStarting}}🔴 **{{mdsafe .MemberName}}** 방송 시작{{else if .IsScheduled}}⏰ **{{mdsafe .MemberName}}** 방송 예정{{else}}⏰ **{{mdsafe .MemberName}}** 방송 {{.MinutesUntil}}분 전{{end}}
{{- $url := .URL}}
{{- $parts := split $url " | "}}
{{- $youtubeURL := or (hasPrefix $url "https://www.youtube.com/watch?") (hasPrefix $url "https://youtube.com/watch?") (hasPrefix $url "https://m.youtube.com/watch?") (hasPrefix $url "https://www.youtube.com/live/") (hasPrefix $url "https://youtube.com/live/") (hasPrefix $url "https://youtu.be/")}}
{{- $trustedURL := or $youtubeURL (hasPrefix $url "https://www.twitch.tv/") (hasPrefix $url "https://twitch.tv/") (hasPrefix $url "https://chzzk.naver.com/live/")}}
{{- $delimiterSafe := and (not (contains $url "\t")) (not (contains $url "\n")) (not (contains $url "\r")) (not (contains $url "(")) (not (contains $url ")")) (not (contains $url "[")) (not (contains $url "]")) (not (contains $url "<")) (not (contains $url ">")) (not (contains $url "\\"))}}
{{- $safeURL := and $url $trustedURL $delimiterSafe (not (contains $url " ")) (not (contains $url "|"))}}
{{- $composite := and (eq (len $parts) 2) $youtubeURL (hasPrefix (index $parts 1) "https://chzzk.naver.com/live/") $delimiterSafe (not (contains (index $parts 0) " ")) (not (contains (index $parts 1) " "))}}
{{- $linkable := and .Title $safeURL}}
{{- if $linkable}}
- [{{mdsafe .Title}}]({{.URL}})
{{- else if .Title}}
- {{mdsafe .Title}}
{{- end}}
{{- if .ScheduleMessage}}
- {{mdsafe .ScheduleMessage}}
{{- end}}
{{- if and .URL (not $linkable)}}
- {{if or $safeURL $composite}}{{.URL}}{{else}}{{mdsafe (replace (replace .URL "\n" " ") "\r" " ")}}{{end}}
{{- end}}
{{- end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

COMMIT;
