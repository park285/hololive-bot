BEGIN;

INSERT INTO notification_templates(template_key, channel_id, body) VALUES
('CMD_MEMBER_NEWS_SUBSCRIBED', NULL, '✅ 뉴스 알림을 켰습니다.
- 발송: **매주 월요일 09:00 KST**'),
('CMD_MAJOR_EVENT_SUBSCRIBED', NULL, '✅ 행사 알림을 켰습니다.
- 발송: **매주 행사 요약**'),
('CMD_ALARM_CLEARED', NULL, '{{- if eq .Count 0 -}}
🔔 설정된 알람이 없습니다.
{{- else -}}
✅ 알람 **{{.Count}}개**를 모두 해제했습니다.
{{- end -}}'),
('CMD_PROFILE', NULL, '{{- if eq (len .Names) 0 -}}
## 👤 멤버 정보
{{- else -}}
## 👤 {{mdsafe (index .Names 0)}}{{if gt (len .Names) 1}} ({{mdsafe (join (slice .Names 1) " / ")}}){{end}}
{{- end}}
{{- if .Catchphrase}}
"{{mdsafe .Catchphrase}}"
{{- end}}
{{- if .Summary}}
{{mdsafe .Summary}}
{{- end}}
{{- if .Highlights}}

**하이라이트**
{{- range .Highlights}}
- {{mdsafe .}}
{{- end}}
{{- end}}
{{- if .DataRows}}

**프로필**
{{- range .DataRows}}
{{- if .Multiline}}
- {{mdsafe .Label}}:
{{mdsafe .Value}}
{{- else}}
- {{mdsafe .Label}}: {{mdsafe .Value}}
{{- end}}
{{- end}}
{{- end}}
{{- if .SocialLinks}}

**링크**
{{- range .SocialLinks}}
- [{{mdsafe .Label}}]({{.URL}})
{{- end}}
{{- end}}
{{- if .OfficialURL}}

[공식 프로필]({{.OfficialURL}})
{{- end -}}'),
('OUTBOX_COMMUNITY', NULL, '🔔 **{{mdsafe .MemberName}}** 커뮤니티 글
{{- if .ContentText}}
{{mdsafe (truncate 100 .ContentText)}}
{{- end}}
{{- if .URL}}
[커뮤니티 글 보기]({{.URL}})
{{- end}}'),
('OUTBOX_COMMUNITY_GROUP', NULL, '## 🔔 {{mdsafe .MemberName}} 커뮤니티 글 ({{.Count}})
{{- $n := 0}}
{{- range $item := .Items}}
{{- if $item.ContentText}}
{{- $n = add $n 1}}
{{$n}}. {{mdsafe (truncate 40 $item.ContentText)}}
{{- if $item.URL}}
   [커뮤니티 글 보기]({{$item.URL}})
{{- end}}
{{- else if $item.URL}}
{{- $n = add $n 1}}
{{$n}}. [커뮤니티 글 보기]({{$item.URL}})
{{- end}}
{{- end}}'),
('CELEBRATION_BIRTHDAY', NULL, '🎂 **{{mdsafe .MemberName}}**{{if gt .Ordinal 0}} {{.Ordinal}}번째{{end}} 생일 축하합니다!{{if .ChannelID}}
[YouTube 채널 보기](https://youtube.com/channel/{{.ChannelID}}){{end}}'),
('CELEBRATION_ANNIVERSARY', NULL, '🎉 **{{mdsafe .MemberName}}** 데뷔 {{.Years}}주년 축하합니다!{{if .ChannelID}}
[YouTube 채널 보기](https://youtube.com/channel/{{.ChannelID}}){{end}}')
ON CONFLICT (template_key) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body, updated_at = now();

COMMIT;
