INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_PROFILE', NULL, '{{- if eq (len .Names) 0 -}}
📘 멤버 정보
{{- else -}}
📘 {{index .Names 0}}{{if gt (len .Names) 1}} ({{join (slice .Names 1) " / "}}){{end}}
{{- end}}
{{- if .Catchphrase}}
🗣️ {{.Catchphrase}}
{{- end}}
{{- if .Summary}}
{{.Summary}}
{{- end}}
{{- if .Highlights}}

✨ 하이라이트
{{- range .Highlights}}
- {{.}}
{{- end}}
{{- end}}
{{- if .DataRows}}

📋 프로필 데이터
{{- range .DataRows}}
{{- if .Multiline}}
- {{.Label}}:
{{.Value}}
{{- else}}
- {{.Label}}: {{.Value}}
{{- end}}
{{- end}}
{{- end}}
{{- if .SocialLinks}}

🔗 링크
{{- range .SocialLinks}}
- {{.Label}}: {{.URL}}
{{- end}}
{{- end}}
{{- if .OfficialURL}}

🌐 공식 프로필: {{.OfficialURL}}
{{- end -}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_STATS_COUNT', NULL, '📘 {{.MemberName}}

📊 현재 구독자: {{.Subscribers}}명')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_STATS_GAINERS', NULL, '📊 구독자 증가 순위{{if .Period}} ({{.Period}}){{end}}
{{range .Gainers}}
{{.Rank}}위. {{.MemberName}}
    +{{.Delta}}명{{if .Current}} (현재 {{.Current}}명){{end}}
{{end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_CALENDAR', NULL, '{{if eq .Count 0}}📅 {{.Year}}년 {{.Month}}월 기념일

등록된 기념일이 없습니다.{{else}}📅 {{.Year}}년 {{.Month}}월 기념일 ({{.Count}}건)
━━━━━━━━━━━━━━━━
{{range $i, $day := .Days}}{{if $i}}
{{end}}
📌 {{$day.Month}}월 {{$day.Day}}일
{{range $day.Entries}}{{if .IsBirthday}}  🎂 {{.Name}} 생일{{else}}  🎉 {{.Name}} 데뷔 {{.Years}}주년{{end}}
{{end}}{{end}}{{end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MEMBER_NOT_LIVE', NULL, '{{.MemberName}}은(는) 현재 방송 중이 아닙니다.')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MEMBER_NO_UPCOMING', NULL, '{{.MemberName}}은(는) {{.Hours}}시간 이내 예정된 방송이 없습니다.')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MEMBER_NOT_FOUND', NULL, '❌ ''{{.MemberName}}'' 멤버를 찾을 수 없습니다.')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();
