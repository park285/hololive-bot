-- 표시 문자의 정리 순서를 보존하면서 중첩 template 함수 호출을 통합합니다.
-- displayline을 지원하는 API/worker와 함께 적용하고 template cache를 갱신해야 합니다.
-- 206 이후 표준 전역 본문만 갱신하며 사용자 지정 본문은 보존합니다.
UPDATE notification_templates AS target
SET body = seed.new_body, updated_at = now()
FROM (VALUES
    ('CMD_LIVE_STREAMS', $old${{- if eq .Count 0 -}}
🔴 방송 중인 스트림이 없습니다.
{{- else -}}
🔴 방송 중 · {{.Count}}개
{{- if gt .Count (len .Streams)}}
전체 {{.Count}}개 중 {{len .Streams}}개 표시
{{- end}}
{{- range $i, $stream := .Streams}}
{{- if gt $i 0}}

──────────
{{- end}}

{{add $i 1}} · {{mdsafe (trim (replace (replace (replace (replace (replace .ChannelName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (trim (replace (replace (replace (replace (replace .Title "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " ")))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$old$, $new${{- if eq .Count 0 -}}
🔴 방송 중인 스트림이 없습니다.
{{- else -}}
🔴 방송 중 · {{.Count}}개
{{- if gt .Count (len .Streams)}}
전체 {{.Count}}개 중 {{len .Streams}}개 표시
{{- end}}
{{- range $i, $stream := .Streams}}
{{- if gt $i 0}}

──────────
{{- end}}

{{add $i 1}} · {{mdsafe (displayline .ChannelName)}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Title))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$new$),
    ('CMD_UPCOMING_STREAMS', $old${{- if eq .Count 0 -}}
📅 {{.Hours}}시간 이내 예정된 방송이 없습니다.
{{- else -}}
📅 예정 방송 · {{.Count}}개
{{.Hours}}시간 이내
{{- if gt .Count (len .Streams)}}
전체 {{.Count}}개 중 {{len .Streams}}개 표시
{{- end}}
{{- range $i, $stream := .Streams}}
{{- if gt $i 0}}

──────────
{{- end}}

{{add $i 1}} · {{mdsafe (trim (replace (replace (replace (replace (replace .ChannelName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}
⏰ {{mdsafe .TimeInfo}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (trim (replace (replace (replace (replace (replace .Title "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " ")))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$old$, $new${{- if eq .Count 0 -}}
📅 {{.Hours}}시간 이내 예정된 방송이 없습니다.
{{- else -}}
📅 예정 방송 · {{.Count}}개
{{.Hours}}시간 이내
{{- if gt .Count (len .Streams)}}
전체 {{.Count}}개 중 {{len .Streams}}개 표시
{{- end}}
{{- range $i, $stream := .Streams}}
{{- if gt $i 0}}

──────────
{{- end}}

{{add $i 1}} · {{mdsafe (displayline .ChannelName)}}
⏰ {{mdsafe .TimeInfo}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Title))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$new$),
    ('CMD_CHANNEL_SCHEDULE', $old${{- if not .ChannelName -}}
❌ 채널 정보를 찾을 수 없습니다.
{{- else if eq .Count 0 -}}
📅 {{mdsafe (trim (replace (replace (replace (replace (replace .ChannelName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}
{{.Days}}일 이내 예정된 방송이 없습니다.
{{- else -}}
📅 {{mdsafe (trim (replace (replace (replace (replace (replace .ChannelName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 일정
{{.Days}}일 이내 · {{.Count}}개
{{- if gt .Count (len .Streams)}}
전체 {{.Count}}개 중 {{len .Streams}}개 표시
{{- end}}
{{- range $i, $stream := .Streams}}
{{- if gt $i 0}}

──────────
{{- end}}

{{add $i 1}} · {{if .IsLive}}🔴 방송 중{{else}}⏰ {{mdsafe .TimeInfo}}{{end}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (trim (replace (replace (replace (replace (replace .Title "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " ")))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$old$, $new${{- if not .ChannelName -}}
❌ 채널 정보를 찾을 수 없습니다.
{{- else if eq .Count 0 -}}
📅 {{mdsafe (displayline .ChannelName)}}
{{.Days}}일 이내 예정된 방송이 없습니다.
{{- else -}}
📅 {{mdsafe (displayline .ChannelName)}} 일정
{{.Days}}일 이내 · {{.Count}}개
{{- if gt .Count (len .Streams)}}
전체 {{.Count}}개 중 {{len .Streams}}개 표시
{{- end}}
{{- range $i, $stream := .Streams}}
{{- if gt $i 0}}

──────────
{{- end}}

{{add $i 1}} · {{if .IsLive}}🔴 방송 중{{else}}⏰ {{mdsafe .TimeInfo}}{{end}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Title))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$new$),
    ('CMD_ALARM_NOTIFICATION', $old$⏰ {{mdsafe (trim (replace (replace (replace (replace (replace .ChannelName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 방송 예정
{{if .ScheduledTimeKST}}{{mdsafe .ScheduledTimeKST}} 시작{{else}}곧 시작{{end}}
{{- if .ScheduleMessage}}
{{printf "\u200b"}}{{replace (replace (mdsafe .ScheduleMessage) "\r\n" "\n") "\n" "\n\u200b"}}
{{- end}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (trim (replace (replace (replace (replace (replace .Title "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " ")))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$old$, $new$⏰ {{mdsafe (displayline .ChannelName)}} 방송 예정
{{if .ScheduledTimeKST}}{{mdsafe .ScheduledTimeKST}} 시작{{else}}곧 시작{{end}}
{{- if .ScheduleMessage}}
{{printf "\u200b"}}{{replace (replace (mdsafe .ScheduleMessage) "\r\n" "\n") "\n" "\n\u200b"}}
{{- end}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Title))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$new$),
    ('CMD_ALARM_LIVE_STARTED', $old$🔴 {{mdsafe (trim (replace (replace (replace (replace (replace .ChannelName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 방송 시작
{{- if .ScheduledTimeKST}}
{{mdsafe .ScheduledTimeKST}} 시작
{{- end}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (trim (replace (replace (replace (replace (replace .Title "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " ")))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$old$, $new$🔴 {{mdsafe (displayline .ChannelName)}} 방송 시작
{{- if .ScheduledTimeKST}}
{{mdsafe .ScheduledTimeKST}} 시작
{{- end}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Title))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$new$),
    ('CMD_ALARM_NOTIFICATION_GROUP', $old$🔔 방송 알림 · {{.Count}}개
{{if le .MinutesUntil 0}}방송이 시작되었습니다.{{else if eq (len .ScheduledTimes) 0}}곧 시작합니다.{{else if eq (len .ScheduledTimes) 1}}⏰ {{mdsafe (index .ScheduledTimes 0)}}{{else}}⏰ {{mdsafe (join .ScheduledTimes ", ")}}{{end}}
{{- range $i, $entry := .Entries}}
{{- if gt $i 0}}

──────────
{{- end}}

{{.Index}} · {{mdsafe (default "알 수 없는 채널" .ChannelName)}}{{if .ScheduledKST}} ({{mdsafe .ScheduledKST}}){{end}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (trim (replace (replace (replace (replace (replace .Title "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " ")))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end}}$old$, $new$🔔 방송 알림 · {{.Count}}개
{{if le .MinutesUntil 0}}방송이 시작되었습니다.{{else if eq (len .ScheduledTimes) 0}}곧 시작합니다.{{else if eq (len .ScheduledTimes) 1}}⏰ {{mdsafe (index .ScheduledTimes 0)}}{{else}}⏰ {{mdsafe (join .ScheduledTimes ", ")}}{{end}}
{{- range $i, $entry := .Entries}}
{{- if gt $i 0}}

──────────
{{- end}}

{{.Index}} · {{mdsafe (default "알 수 없는 채널" .ChannelName)}}{{if .ScheduledKST}} ({{mdsafe .ScheduledKST}}){{end}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Title))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end}}$new$),
    ('OUTBOX_VIDEO', $old${{if eq .Kind "LIVE_STREAM"}}🔴 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 방송 시작{{else if .IsUpcomingPremiere}}🔔 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} {{.MinutesUntilPremiere}}분 후 공개 예정{{else if .IsPremiere}}🔔 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 최초공개{{else}}🔔 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 새 영상{{end}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (trim (replace (replace (replace (replace (replace .Title "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " ")))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$old$, $new${{if eq .Kind "LIVE_STREAM"}}🔴 {{mdsafe (displayline .MemberName)}} 방송 시작{{else if .IsUpcomingPremiere}}🔔 {{mdsafe (displayline .MemberName)}} {{.MinutesUntilPremiere}}분 후 공개 예정{{else if .IsPremiere}}🔔 {{mdsafe (displayline .MemberName)}} 최초공개{{else}}🔔 {{mdsafe (displayline .MemberName)}} 새 영상{{end}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Title))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$new$),
    ('OUTBOX_SHORTS', $old$🔔 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 새 쇼츠
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (trim (replace (replace (replace (replace (replace .Title "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " ")))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$old$, $new$🔔 {{mdsafe (displayline .MemberName)}} 새 쇼츠
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Title))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$new$),
    ('OUTBOX_COMMUNITY', $old$🔔 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 커뮤니티 글
{{- if trim .ContentText}}
{{printf "\u200b"}}{{mdsafe (truncate 100 (trim (replace (replace (replace (replace (replace .ContentText "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " ")))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$old$, $new$🔔 {{mdsafe (displayline .MemberName)}} 커뮤니티 글
{{- if trim .ContentText}}
{{printf "\u200b"}}{{mdsafe (truncate 100 (displayline .ContentText))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$new$),
    ('CELEBRATION_BIRTHDAY_STREAM', $old$🎂 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 생일 방송
{{- if .ScheduledStartKST}}
⏰ {{mdsafe .ScheduledStartKST}}
{{- end}}
{{- if trim .StreamTitle}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (trim (replace (replace (replace (replace (replace .StreamTitle "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " ")))}}
{{- end}}
{{- if .StreamURL}}
{{.StreamURL}}
{{- end}}$old$, $new$🎂 {{mdsafe (displayline .MemberName)}} 생일 방송
{{- if .ScheduledStartKST}}
⏰ {{mdsafe .ScheduledStartKST}}
{{- end}}
{{- if trim .StreamTitle}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .StreamTitle))}}
{{- end}}
{{- if .StreamURL}}
{{.StreamURL}}
{{- end}}$new$),
    ('X_SPACE_STARTED', $old$🔴 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 스페이스 시작
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (trim (replace (replace (replace (replace (replace .Title "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " ")))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$old$, $new$🔴 {{mdsafe (displayline .MemberName)}} 스페이스 시작
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Title))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$new$),
    ('CMD_MILESTONE_APPROACHING', $old$📊 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 구독자 {{.Milestone}}명까지 {{formatNumber .Remaining}}명 남았습니다.$old$, $new$📊 {{mdsafe (displayline .MemberName)}} 구독자 {{.Milestone}}명까지 {{formatNumber .Remaining}}명 남았습니다.$new$),
    ('OUTBOX_MILESTONE', $old$🎉 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} {{mdsafe .Milestone}} 달성$old$, $new$🎉 {{mdsafe (displayline .MemberName)}} {{mdsafe .Milestone}} 달성$new$),
    ('CMD_ALARM_REMOVED', $old${{- if .Removed -}}
✅ {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 알람을 해제했습니다.
{{- else -}}
ℹ️ {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 알람이 설정되어 있지 않습니다.
{{- end -}}$old$, $new${{- if .Removed -}}
✅ {{mdsafe (displayline .MemberName)}} 알람을 해제했습니다.
{{- else -}}
ℹ️ {{mdsafe (displayline .MemberName)}} 알람이 설정되어 있지 않습니다.
{{- end -}}$new$),
    ('CMD_STATS_COUNT', $old$📊 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 구독자 {{.Subscribers}}명$old$, $new$📊 {{mdsafe (displayline .MemberName)}} 구독자 {{.Subscribers}}명$new$),
    ('CMD_PROFILE', $old${{- if eq (len .Names) 0 -}}
👤 멤버 정보
{{- else -}}
👤 {{mdsafe (trim (replace (replace (replace (replace (replace (index .Names 0) "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}
{{- if gt (len .Names) 1}}
별칭: {{mdsafe (trim (replace (replace (replace (replace (replace (join (slice .Names 1) " / ") "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}
{{- end}}
{{- end}}
{{- if .Catchphrase}}
"{{mdsafe (trim (replace (replace (replace (replace (replace .Catchphrase "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}"
{{- end}}
{{- if .Summary}}

{{printf "\u200b"}}{{replace (replace (mdsafe .Summary) "\r\n" "\n") "\n" "\n\u200b"}}
{{- end}}
{{- if .Highlights}}

[하이라이트]
{{- range .Highlights}}
· {{mdsafe (trim (replace (replace (replace (replace (replace . "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}
{{- end}}
{{- end}}
{{- if .DataRows}}

[프로필]
{{- range .DataRows}}
{{- if .Multiline}}
{{mdsafe (trim (replace (replace (replace (replace (replace .Label "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}:
{{printf "\u200b"}}{{replace (replace (mdsafe .Value) "\r\n" "\n") "\n" "\n\u200b"}}
{{- else}}
{{mdsafe (trim (replace (replace (replace (replace (replace .Label "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}: {{mdsafe (trim (replace (replace (replace (replace (replace .Value "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}
{{- end}}
{{- end}}
{{- end}}
{{- if .SocialLinks}}

[링크]
{{- range $i, $link := .SocialLinks}}
{{if gt $i 0}}
{{end}}{{mdsafe (trim (replace (replace (replace (replace (replace .Label "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}
{{.URL}}
{{- end}}
{{- end}}
{{- if .OfficialURL}}

공식 프로필
{{.OfficialURL}}
{{- end -}}$old$, $new${{- if eq (len .Names) 0 -}}
👤 멤버 정보
{{- else -}}
👤 {{mdsafe (displayline (index .Names 0))}}
{{- if gt (len .Names) 1}}
별칭: {{mdsafe (displayline (join (slice .Names 1) " / "))}}
{{- end}}
{{- end}}
{{- if .Catchphrase}}
"{{mdsafe (displayline .Catchphrase)}}"
{{- end}}
{{- if .Summary}}

{{printf "\u200b"}}{{replace (replace (mdsafe .Summary) "\r\n" "\n") "\n" "\n\u200b"}}
{{- end}}
{{- if .Highlights}}

[하이라이트]
{{- range .Highlights}}
· {{mdsafe (displayline .)}}
{{- end}}
{{- end}}
{{- if .DataRows}}

[프로필]
{{- range .DataRows}}
{{- if .Multiline}}
{{mdsafe (displayline .Label)}}:
{{printf "\u200b"}}{{replace (replace (mdsafe .Value) "\r\n" "\n") "\n" "\n\u200b"}}
{{- else}}
{{mdsafe (displayline .Label)}}: {{mdsafe (displayline .Value)}}
{{- end}}
{{- end}}
{{- end}}
{{- if .SocialLinks}}

[링크]
{{- range $i, $link := .SocialLinks}}
{{if gt $i 0}}
{{end}}{{mdsafe (displayline .Label)}}
{{.URL}}
{{- end}}
{{- end}}
{{- if .OfficialURL}}

공식 프로필
{{.OfficialURL}}
{{- end -}}$new$),
    ('CELEBRATION_BIRTHDAY', $old$🎂 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}{{if gt .Ordinal 0}} {{.Ordinal}}번째{{end}} 생일 축하합니다!{{if .ChannelID}}
https://youtube.com/channel/{{.ChannelID}}{{end}}$old$, $new$🎂 {{mdsafe (displayline .MemberName)}}{{if gt .Ordinal 0}} {{.Ordinal}}번째{{end}} 생일 축하합니다!{{if .ChannelID}}
https://youtube.com/channel/{{.ChannelID}}{{end}}$new$),
    ('CELEBRATION_ANNIVERSARY', $old$🎉 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 데뷔 {{.Years}}주년 축하합니다!{{if .ChannelID}}
https://youtube.com/channel/{{.ChannelID}}{{end}}$old$, $new$🎉 {{mdsafe (displayline .MemberName)}} 데뷔 {{.Years}}주년 축하합니다!{{if .ChannelID}}
https://youtube.com/channel/{{.ChannelID}}{{end}}$new$),
    ('CMD_MEMBER_DIRECTORY', $old${{- if eq (len .Groups) 0 -}}
👤 등록된 멤버가 없습니다.
{{- else -}}
👤 멤버 목록 · {{.Total}}명
{{- range $i, $group := .Groups}}
{{- if gt $i 0}}

──────────
{{- end}}

[{{mdsafe (trim (replace (replace (replace (replace (replace .GroupName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}]
{{- range .Members}}
{{- if or .Primary .Secondary}}
· {{if .ShowBoth}}{{mdsafe (trim (replace (replace (replace (replace (replace .Primary "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} ({{mdsafe (trim (replace (replace (replace (replace (replace .Secondary "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}){{else if .Primary}}{{mdsafe (trim (replace (replace (replace (replace (replace .Primary "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}{{else if .Secondary}}{{mdsafe (trim (replace (replace (replace (replace (replace .Secondary "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}{{end}}
{{- end}}
{{- end}}
{{- end}}
{{- end -}}$old$, $new${{- if eq (len .Groups) 0 -}}
👤 등록된 멤버가 없습니다.
{{- else -}}
👤 멤버 목록 · {{.Total}}명
{{- range $i, $group := .Groups}}
{{- if gt $i 0}}

──────────
{{- end}}

[{{mdsafe (displayline .GroupName)}}]
{{- range .Members}}
{{- if or .Primary .Secondary}}
· {{if .ShowBoth}}{{mdsafe (displayline .Primary)}} ({{mdsafe (displayline .Secondary)}}){{else if .Primary}}{{mdsafe (displayline .Primary)}}{{else if .Secondary}}{{mdsafe (displayline .Secondary)}}{{end}}
{{- end}}
{{- end}}
{{- end}}
{{- end -}}$new$),
    ('CMD_MILESTONE_ACHIEVED', $old$🎉 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 구독자 {{.Milestone}}명 달성!$old$, $new$🎉 {{mdsafe (displayline .MemberName)}} 구독자 {{.Milestone}}명 달성!$new$),
    ('CMD_ALARM_ADDED', $old${{- if .Added -}}
✅ {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 알람을 설정했습니다.
방송 시작 5분 전에 알립니다.
{{- if and .NextStream (or (eq .NextStream.Status "live") (eq .NextStream.Status "upcoming"))}}

다음 방송
{{- end}}
{{- if .NextStream}}
{{- if eq .NextStream.Status "live"}}
🔴 방송 중
{{- else if eq .NextStream.Status "upcoming"}}
⏰ {{if .NextStream.StartingSoon}}곧 시작{{else}}{{mdsafe .NextStream.ScheduledKST}}{{if .NextStream.TimeDetail}} ({{mdsafe .NextStream.TimeDetail}}){{end}}{{end}}
{{- end}}
{{- if or (eq .NextStream.Status "live") (eq .NextStream.Status "upcoming")}}
{{- if trim .NextStream.Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (trim (replace (replace (replace (replace (replace .NextStream.Title "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " ")))}}
{{- end}}
{{- if .NextStream.URL}}
{{.NextStream.URL}}
{{- end}}
{{- end}}
{{- end}}
{{- else -}}
ℹ️ {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 알람이 이미 설정되어 있습니다.
{{- end -}}$old$, $new${{- if .Added -}}
✅ {{mdsafe (displayline .MemberName)}} 알람을 설정했습니다.
방송 시작 5분 전에 알립니다.
{{- if and .NextStream (or (eq .NextStream.Status "live") (eq .NextStream.Status "upcoming"))}}

다음 방송
{{- end}}
{{- if .NextStream}}
{{- if eq .NextStream.Status "live"}}
🔴 방송 중
{{- else if eq .NextStream.Status "upcoming"}}
⏰ {{if .NextStream.StartingSoon}}곧 시작{{else}}{{mdsafe .NextStream.ScheduledKST}}{{if .NextStream.TimeDetail}} ({{mdsafe .NextStream.TimeDetail}}){{end}}{{end}}
{{- end}}
{{- if or (eq .NextStream.Status "live") (eq .NextStream.Status "upcoming")}}
{{- if trim .NextStream.Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .NextStream.Title))}}
{{- end}}
{{- if .NextStream.URL}}
{{.NextStream.URL}}
{{- end}}
{{- end}}
{{- end}}
{{- else -}}
ℹ️ {{mdsafe (displayline .MemberName)}} 알람이 이미 설정되어 있습니다.
{{- end -}}$new$),
    ('CMD_MAJOR_EVENT_MONTHLY_SUMMARY', $old$📅 이번 달 행사 · {{.Count}}개
{{- if .LLMSummary}}

{{.LLMSummary}}
{{- end}}
{{- range $i, $event := .Events}}
{{- if gt $i 0}}

──────────
{{- end}}

{{add $i 1}} · {{if trim $event.Title}}{{mdsafe (truncate 64 (trim (replace (replace (replace (replace (replace $event.Title "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " ")))}}{{else}}행사{{end}}
{{- if $event.DateStr}}
⏰ {{mdsafe (trim (replace (replace (replace (replace (replace $event.DateStr "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}
{{- end}}
{{- if $event.Members}}
참여: {{mdsafe (trim (replace (replace (replace (replace (replace $event.Members "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}
{{- end}}
{{- if $event.Link}}
{{$event.Link}}
{{- end}}
{{- end}}$old$, $new$📅 이번 달 행사 · {{.Count}}개
{{- if .LLMSummary}}

{{.LLMSummary}}
{{- end}}
{{- range $i, $event := .Events}}
{{- if gt $i 0}}

──────────
{{- end}}

{{add $i 1}} · {{if trim $event.Title}}{{mdsafe (truncate 64 (displayline $event.Title))}}{{else}}행사{{end}}
{{- if $event.DateStr}}
⏰ {{mdsafe (displayline $event.DateStr)}}
{{- end}}
{{- if $event.Members}}
참여: {{mdsafe (displayline $event.Members)}}
{{- end}}
{{- if $event.Link}}
{{$event.Link}}
{{- end}}
{{- end}}$new$),
    ('CMD_MEMBER_NEWS_DIGEST', $old${{- if trim .Headline -}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (trim (replace (replace (replace (replace (replace .Headline "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " ")))}}
{{- else -}}
📰 멤버 뉴스
{{- end}}
{{- if eq (len .TopItems) 0}}
표시할 뉴스가 없습니다.
{{- else}}
{{- range $i, $item := .TopItems}}
{{- if gt $i 0}}

──────────
{{- end}}

{{add $i 1}} · {{mdsafe (trim (replace (replace (replace (replace (replace $item.Member "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}
{{- if or $item.DateText $item.Category}}
{{if $item.DateText}}{{mdsafe (trim (replace (replace (replace (replace (replace $item.DateText "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}{{end}}{{if and $item.DateText $item.Category}} · {{end}}{{if $item.Category}}{{mdsafe (trim (replace (replace (replace (replace (replace $item.Category "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}{{end}}
{{- end}}
{{- if trim $item.Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (trim (replace (replace (replace (replace (replace $item.Title "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " ")))}}
{{- end}}
{{- if $item.Summary}}
{{printf "\u200b"}}{{mdsafe (trim (replace (replace (replace (replace (replace $item.Summary "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}
{{- end}}
{{- if $item.SourceURL}}
{{$item.SourceURL}}
{{- end}}
{{- end}}
{{- if .MoreSummary}}

{{printf "\u200b"}}{{replace (replace (mdsafe .MoreSummary) "\r\n" "\n") "\n" "\n\u200b"}}
{{- end}}
{{- end}}$old$, $new${{- if trim .Headline -}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Headline))}}
{{- else -}}
📰 멤버 뉴스
{{- end}}
{{- if eq (len .TopItems) 0}}
표시할 뉴스가 없습니다.
{{- else}}
{{- range $i, $item := .TopItems}}
{{- if gt $i 0}}

──────────
{{- end}}

{{add $i 1}} · {{mdsafe (displayline $item.Member)}}
{{- if or $item.DateText $item.Category}}
{{if $item.DateText}}{{mdsafe (displayline $item.DateText)}}{{end}}{{if and $item.DateText $item.Category}} · {{end}}{{if $item.Category}}{{mdsafe (displayline $item.Category)}}{{end}}
{{- end}}
{{- if trim $item.Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline $item.Title))}}
{{- end}}
{{- if $item.Summary}}
{{printf "\u200b"}}{{mdsafe (displayline $item.Summary)}}
{{- end}}
{{- if $item.SourceURL}}
{{$item.SourceURL}}
{{- end}}
{{- end}}
{{- if .MoreSummary}}

{{printf "\u200b"}}{{replace (replace (mdsafe .MoreSummary) "\r\n" "\n") "\n" "\n\u200b"}}
{{- end}}
{{- end}}$new$),
    ('ALARM_DISPATCH_NOTIFICATION_GROUP', $old${{if .IsStarting}}🔴 {{if .AllPremiere}}선행공개{{else}}방송{{end}} 시작{{else}}⏰ {{if .AllPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전{{end}} · {{len .Entries}}개
{{- range $i, $entry := .Entries}}
{{- if gt $i 0}}

──────────
{{- end}}

{{add $i 1}} · {{if .IsStarting}}🔴 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} 시작{{else if .IsScheduled}}⏰ {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} 예정{{else}}⏰ {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전{{end}}
{{- $url := .URL}}
{{- $parts := split $url " | "}}
{{- $shortLinkURL := hasPrefix $url "https://short.holoshi.com/l/"}}
{{- $youtubeURL := or $shortLinkURL (hasPrefix $url "https://www.youtube.com/watch?") (hasPrefix $url "https://youtube.com/watch?") (hasPrefix $url "https://m.youtube.com/watch?") (hasPrefix $url "https://www.youtube.com/live/") (hasPrefix $url "https://youtube.com/live/") (hasPrefix $url "https://youtu.be/")}}
{{- $trustedURL := or $youtubeURL (hasPrefix $url "https://www.twitch.tv/") (hasPrefix $url "https://twitch.tv/") (hasPrefix $url "https://chzzk.naver.com/live/")}}
{{- $delimiterSafe := and (not (contains $url "\t")) (not (contains $url "\n")) (not (contains $url "\r")) (not (contains $url "(")) (not (contains $url ")")) (not (contains $url "[")) (not (contains $url "]")) (not (contains $url "<")) (not (contains $url ">")) (not (contains $url "\\"))}}
{{- $safeURL := and $url $trustedURL $delimiterSafe (not (contains $url " ")) (not (contains $url "|"))}}
{{- $composite := and (eq (len $parts) 2) $youtubeURL (hasPrefix (index $parts 1) "https://chzzk.naver.com/live/") $delimiterSafe (not (contains (index $parts 0) " ")) (not (contains (index $parts 1) " "))}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (trim (replace (replace (replace (replace (replace .Title "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " ")))}}
{{- end}}
{{- if .CollabMembers}}
콜라보: {{mdsafe (trim (replace (replace (replace (replace (replace .CollabMembers "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}
{{- end}}
{{- if .ScheduleMessage}}
{{printf "\u200b"}}{{replace (replace (mdsafe .ScheduleMessage) "\r\n" "\n") "\n" "\n\u200b"}}
{{- end}}
{{- if .URL}}
{{if $composite}}{{index $parts 0}}
{{index $parts 1}}{{else if $safeURL}}{{.URL}}{{else}}{{printf "\u200b"}}{{mdsafe (replace (replace .URL "\n" " ") "\r" " ")}}{{end}}
{{- end}}
{{- end}}$old$, $new${{if .IsStarting}}🔴 {{if .AllPremiere}}선행공개{{else}}방송{{end}} 시작{{else}}⏰ {{if .AllPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전{{end}} · {{len .Entries}}개
{{- range $i, $entry := .Entries}}
{{- if gt $i 0}}

──────────
{{- end}}

{{add $i 1}} · {{if .IsStarting}}🔴 {{mdsafe (displayline .MemberName)}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} 시작{{else if .IsScheduled}}⏰ {{mdsafe (displayline .MemberName)}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} 예정{{else}}⏰ {{mdsafe (displayline .MemberName)}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전{{end}}
{{- $url := .URL}}
{{- $parts := split $url " | "}}
{{- $shortLinkURL := hasPrefix $url "https://short.holoshi.com/l/"}}
{{- $youtubeURL := or $shortLinkURL (hasPrefix $url "https://www.youtube.com/watch?") (hasPrefix $url "https://youtube.com/watch?") (hasPrefix $url "https://m.youtube.com/watch?") (hasPrefix $url "https://www.youtube.com/live/") (hasPrefix $url "https://youtube.com/live/") (hasPrefix $url "https://youtu.be/")}}
{{- $trustedURL := or $youtubeURL (hasPrefix $url "https://www.twitch.tv/") (hasPrefix $url "https://twitch.tv/") (hasPrefix $url "https://chzzk.naver.com/live/")}}
{{- $delimiterSafe := and (not (contains $url "\t")) (not (contains $url "\n")) (not (contains $url "\r")) (not (contains $url "(")) (not (contains $url ")")) (not (contains $url "[")) (not (contains $url "]")) (not (contains $url "<")) (not (contains $url ">")) (not (contains $url "\\"))}}
{{- $safeURL := and $url $trustedURL $delimiterSafe (not (contains $url " ")) (not (contains $url "|"))}}
{{- $composite := and (eq (len $parts) 2) $youtubeURL (hasPrefix (index $parts 1) "https://chzzk.naver.com/live/") $delimiterSafe (not (contains (index $parts 0) " ")) (not (contains (index $parts 1) " "))}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Title))}}
{{- end}}
{{- if .CollabMembers}}
콜라보: {{mdsafe (displayline .CollabMembers)}}
{{- end}}
{{- if .ScheduleMessage}}
{{printf "\u200b"}}{{replace (replace (mdsafe .ScheduleMessage) "\r\n" "\n") "\n" "\n\u200b"}}
{{- end}}
{{- if .URL}}
{{if $composite}}{{index $parts 0}}
{{index $parts 1}}{{else if $safeURL}}{{.URL}}{{else}}{{printf "\u200b"}}{{mdsafe (replace (replace .URL "\n" " ") "\r" " ")}}{{end}}
{{- end}}
{{- end}}$new$),
    ('OUTBOX_VIDEO_GROUP', $old${{if eq .Kind "LIVE_STREAM"}}🔴 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 방송 시작{{else if eq .Kind "NEW_VIDEO"}}🔔 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 새 영상{{else}}🔔 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 알림{{end}} · {{.Count}}개
{{- $n := 0}}
{{- range $item := .Items}}
{{- if or (trim $item.Title) $item.URL}}
{{- if gt $n 0}}

──────────
{{- end}}

{{$n = add $n 1 -}}
{{$n}} · {{if trim $item.Title}}{{mdsafe (truncate 64 (trim (replace (replace (replace (replace (replace $item.Title "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " ")))}}{{else}}영상{{end}}
{{- if $item.URL}}
{{$item.URL}}
{{- end}}
{{- end}}
{{- end}}
{{- if ne $n .Count}}

전체 {{.Count}}개 중 {{$n}}개 표시
{{- end}}$old$, $new${{if eq .Kind "LIVE_STREAM"}}🔴 {{mdsafe (displayline .MemberName)}} 방송 시작{{else if eq .Kind "NEW_VIDEO"}}🔔 {{mdsafe (displayline .MemberName)}} 새 영상{{else}}🔔 {{mdsafe (displayline .MemberName)}} 알림{{end}} · {{.Count}}개
{{- $n := 0}}
{{- range $item := .Items}}
{{- if or (trim $item.Title) $item.URL}}
{{- if gt $n 0}}

──────────
{{- end}}

{{$n = add $n 1 -}}
{{$n}} · {{if trim $item.Title}}{{mdsafe (truncate 64 (displayline $item.Title))}}{{else}}영상{{end}}
{{- if $item.URL}}
{{$item.URL}}
{{- end}}
{{- end}}
{{- end}}
{{- if ne $n .Count}}

전체 {{.Count}}개 중 {{$n}}개 표시
{{- end}}$new$),
    ('OUTBOX_SHORTS_GROUP', $old$🔔 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 새 쇼츠 · {{.Count}}개
{{- $n := 0}}
{{- range $item := .Items}}
{{- if or (trim $item.Title) $item.URL}}
{{- if gt $n 0}}

──────────
{{- end}}

{{$n = add $n 1 -}}
{{$n}} · {{if trim $item.Title}}{{mdsafe (truncate 64 (trim (replace (replace (replace (replace (replace $item.Title "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " ")))}}{{else}}쇼츠{{end}}
{{- if $item.URL}}
{{$item.URL}}
{{- end}}
{{- end}}
{{- end}}
{{- if ne $n .Count}}

전체 {{.Count}}개 중 {{$n}}개 표시
{{- end}}$old$, $new$🔔 {{mdsafe (displayline .MemberName)}} 새 쇼츠 · {{.Count}}개
{{- $n := 0}}
{{- range $item := .Items}}
{{- if or (trim $item.Title) $item.URL}}
{{- if gt $n 0}}

──────────
{{- end}}

{{$n = add $n 1 -}}
{{$n}} · {{if trim $item.Title}}{{mdsafe (truncate 64 (displayline $item.Title))}}{{else}}쇼츠{{end}}
{{- if $item.URL}}
{{$item.URL}}
{{- end}}
{{- end}}
{{- end}}
{{- if ne $n .Count}}

전체 {{.Count}}개 중 {{$n}}개 표시
{{- end}}$new$),
    ('OUTBOX_COMMUNITY_GROUP', $old$🔔 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 커뮤니티 글 · {{.Count}}개
{{- $n := 0}}
{{- range $item := .Items}}
{{- if or (trim $item.ContentText) $item.URL}}
{{- if gt $n 0}}

──────────
{{- end}}

{{$n = add $n 1 -}}
{{$n}} · {{if trim $item.ContentText}}{{mdsafe (truncate 100 (trim (replace (replace (replace (replace (replace $item.ContentText "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " ")))}}{{else}}커뮤니티 글{{end}}
{{- if $item.URL}}
{{$item.URL}}
{{- end}}
{{- end}}
{{- end}}
{{- if ne $n .Count}}

전체 {{.Count}}개 중 {{$n}}개 표시
{{- end}}$old$, $new$🔔 {{mdsafe (displayline .MemberName)}} 커뮤니티 글 · {{.Count}}개
{{- $n := 0}}
{{- range $item := .Items}}
{{- if or (trim $item.ContentText) $item.URL}}
{{- if gt $n 0}}

──────────
{{- end}}

{{$n = add $n 1 -}}
{{$n}} · {{if trim $item.ContentText}}{{mdsafe (truncate 100 (displayline $item.ContentText))}}{{else}}커뮤니티 글{{end}}
{{- if $item.URL}}
{{$item.URL}}
{{- end}}
{{- end}}
{{- end}}
{{- if ne $n .Count}}

전체 {{.Count}}개 중 {{$n}}개 표시
{{- end}}$new$),
    ('CMD_STATS_GAINERS', $old$📊 구독자 증가 순위{{if .Period}} · {{mdsafe (trim (replace (replace (replace (replace (replace .Period "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}{{end}}
{{- range $i, $item := .Gainers}}
{{- if gt $i 0}}

──────────
{{- end}}

{{.Rank}} · {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}
증가: +{{.Delta}}명{{if .Current}} · 현재: {{.Current}}명{{end}}
{{- end}}$old$, $new$📊 구독자 증가 순위{{if .Period}} · {{mdsafe (displayline .Period)}}{{end}}
{{- range $i, $item := .Gainers}}
{{- if gt $i 0}}

──────────
{{- end}}

{{.Rank}} · {{mdsafe (displayline .MemberName)}}
증가: +{{.Delta}}명{{if .Current}} · 현재: {{.Current}}명{{end}}
{{- end}}$new$),
    ('CMD_CALENDAR', $old${{- if eq .Count 0 -}}
📅 {{.Year}}년 {{.Month}}월 등록된 기념일이 없습니다.
{{- else -}}
📅 {{.Year}}년 {{.Month}}월 기념일 · {{.Count}}개
{{- range $i, $day := .Days}}
{{- if gt $i 0}}

──────────
{{- end}}

[{{printf "%02d/%02d" .Month .Day}}]
{{- range .Entries}}
{{if .IsBirthday}}🎂 {{mdsafe (trim (replace (replace (replace (replace (replace .Name "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 생일{{else}}🎉 {{mdsafe (trim (replace (replace (replace (replace (replace .Name "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 데뷔 {{.Years}}주년{{end}}
{{- end}}
{{- end}}
{{- end -}}$old$, $new${{- if eq .Count 0 -}}
📅 {{.Year}}년 {{.Month}}월 등록된 기념일이 없습니다.
{{- else -}}
📅 {{.Year}}년 {{.Month}}월 기념일 · {{.Count}}개
{{- range $i, $day := .Days}}
{{- if gt $i 0}}

──────────
{{- end}}

[{{printf "%02d/%02d" .Month .Day}}]
{{- range .Entries}}
{{if .IsBirthday}}🎂 {{mdsafe (displayline .Name)}} 생일{{else}}🎉 {{mdsafe (displayline .Name)}} 데뷔 {{.Years}}주년{{end}}
{{- end}}
{{- end}}
{{- end -}}$new$),
    ('CMD_MEMBER_NOT_LIVE', $old${{printf "\u200b"}}{{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}: 현재 방송 중이 아닙니다.$old$, $new${{printf "\u200b"}}{{mdsafe (displayline .MemberName)}}: 현재 방송 중이 아닙니다.$new$),
    ('CMD_MEMBER_NO_UPCOMING', $old${{printf "\u200b"}}{{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}: {{.Hours}}시간 이내 예정된 방송이 없습니다.$old$, $new${{printf "\u200b"}}{{mdsafe (displayline .MemberName)}}: {{.Hours}}시간 이내 예정된 방송이 없습니다.$new$),
    ('CMD_MEMBER_NOT_FOUND', $old$❌ '{{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}' 멤버를 찾을 수 없습니다.$old$, $new$❌ '{{mdsafe (displayline .MemberName)}}' 멤버를 찾을 수 없습니다.$new$),
    ('CMD_AMBIGUOUS_MEMBER', $old$동일한 이름의 멤버가 여러 명 있습니다.
{{range .Candidates}}{{.Index}} · {{mdsafe (trim (replace (replace (replace (replace (replace .Name "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}
{{end}}
예) {{mdescape (printf "%s%s %s" .Prefix .CommandExample (trim (replace (replace (replace (replace (replace .FirstName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " ")))}}$old$, $new$동일한 이름의 멤버가 여러 명 있습니다.
{{range .Candidates}}{{.Index}} · {{mdsafe (displayline .Name)}}
{{end}}
예) {{mdescape (printf "%s%s %s" .Prefix .CommandExample (displayline .FirstName))}}$new$),
    ('CMD_MAJOR_EVENT_WEEKLY_SUMMARY', $old$📅 이번 주 행사 · {{.Count}}개
{{- if .LLMSummary}}

{{.LLMSummary}}
{{- end}}
{{- range $i, $event := .Events}}
{{- if gt $i 0}}

──────────
{{- end}}

{{add $i 1}} · {{if trim $event.Title}}{{mdsafe (truncate 64 (trim (replace (replace (replace (replace (replace $event.Title "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " ")))}}{{else}}행사{{end}}
{{- if $event.DateStr}}
⏰ {{mdsafe (trim (replace (replace (replace (replace (replace $event.DateStr "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}
{{- end}}
{{- if $event.Members}}
참여: {{mdsafe (trim (replace (replace (replace (replace (replace $event.Members "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}
{{- end}}
{{- if $event.Link}}
{{$event.Link}}
{{- end}}
{{- end}}$old$, $new$📅 이번 주 행사 · {{.Count}}개
{{- if .LLMSummary}}

{{.LLMSummary}}
{{- end}}
{{- range $i, $event := .Events}}
{{- if gt $i 0}}

──────────
{{- end}}

{{add $i 1}} · {{if trim $event.Title}}{{mdsafe (truncate 64 (displayline $event.Title))}}{{else}}행사{{end}}
{{- if $event.DateStr}}
⏰ {{mdsafe (displayline $event.DateStr)}}
{{- end}}
{{- if $event.Members}}
참여: {{mdsafe (displayline $event.Members)}}
{{- end}}
{{- if $event.Link}}
{{$event.Link}}
{{- end}}
{{- end}}$new$),
    ('ALARM_DISPATCH_NOTIFICATION', $old${{if .IsStarting}}🔴 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} 시작{{else if .IsScheduled}}⏰ {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} 예정{{else}}⏰ {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전{{end}}
{{- $url := .URL}}
{{- $parts := split $url " | "}}
{{- $shortLinkURL := hasPrefix $url "https://short.holoshi.com/l/"}}
{{- $youtubeURL := or $shortLinkURL (hasPrefix $url "https://www.youtube.com/watch?") (hasPrefix $url "https://youtube.com/watch?") (hasPrefix $url "https://m.youtube.com/watch?") (hasPrefix $url "https://www.youtube.com/live/") (hasPrefix $url "https://youtube.com/live/") (hasPrefix $url "https://youtu.be/")}}
{{- $trustedURL := or $youtubeURL (hasPrefix $url "https://www.twitch.tv/") (hasPrefix $url "https://twitch.tv/") (hasPrefix $url "https://chzzk.naver.com/live/")}}
{{- $delimiterSafe := and (not (contains $url "\t")) (not (contains $url "\n")) (not (contains $url "\r")) (not (contains $url "(")) (not (contains $url ")")) (not (contains $url "[")) (not (contains $url "]")) (not (contains $url "<")) (not (contains $url ">")) (not (contains $url "\\"))}}
{{- $safeURL := and $url $trustedURL $delimiterSafe (not (contains $url " ")) (not (contains $url "|"))}}
{{- $composite := and (eq (len $parts) 2) $youtubeURL (hasPrefix (index $parts 1) "https://chzzk.naver.com/live/") $delimiterSafe (not (contains (index $parts 0) " ")) (not (contains (index $parts 1) " "))}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (trim (replace (replace (replace (replace (replace .Title "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " ")))}}
{{- end}}
{{- if .CollabMembers}}
콜라보: {{mdsafe (trim (replace (replace (replace (replace (replace .CollabMembers "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}
{{- end}}
{{- if .ScheduleMessage}}
{{printf "\u200b"}}{{replace (replace (mdsafe .ScheduleMessage) "\r\n" "\n") "\n" "\n\u200b"}}
{{- end}}
{{- if .URL}}
{{if $composite}}{{index $parts 0}}
{{index $parts 1}}{{else if $safeURL}}{{.URL}}{{else}}{{printf "\u200b"}}{{mdsafe (replace (replace .URL "\n" " ") "\r" " ")}}{{end}}
{{- end}}$old$, $new${{if .IsStarting}}🔴 {{mdsafe (displayline .MemberName)}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} 시작{{else if .IsScheduled}}⏰ {{mdsafe (displayline .MemberName)}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} 예정{{else}}⏰ {{mdsafe (displayline .MemberName)}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전{{end}}
{{- $url := .URL}}
{{- $parts := split $url " | "}}
{{- $shortLinkURL := hasPrefix $url "https://short.holoshi.com/l/"}}
{{- $youtubeURL := or $shortLinkURL (hasPrefix $url "https://www.youtube.com/watch?") (hasPrefix $url "https://youtube.com/watch?") (hasPrefix $url "https://m.youtube.com/watch?") (hasPrefix $url "https://www.youtube.com/live/") (hasPrefix $url "https://youtube.com/live/") (hasPrefix $url "https://youtu.be/")}}
{{- $trustedURL := or $youtubeURL (hasPrefix $url "https://www.twitch.tv/") (hasPrefix $url "https://twitch.tv/") (hasPrefix $url "https://chzzk.naver.com/live/")}}
{{- $delimiterSafe := and (not (contains $url "\t")) (not (contains $url "\n")) (not (contains $url "\r")) (not (contains $url "(")) (not (contains $url ")")) (not (contains $url "[")) (not (contains $url "]")) (not (contains $url "<")) (not (contains $url ">")) (not (contains $url "\\"))}}
{{- $safeURL := and $url $trustedURL $delimiterSafe (not (contains $url " ")) (not (contains $url "|"))}}
{{- $composite := and (eq (len $parts) 2) $youtubeURL (hasPrefix (index $parts 1) "https://chzzk.naver.com/live/") $delimiterSafe (not (contains (index $parts 0) " ")) (not (contains (index $parts 1) " "))}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Title))}}
{{- end}}
{{- if .CollabMembers}}
콜라보: {{mdsafe (displayline .CollabMembers)}}
{{- end}}
{{- if .ScheduleMessage}}
{{printf "\u200b"}}{{replace (replace (mdsafe .ScheduleMessage) "\r\n" "\n") "\n" "\n\u200b"}}
{{- end}}
{{- if .URL}}
{{if $composite}}{{index $parts 0}}
{{index $parts 1}}{{else if $safeURL}}{{.URL}}{{else}}{{printf "\u200b"}}{{mdsafe (replace (replace .URL "\n" " ") "\r" " ")}}{{end}}
{{- end}}$new$),
    ('CMD_ALARM_LIST', $old${{- if eq .Count 0 -}}
🔔 설정된 알람이 없습니다.
예) {{mdescape .Prefix}}알람 추가 페코라
{{- else -}}
🔔 설정된 알람 · {{.Count}}개
{{- range $i, $alarm := .Alarms}}
{{- if gt $i 0}}

──────────
{{- end}}

{{add $i 1}} · {{mdsafe (trim (replace (replace (replace (replace (replace $alarm.MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}
{{- if $alarm.TypesLabel}}
알림: {{mdsafe (trim (replace (replace (replace (replace (replace $alarm.TypesLabel "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}
{{- end}}
{{- if $alarm.NextStream}}
{{- if eq $alarm.NextStream.Status "live"}}
🔴 방송 중
{{- else if eq $alarm.NextStream.Status "upcoming"}}
⏰ {{if $alarm.NextStream.StartingSoon}}곧 시작{{else}}{{mdsafe $alarm.NextStream.ScheduledKST}}{{if $alarm.NextStream.TimeDetail}} ({{mdsafe $alarm.NextStream.TimeDetail}}){{end}}{{end}}
{{- end}}
{{- if or (eq $alarm.NextStream.Status "live") (eq $alarm.NextStream.Status "upcoming")}}
{{- if trim $alarm.NextStream.Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (trim (replace (replace (replace (replace (replace $alarm.NextStream.Title "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " ")))}}
{{- end}}
{{- if $alarm.NextStream.URL}}
{{$alarm.NextStream.URL}}
{{- end}}
{{- end}}
{{- end}}
{{- end -}}
{{- end -}}$old$, $new${{- if eq .Count 0 -}}
🔔 설정된 알람이 없습니다.
예) {{mdescape .Prefix}}알람 추가 페코라
{{- else -}}
🔔 설정된 알람 · {{.Count}}개
{{- range $i, $alarm := .Alarms}}
{{- if gt $i 0}}

──────────
{{- end}}

{{add $i 1}} · {{mdsafe (displayline $alarm.MemberName)}}
{{- if $alarm.TypesLabel}}
알림: {{mdsafe (displayline $alarm.TypesLabel)}}
{{- end}}
{{- if $alarm.NextStream}}
{{- if eq $alarm.NextStream.Status "live"}}
🔴 방송 중
{{- else if eq $alarm.NextStream.Status "upcoming"}}
⏰ {{if $alarm.NextStream.StartingSoon}}곧 시작{{else}}{{mdsafe $alarm.NextStream.ScheduledKST}}{{if $alarm.NextStream.TimeDetail}} ({{mdsafe $alarm.NextStream.TimeDetail}}){{end}}{{end}}
{{- end}}
{{- if or (eq $alarm.NextStream.Status "live") (eq $alarm.NextStream.Status "upcoming")}}
{{- if trim $alarm.NextStream.Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline $alarm.NextStream.Title))}}
{{- end}}
{{- if $alarm.NextStream.URL}}
{{$alarm.NextStream.URL}}
{{- end}}
{{- end}}
{{- end}}
{{- end -}}
{{- end -}}$new$)
) AS seed(template_key, old_body, new_body)
WHERE target.template_key = seed.template_key
  AND target.channel_id IS NULL
  AND target.body = seed.old_body
  AND target.body IS DISTINCT FROM seed.new_body;
