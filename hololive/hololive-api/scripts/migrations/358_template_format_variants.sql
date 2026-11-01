-- 353~357이 만든 format_variant로 기본 템플릿 42개를 방 유형별 본문으로 나눈다.
-- 표준 기본값(347이 쓴 본문, sha256으로 판정)은 plain을 347 이전의 일반 텍스트 본문으로 되돌리고 richtext에는
-- 오픈채팅용 새 본문을 둔다. 사용자 지정 기본값과 채널 override는 지금과 같은 본문이 두 경로에 그대로 나가도록
-- 같은 본문을 richtext 행으로 복사하고 plain 행은 바꾸지 않는다. 행 id·revision·저장된 발송 요청은 건드리지 않는다.
-- 반복 적용은 richtext 행 충돌(DO NOTHING)과 표준 해시 불일치로 아무것도 바꾸지 않는다.
BEGIN;
CREATE TEMP TABLE template_format_seed (
    template_key TEXT PRIMARY KEY,
    standard_sha256 TEXT NOT NULL,
    plain_body TEXT NOT NULL,
    richtext_body TEXT NOT NULL
) ON COMMIT DROP;

INSERT INTO template_format_seed (template_key, standard_sha256, plain_body, richtext_body) VALUES
    ('ALARM_DISPATCH_NOTIFICATION', 'ff64c3d9d728e042e6995743dafede68920863025b0f2ec00f0c9482ccfd8790', $plain${{if .IsStarting}}🔴 {{mdsafe (displayline .MemberName)}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} 시작{{else if .IsScheduled}}⏰ {{mdsafe (displayline .MemberName)}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} 예정{{else}}⏰ {{mdsafe (displayline .MemberName)}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전{{end}}
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
{{- end}}$plain$, $rich${{if .IsStarting}}🔴 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} 시작{{else if .IsScheduled}}⏰ {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} 예정{{else}}⏰ {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전{{end}}
{{- $url := .URL}}
{{- $parts := split $url " | "}}
{{- $shortLinkURL := hasPrefix $url "https://short.holoshi.com/l/"}}
{{- $youtubeURL := or $shortLinkURL (hasPrefix $url "https://www.youtube.com/watch?") (hasPrefix $url "https://youtube.com/watch?") (hasPrefix $url "https://m.youtube.com/watch?") (hasPrefix $url "https://www.youtube.com/live/") (hasPrefix $url "https://youtube.com/live/") (hasPrefix $url "https://youtu.be/")}}
{{- $trustedURL := or $youtubeURL (hasPrefix $url "https://www.twitch.tv/") (hasPrefix $url "https://twitch.tv/") (hasPrefix $url "https://chzzk.naver.com/live/")}}
{{- $delimiterSafe := and (not (contains $url "\t")) (not (contains $url "\n")) (not (contains $url "\r")) (not (contains $url "(")) (not (contains $url ")")) (not (contains $url "[")) (not (contains $url "]")) (not (contains $url "<")) (not (contains $url ">")) (not (contains $url "\\"))}}
{{- $safeURL := and $url $trustedURL $delimiterSafe (not (contains $url " ")) (not (contains $url "|"))}}
{{- $composite := and (eq (len $parts) 2) $youtubeURL (hasPrefix (index $parts 1) "https://chzzk.naver.com/live/") $delimiterSafe (not (contains (index $parts 0) " ")) (not (contains (index $parts 1) " "))}}
{{- with displayline .Title}}
{{mdsafe (truncate 64 .)}}
{{- end}}
{{- if .CollabMembers}}
콜라보: {{mdsafe (displayline .CollabMembers)}}
{{- end}}
{{- if .ScheduleMessage}}
{{replace (mdsafe .ScheduleMessage) "\r\n" "\n"}}
{{- end}}
{{- if .URL}}
{{if $composite}}{{index $parts 0}}
{{index $parts 1}}{{else if $safeURL}}{{.URL}}{{else}}{{mdsafe (replace (replace .URL "\n" " ") "\r" " ")}}{{end}}
{{- end}}$rich$),
    ('ALARM_DISPATCH_NOTIFICATION_GROUP', '11e8c0860bc734a80df5be7ac519a66994e0083ddadf15f1748772e73be64e22', $plain${{if .IsStarting}}🔴 {{if .AllPremiere}}선행공개{{else}}방송{{end}} 시작{{else}}⏰ {{if .AllPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전{{end}} · {{len .Entries}}개
{{- range $i, $entry := .Entries}}
{{if gt $i 0}}──────────{{end}}
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
{{- end}}$plain$, $rich${{if .IsStarting}}🔴 **{{if .AllPremiere}}선행공개{{else}}방송{{end}} 시작**{{else}}⏰ **{{if .AllPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전**{{end}} · {{len .Entries}}개
{{- range $i, $entry := .Entries}}
{{if gt $i 0}}──────────{{end}}
{{with displayline .MemberName}}**{{mdsafe .}}**{{end}} · {{if .IsStarting}}{{if .IsPremiere}}선행공개{{else}}방송{{end}} 시작{{else if .IsScheduled}}{{if .IsPremiere}}선행공개{{else}}방송{{end}} 예정{{else}}{{if .IsPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전{{end}}
{{- $url := .URL}}
{{- $parts := split $url " | "}}
{{- $shortLinkURL := hasPrefix $url "https://short.holoshi.com/l/"}}
{{- $youtubeURL := or $shortLinkURL (hasPrefix $url "https://www.youtube.com/watch?") (hasPrefix $url "https://youtube.com/watch?") (hasPrefix $url "https://m.youtube.com/watch?") (hasPrefix $url "https://www.youtube.com/live/") (hasPrefix $url "https://youtube.com/live/") (hasPrefix $url "https://youtu.be/")}}
{{- $trustedURL := or $youtubeURL (hasPrefix $url "https://www.twitch.tv/") (hasPrefix $url "https://twitch.tv/") (hasPrefix $url "https://chzzk.naver.com/live/")}}
{{- $delimiterSafe := and (not (contains $url "\t")) (not (contains $url "\n")) (not (contains $url "\r")) (not (contains $url "(")) (not (contains $url ")")) (not (contains $url "[")) (not (contains $url "]")) (not (contains $url "<")) (not (contains $url ">")) (not (contains $url "\\"))}}
{{- $safeURL := and $url $trustedURL $delimiterSafe (not (contains $url " ")) (not (contains $url "|"))}}
{{- $composite := and (eq (len $parts) 2) $youtubeURL (hasPrefix (index $parts 1) "https://chzzk.naver.com/live/") $delimiterSafe (not (contains (index $parts 0) " ")) (not (contains (index $parts 1) " "))}}
{{- with displayline .Title}}
{{mdsafe (truncate 64 .)}}
{{- end}}
{{- if .CollabMembers}}
콜라보: {{mdsafe (displayline .CollabMembers)}}
{{- end}}
{{- if .ScheduleMessage}}
{{replace (mdsafe .ScheduleMessage) "\r\n" "\n"}}
{{- end}}
{{- if .URL}}
{{if $composite}}{{index $parts 0}}
{{index $parts 1}}{{else if $safeURL}}{{.URL}}{{else}}{{mdsafe (replace (replace .URL "\n" " ") "\r" " ")}}{{end}}
{{- end}}
{{- end}}$rich$),
    ('CELEBRATION_ANNIVERSARY', '2c92359033da4f3acdba99cafdb2f508fc258579d427e7b24d819a176ca7e4f4', $plain$🎉 {{mdsafe (displayline .MemberName)}} 데뷔 {{.Years}}주년 축하합니다!{{if .ChannelID}}
https://youtube.com/channel/{{.ChannelID}}{{end}}$plain$, $rich$🎉 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 데뷔 {{.Years}}주년 축하합니다!{{if .ChannelID}}
https://youtube.com/channel/{{.ChannelID}}{{end}}$rich$),
    ('CELEBRATION_BIRTHDAY', 'cb46183b0be19ddbd8b0de00c48b0b3ba19f465fabe7fed32d05451d19471868', $plain$🎂 {{mdsafe (displayline .MemberName)}}{{if gt .Ordinal 0}} {{.Ordinal}}번째{{end}} 생일 축하합니다!{{if .ChannelID}}
https://youtube.com/channel/{{.ChannelID}}{{end}}$plain$, $rich$🎂 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}}{{if gt .Ordinal 0}} {{.Ordinal}}번째{{end}} 생일 축하합니다!{{if .ChannelID}}
https://youtube.com/channel/{{.ChannelID}}{{end}}$rich$),
    ('CELEBRATION_BIRTHDAY_STREAM', '84bf9e5fe937cb7787c9f8e5ba3c20ec04d00ed21e157b5a1e3cf6c5b252b463', $plain$🎂 {{mdsafe (displayline .MemberName)}} 생일 방송
{{- if .ScheduledStartKST}}
⏰ {{mdsafe .ScheduledStartKST}}
{{- end}}
{{- if trim .StreamTitle}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .StreamTitle))}}
{{- end}}
{{- if .StreamURL}}
{{.StreamURL}}
{{- end}}$plain$, $rich$🎂 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 생일 방송{{if .ScheduledStartKST}} · {{mdsafe .ScheduledStartKST}}{{end}}
{{- with displayline .StreamTitle}}
{{mdsafe (truncate 64 .)}}
{{- end}}
{{- if .StreamURL}}
{{.StreamURL}}
{{- end}}$rich$),
    ('CMD_ALARM_ADDED', 'e18edc4d8efa0169f33b29377ff3779641d7b7810576fddad42d31ba10897d17', $plain${{- if .Added -}}
✅ {{mdsafe (displayline .MemberName)}} 알람을 설정했습니다.
방송 시작 5분 전에 알립니다.
{{- else -}}
ℹ️ {{mdsafe (displayline .MemberName)}} 알람이 이미 설정되어 있습니다.
{{- end -}}$plain$, $rich${{- if .Added -}}
✅ {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 알람을 설정했습니다.
방송 시작 5분 전에 알립니다.
{{- else -}}
ℹ️ {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 알람이 이미 설정되어 있습니다.
{{- end -}}$rich$),
    ('CMD_ALARM_CLEARED', 'fa1a3f136428c95776a9b11228d7d6fa269e1673254a520ef552c1284f0895e0', $plain${{- if eq .Count 0 -}}
🔔 설정된 알람이 없습니다.
{{- else -}}
✅ 알람 {{.Count}}개를 모두 해제했습니다.
{{- end -}}$plain$, $rich${{- if eq .Count 0 -}}
🔔 설정된 알람이 없습니다.
{{- else -}}
✅ 알람 {{.Count}}개를 모두 해제했습니다.
{{- end -}}$rich$),
    ('CMD_ALARM_LIST', 'e583aa53f502093f22bceee7920c055de09776d02fdfd3cb42e48f52de67020f', $plain${{- if eq .Count 0 -}}
🔔 설정된 알람이 없습니다.
예) {{mdescape .Prefix}}알람 추가 페코라
{{- else -}}
🔔 설정된 알람 · {{.Count}}개
{{- range $i, $alarm := .Alarms}}
{{if eq $i 0}}
{{end}}{{add $i 1}} · {{mdsafe (displayline $alarm.MemberName)}}{{if $alarm.TypesLabel}} ({{mdsafe (displayline $alarm.TypesLabel)}}){{end}}
{{- end -}}
{{- end -}}$plain$, $rich${{- if eq .Count 0 -}}
🔔 설정된 알람이 없습니다.
예) {{mdescape .Prefix}}알람 추가 페코라
{{- else -}}
🔔 **설정된 알람** · {{.Count}}개
{{- range $i, $alarm := .Alarms}}
{{if eq $i 0}}
{{end}}· {{mdsafe (displayline $alarm.MemberName)}}{{if $alarm.TypesLabel}} ({{mdsafe (displayline $alarm.TypesLabel)}}){{end}}
{{- end -}}
{{- end -}}$rich$),
    ('CMD_ALARM_REMOVED', '2393277d24bb736c00ba758ca9da74c5a407d06b048188ee1f7f0f15520c8ff2', $plain${{- if .Removed -}}
✅ {{mdsafe (displayline .MemberName)}} 알람을 해제했습니다.
{{- else -}}
ℹ️ {{mdsafe (displayline .MemberName)}} 알람이 설정되어 있지 않습니다.
{{- end -}}$plain$, $rich${{- if .Removed -}}
✅ {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 알람을 해제했습니다.
{{- else -}}
ℹ️ {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 알람이 설정되어 있지 않습니다.
{{- end -}}$rich$),
    ('CMD_AMBIGUOUS_MEMBER', 'b5a9b31281ae29105e47c6b46ab01ab7c84765e998206034df362c5938284e15', $plain$동일한 이름의 멤버가 여러 명 있습니다.
{{range .Candidates}}{{.Index}} · {{mdsafe (displayline .Name)}}
{{end}}
예) {{mdescape (printf "%s%s %s" .Prefix .CommandExample (displayline .FirstName))}}$plain$, $rich$같은 이름의 멤버가 여러 명 있습니다.
{{range .Candidates}}· {{mdsafe (displayline .Name)}}
{{end}}
예) {{mdescape (printf "%s%s %s" .Prefix .CommandExample (displayline .FirstName))}}$rich$),
    ('CMD_CALENDAR', '277b7084314d9486b3f2f4eb27d80f2b8c14c11c2f6f18176e61982b5cf32405', $plain${{- if eq .Count 0 -}}
📅 {{.Year}}년 {{.Month}}월 등록된 기념일이 없습니다.
{{- else -}}
📅 {{.Year}}년 {{.Month}}월 기념일 · {{.Count}}개
{{- range $i, $day := .Days}}
{{if gt $i 0}}──────────{{end}}
[{{printf "%02d/%02d" .Month .Day}}]
{{- range .Entries}}
{{if .IsBirthday}}🎂 {{mdsafe (displayline .Name)}} 생일{{else}}🎉 {{mdsafe (displayline .Name)}} 데뷔 {{.Years}}주년{{end}}
{{- end}}
{{- end}}
{{- end -}}$plain$, $rich${{- if eq .Count 0 -}}
📅 {{.Year}}년 {{.Month}}월 등록된 기념일이 없습니다.
{{- else -}}
📅 **{{.Year}}년 {{.Month}}월 기념일** · {{.Count}}개
{{- range $i, $day := .Days}}
{{if gt $i 0}}──────────{{end}}
**{{printf "%02d/%02d" .Month .Day}}**
{{- range .Entries}}
{{if .IsBirthday}}🎂 {{mdsafe (displayline .Name)}} 생일{{else}}🎉 {{mdsafe (displayline .Name)}} 데뷔 {{.Years}}주년{{end}}
{{- end}}
{{- end}}
{{- end -}}$rich$),
    ('CMD_CHANNEL_SCHEDULE', '71230c2082e5f47e78b59c11a60b1bae1fef47c9e146fe1c0f681c3b13da9261', $plain${{- if not .ChannelName -}}
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
{{if gt $i 0}}──────────{{end}}
{{add $i 1}} · {{if .IsLive}}🔴 방송 중{{else}}⏰ {{mdsafe .TimeInfo}}{{end}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Title))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$plain$, $rich${{- if not (displayline .ChannelName) -}}
❌ 채널 정보를 찾을 수 없습니다.
{{- else if eq .Count 0 -}}
📅 {{with displayline .ChannelName}}**{{mdsafe .}}**{{end}}
{{.Days}}일 이내 예정된 방송이 없습니다.
{{- else -}}
📅 {{with displayline .ChannelName}}**{{mdsafe .}}**{{end}} 일정 · {{.Count}}개
{{.Days}}일 이내{{if gt .Count (len .Streams)}} · 전체 {{.Count}}개 중 {{len .Streams}}개 표시{{end}}
{{- range $i, $stream := .Streams}}
{{if gt $i 0}}──────────{{end}}
{{if .IsLive}}🔴 **방송 중**{{else}}**{{mdsafe .TimeInfo}}**{{end}}
{{- with displayline .Title}}
{{mdsafe (truncate 64 .)}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$rich$),
    ('CMD_HELP', 'cb17c39598bee9505da34d18fa447f5af83338637aacb8a5e7dcd28989187a98', $plain$📖 홀로라이브 봇 명령어

[방송]
{{mdescape .Prefix}}라이브 - 방송 중 목록
{{mdescape .Prefix}}라이브 [멤버명] - 멤버 라이브 확인
{{mdescape .Prefix}}예정 - 예정 방송 목록
{{mdescape .Prefix}}예정 [멤버명] - 멤버 예정 방송
{{mdescape .Prefix}}멤버 [이름] - 일주일 이내 방송 일정

[방송 이력]
{{mdescape .Prefix}}방송이력/방송기록 [멤버명] [타입] - 종료된 방송 이력
{{mdescape .Prefix}}방송이력 경마 30 - 최근 30일 경마
{{mdescape .Prefix}}방송기록 페코라 게임 - 멤버·타입 필터
{{mdescape .Prefix}}방송이력 카테고리:게임 14일 개수:10 - 타입·기간·개수
타입: 게임/잡담/노래/ASMR/멤버십/이벤트/경마/동시시청/뉴스/기타/미분류
{{mdescape .Prefix}}방송이력 썸네일 [video_id] - 종료 방송 썸네일

[멤버]
{{mdescape .Prefix}}멤버 - 전체 멤버 목록
{{mdescape .Prefix}}정보 [멤버명] - 프로필 조회

[알람]
{{mdescape .Prefix}}알람 추가 [멤버명]
{{mdescape .Prefix}}알람 제거 [멤버명]
{{mdescape .Prefix}}알람 목록
{{mdescape .Prefix}}알람 초기화

[뉴스]
{{mdescape .Prefix}}뉴스 - 주간 뉴스 요약
{{mdescape .Prefix}}뉴스알림 켜기 / 끄기 / 상태

[행사]
{{mdescape .Prefix}}행사 - 행사 알림 상태
{{mdescape .Prefix}}행사 켜기 / 끄기

[기념일]
{{mdescape .Prefix}}기념일 - 이번 달 생일·주년
{{mdescape .Prefix}}기념일 다음달 / 저번달

[기타]
{{mdescape .Prefix}}도움말 - 도움말$plain$, $rich$📖 **홀로라이브 봇 명령어**

**방송**
{{mdescape .Prefix}}라이브 - 방송 중 목록
{{mdescape .Prefix}}라이브 [멤버명] - 멤버 라이브 확인
{{mdescape .Prefix}}예정 - 예정 방송 목록
{{mdescape .Prefix}}예정 [멤버명] - 멤버 예정 방송
{{mdescape .Prefix}}멤버 [이름] - 일주일 이내 방송 일정

**방송 이력**
{{mdescape .Prefix}}방송이력/방송기록 [멤버명] [타입] - 종료된 방송 이력
{{mdescape .Prefix}}방송이력 경마 30 - 최근 30일 경마
{{mdescape .Prefix}}방송기록 페코라 게임 - 멤버·타입 필터
{{mdescape .Prefix}}방송이력 카테고리:게임 14일 개수:10 - 타입·기간·개수
타입: 게임/잡담/노래/ASMR/멤버십/이벤트/경마/동시시청/뉴스/기타/미분류
{{mdescape .Prefix}}방송이력 썸네일 [video_id] - 종료 방송 썸네일

**멤버**
{{mdescape .Prefix}}멤버 - 전체 멤버 목록
{{mdescape .Prefix}}정보 [멤버명] - 프로필 조회

**알람**
{{mdescape .Prefix}}알람 추가 [멤버명]
{{mdescape .Prefix}}알람 제거 [멤버명]
{{mdescape .Prefix}}알람 목록
{{mdescape .Prefix}}알람 초기화

**뉴스**
{{mdescape .Prefix}}뉴스 - 주간 뉴스 요약
{{mdescape .Prefix}}뉴스알림 켜기 / 끄기 / 상태

**행사**
{{mdescape .Prefix}}행사 - 행사 알림 상태
{{mdescape .Prefix}}행사 켜기 / 끄기

**기념일**
{{mdescape .Prefix}}기념일 - 이번 달 생일·주년
{{mdescape .Prefix}}기념일 다음달 / 저번달

**기타**
{{mdescape .Prefix}}도움말 - 도움말$rich$),
    ('CMD_LIVE_STREAMS', 'e95a117dfe278b26a197b6b8239daf43a93e6f339f73275e9512a8b19cf8f669', $plain${{- if eq .Count 0 -}}
현재 방송 중인 멤버가 없습니다.
{{- else -}}
🔴 방송 중 · {{.Count}}개
{{- if gt .Count (len .Streams)}}
전체 {{.Count}}개 중 {{len .Streams}}개 표시
{{- end}}
{{- range $i, $stream := .Streams}}
{{if gt $i 0}}──────────{{end}}
{{add $i 1}} · {{mdsafe (displayline .ChannelName)}}{{- if .Elapsed}} · ⏱ {{.Elapsed}}{{- end}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Title))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$plain$, $rich${{- if eq .Count 0 -}}
현재 방송 중인 멤버가 없습니다.
{{- else -}}
🔴 **방송 중** · {{.Count}}개
{{- if gt .Count (len .Streams)}}
전체 {{.Count}}개 중 {{len .Streams}}개 표시
{{- end}}
{{- range $i, $stream := .Streams}}
{{if gt $i 0}}──────────{{end}}
{{with displayline .ChannelName}}**{{mdsafe .}}**{{end}}{{- if .Elapsed}} · {{.Elapsed}}{{- end}}
{{- with displayline .Title}}
{{mdsafe (truncate 64 .)}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$rich$),
    ('CMD_MAJOR_EVENT_ALREADY_SUB', '064fef69a5cdc6b58b2cca670bd488722080d1d469dc6ea5839e42fbb5b0e0e7', $plain$ℹ️ 행사 알림이 이미 켜져 있습니다.$plain$, $rich$ℹ️ 행사 알림이 이미 켜져 있습니다.$rich$),
    ('CMD_MAJOR_EVENT_MONTHLY_SUMMARY', '50a24c9336641a3976ac49809f1abc71503c704fc044ceaa9f6a5e86fc850727', $plain$📅 이번 달 행사 · {{.Count}}개
{{- if .LLMSummary}}

{{.LLMSummary}}
{{- end}}
{{- range $i, $event := .Events}}
{{if gt $i 0}}──────────{{end}}
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
{{- end}}$plain$, $rich$📅 **이번 달 행사** · {{.Count}}개
{{- if .LLMSummary}}

{{.LLMSummary}}
{{- end}}
{{- range $i, $event := .Events}}
{{if gt $i 0}}──────────{{end}}
{{with displayline $event.Title}}**{{mdsafe (truncate 64 .)}}**{{else}}**행사**{{end}}
{{- if $event.DateStr}}
{{mdsafe (displayline $event.DateStr)}}
{{- end}}
{{- if $event.Members}}
참여: {{mdsafe (displayline $event.Members)}}
{{- end}}
{{- if $event.Link}}
{{$event.Link}}
{{- end}}
{{- end}}$rich$),
    ('CMD_MAJOR_EVENT_NOT_SUB', '375756ffa5e4ea073d9ec5380071e8d76e19896d7acb492c58baeb7ecff9d503', $plain$ℹ️ 행사 알림이 꺼져 있습니다.
설정: {{mdescape .Prefix}}행사 켜기$plain$, $rich$ℹ️ 행사 알림이 꺼져 있습니다.
설정: {{mdescape .Prefix}}행사 켜기$rich$),
    ('CMD_MAJOR_EVENT_STATUS', '1ed0aef8f84e24f388ffa3124bf9b12c8cc3774304c8bad3802277c4b8e65d79', $plain${{if .IsSubscribed}}🔔{{else}}🔕{{end}} 행사 알림: {{if .IsSubscribed}}켜짐{{else}}꺼짐{{end}}
{{- if .IsSubscribed}}
해제: {{mdescape .Prefix}}행사 끄기
{{- else}}
설정: {{mdescape .Prefix}}행사 켜기
{{- end}}$plain$, $rich${{if .IsSubscribed}}🔔{{else}}🔕{{end}} 행사 알림: **{{if .IsSubscribed}}켜짐{{else}}꺼짐{{end}}**
{{- if .IsSubscribed}}
해제: {{mdescape .Prefix}}행사 끄기
{{- else}}
설정: {{mdescape .Prefix}}행사 켜기
{{- end}}$rich$),
    ('CMD_MAJOR_EVENT_SUBSCRIBED', '3f194be6d3e0e48f3917fc39390c12e7d2c1635cb6bb962067b913d889736756', $plain$✅ 행사 알림을 켰습니다.
발송: 매주 행사 요약$plain$, $rich$✅ 행사 알림을 켰습니다.
발송: 매주 행사 요약$rich$),
    ('CMD_MAJOR_EVENT_UNSUBSCRIBED', '6434ca52cbb2976cdbf915d9eb9130cc1785c366b35f60643dab5a7a0733920d', $plain$✅ 행사 알림을 껐습니다.$plain$, $rich$✅ 행사 알림을 껐습니다.$rich$),
    ('CMD_MAJOR_EVENT_USAGE', '1b1f548239ec8bded12f925a827650155441668f29f7b10d021d570fe26c0eb7', $plain$🔔 행사 알림 명령어
{{mdescape .Prefix}}행사 켜기 / 끄기 / 상태$plain$, $rich$🔔 **행사 알림 명령어**
{{mdescape .Prefix}}행사 켜기 / 끄기 / 상태$rich$),
    ('CMD_MAJOR_EVENT_WEEKLY_SUMMARY', '9c84ba7adf5969ad52cefb2bbef1c2b24b1ab74bbd2de9e0340c0a88ca477f7e', $plain$📅 이번 주 행사 · {{.Count}}개
{{- if .LLMSummary}}

{{.LLMSummary}}
{{- end}}
{{- range $i, $event := .Events}}
{{if gt $i 0}}──────────{{end}}
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
{{- end}}$plain$, $rich$📅 **이번 주 행사** · {{.Count}}개
{{- if .LLMSummary}}

{{.LLMSummary}}
{{- end}}
{{- range $i, $event := .Events}}
{{if gt $i 0}}──────────{{end}}
{{with displayline $event.Title}}**{{mdsafe (truncate 64 .)}}**{{else}}**행사**{{end}}
{{- if $event.DateStr}}
{{mdsafe (displayline $event.DateStr)}}
{{- end}}
{{- if $event.Members}}
참여: {{mdsafe (displayline $event.Members)}}
{{- end}}
{{- if $event.Link}}
{{$event.Link}}
{{- end}}
{{- end}}$rich$),
    ('CMD_MEMBER_DIRECTORY', 'ee69f630ba3875bd3ec61c645b64185c7c4c9eccf0564fc84143464f1954dd16', $plain${{- if eq (len .Groups) 0 -}}
👤 등록된 멤버가 없습니다.
{{- else -}}
👤 멤버 목록 · {{.Total}}명
{{- range $i, $group := .Groups}}
{{if gt $i 0}}──────────{{end}}
[{{mdsafe (displayline .GroupName)}}]
{{- range .Members}}
{{- if or .Primary .Secondary}}
· {{if .ShowBoth}}{{mdsafe (displayline .Primary)}} ({{mdsafe (displayline .Secondary)}}){{else if .Primary}}{{mdsafe (displayline .Primary)}}{{else if .Secondary}}{{mdsafe (displayline .Secondary)}}{{end}}
{{- end}}
{{- end}}
{{- end}}
{{- end -}}$plain$, $rich${{- if eq (len .Groups) 0 -}}
👤 등록된 멤버가 없습니다.
{{- else -}}
👤 **멤버 목록** · {{.Total}}명
{{- range $i, $group := .Groups}}
{{if gt $i 0}}──────────{{end}}
{{with displayline .GroupName}}**{{mdsafe .}}**{{end}}
{{- range .Members}}
{{- if or (displayline .Primary) (displayline .Secondary)}}
· {{if and .ShowBoth (displayline .Primary) (displayline .Secondary)}}{{mdsafe (displayline .Primary)}} ({{mdsafe (displayline .Secondary)}}){{else if displayline .Primary}}{{mdsafe (displayline .Primary)}}{{else if displayline .Secondary}}{{mdsafe (displayline .Secondary)}}{{end}}
{{- end}}
{{- end}}
{{- end}}
{{- end -}}$rich$),
    ('CMD_MEMBER_NEWS_ALREADY_SUB', '731812491b98b1a3b76a6ba1cca2cee2b08d65d121e55c5d33dbfb375e61e7ed', $plain$ℹ️ 뉴스 알림이 이미 켜져 있습니다.$plain$, $rich$ℹ️ 뉴스 알림이 이미 켜져 있습니다.$rich$),
    ('CMD_MEMBER_NEWS_DIGEST', '37a69fa33ac0d3c564026eb027551c5a4645fc9a18a40fa42b5a7b323c797f57', $plain${{- if trim .Headline -}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Headline))}}
{{- else -}}
📰 멤버 뉴스
{{- end}}
{{- if eq (len .TopItems) 0}}
표시할 뉴스가 없습니다.
{{- else}}
{{- range $i, $item := .TopItems}}
{{if gt $i 0}}──────────{{end}}
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
{{- end}}$plain$, $rich${{- with displayline .Headline -}}
📰 **{{mdsafe (truncate 64 .)}}**
{{- else -}}
📰 **멤버 뉴스**
{{- end}}
{{- if eq (len .TopItems) 0}}
표시할 뉴스가 없습니다.
{{- else}}
{{- range $i, $item := .TopItems}}
{{if gt $i 0}}──────────{{end}}
{{with displayline $item.Member}}**{{mdsafe .}}**{{else}}**뉴스**{{end}}{{if $item.DateText}} · {{mdsafe (displayline $item.DateText)}}{{end}}{{if $item.Category}} · {{mdsafe (displayline $item.Category)}}{{end}}
{{- with displayline $item.Title}}
{{mdsafe (truncate 64 .)}}
{{- end}}
{{- if $item.Summary}}
{{mdsafe (displayline $item.Summary)}}
{{- end}}
{{- if $item.SourceURL}}
{{$item.SourceURL}}
{{- end}}
{{- end}}
{{- if .MoreSummary}}

{{replace (mdsafe .MoreSummary) "\r\n" "\n"}}
{{- end}}
{{- end}}$rich$),
    ('CMD_MEMBER_NEWS_NOT_SUB', '53de7ee9714cfc714239bc0b479d2bc7e4a4b618f270744eaf88581edd9c2a62', $plain$ℹ️ 뉴스 알림이 이미 꺼져 있습니다.$plain$, $rich$ℹ️ 뉴스 알림이 이미 꺼져 있습니다.$rich$),
    ('CMD_MEMBER_NEWS_NO_MEMBERS', '7d4c4bf3ce2cdf4b27053e57365402641fa06a99b86250a64b63ab48ffd6a236', $plain$📰 뉴스 대상 멤버가 없습니다.
예) {{mdescape .Prefix}}알람 추가 페코라$plain$, $rich$📰 뉴스 대상 멤버가 없습니다.
예) {{mdescape .Prefix}}알람 추가 페코라$rich$),
    ('CMD_MEMBER_NEWS_STATUS', '933c76a5ebcfb8688cbabaeaa06da1b98a92bfdad2168bcec253b7260a788a80', $plain${{if .IsSubscribed}}🔔{{else}}🔕{{end}} 뉴스 알림: {{if .IsSubscribed}}켜짐{{else}}꺼짐{{end}}
{{- if .IsSubscribed}}
발송: 매주 월요일 09:00 KST
해제: {{mdescape .Prefix}}뉴스알림 끄기
{{- else}}
설정: {{mdescape .Prefix}}뉴스알림 켜기
{{- end}}$plain$, $rich${{if .IsSubscribed}}🔔{{else}}🔕{{end}} 뉴스 알림: **{{if .IsSubscribed}}켜짐{{else}}꺼짐{{end}}**
{{- if .IsSubscribed}}
발송: 매주 월요일 09:00 KST
해제: {{mdescape .Prefix}}뉴스알림 끄기
{{- else}}
설정: {{mdescape .Prefix}}뉴스알림 켜기
{{- end}}$rich$),
    ('CMD_MEMBER_NEWS_SUBSCRIBED', 'a8dfee65856f553c435212f23fc03e623cac2e5b1ec6a19f04ea11b3c1724e07', $plain$✅ 뉴스 알림을 켰습니다.
발송: 매주 월요일 09:00 KST$plain$, $rich$✅ 뉴스 알림을 켰습니다.
발송: 매주 월요일 09:00 KST$rich$),
    ('CMD_MEMBER_NEWS_UNSUBSCRIBED', '706c9359df898ca28881b368c497cac549e5987d51aeb34a9a86bceec3b71e41', $plain$✅ 뉴스 알림을 껐습니다.$plain$, $rich$✅ 뉴스 알림을 껐습니다.$rich$),
    ('CMD_MEMBER_NOT_FOUND', '3f58a998c03232bfc23e66cf2455246e7f8723258c939eee72f3d1820629d3af', $plain$❌ '{{mdsafe (displayline .MemberName)}}' 멤버를 찾을 수 없습니다.$plain$, $rich$❌ '{{mdsafe (displayline .MemberName)}}' 멤버를 찾을 수 없습니다.$rich$),
    ('CMD_MEMBER_NOT_LIVE', '0a81284b1bef0bda6ce7b8dd08317c8b557208ac3e2040a5dc4cb4ec970e7b4f', $plain${{printf "\u200b"}}{{mdsafe (displayline .MemberName)}}: 현재 방송 중이 아닙니다.$plain$, $rich${{with displayline .MemberName}}**{{mdsafe .}}**{{end}}: 현재 방송 중이 아닙니다.$rich$),
    ('CMD_MEMBER_NO_UPCOMING', '8dda01b69f46230e62216122893eae2662282264980637230909f8f05c82c2ad', $plain${{printf "\u200b"}}{{mdsafe (displayline .MemberName)}}: {{.Hours}}시간 이내 예정된 방송이 없습니다.$plain$, $rich${{with displayline .MemberName}}**{{mdsafe .}}**{{end}}: {{.Hours}}시간 이내 예정된 방송이 없습니다.$rich$),
    ('CMD_PROFILE', '2eb67a166a57c9619c3d1e77e07e422f47c600882a41c90e68e0cbf4403a933f', $plain${{- if eq (len .Names) 0 -}}
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
{{- end -}}$plain$, $rich${{- if eq (len .Names) 0 -}}
👤 **멤버 정보**
{{- else -}}
👤 {{with displayline (index .Names 0)}}**{{mdsafe .}}**{{else}}**멤버 정보**{{end}}
{{- if gt (len .Names) 1}}
별칭: {{mdsafe (displayline (join (slice .Names 1) " / "))}}
{{- end}}
{{- end}}
{{- with displayline .Catchphrase}}
*"{{mdsafe .}}"*
{{- end}}
{{- if .Summary}}

{{replace (mdsafe .Summary) "\r\n" "\n"}}
{{- end}}
{{- if .Highlights}}

**하이라이트**
{{- range .Highlights}}
· {{mdsafe (displayline .)}}
{{- end}}
{{- end}}
{{- if .DataRows}}

**프로필**
{{- range .DataRows}}
{{- if .Multiline}}
{{mdsafe (displayline .Label)}}:
{{replace (mdsafe .Value) "\r\n" "\n"}}
{{- else}}
{{mdsafe (displayline .Label)}}: {{mdsafe (displayline .Value)}}
{{- end}}
{{- end}}
{{- end}}
{{- if .SocialLinks}}

**링크**
{{- range $i, $link := .SocialLinks}}
{{if gt $i 0}}
{{end}}{{mdsafe (displayline .Label)}}
{{.URL}}
{{- end}}
{{- end}}
{{- if .OfficialURL}}

**공식 프로필**
{{.OfficialURL}}
{{- end -}}$rich$),
    ('CMD_UPCOMING_STREAMS', '5c1bb9858b4ffdc5828940f74344feda8298fc1170342ad6e570ecd6ec668c49', $plain${{- if eq .Count 0 -}}
📅 {{.Hours}}시간 이내 예정된 방송이 없습니다.
{{- else -}}
📅 예정 방송 · {{.Count}}개
{{.Hours}}시간 이내
{{- if gt .Count (len .Streams)}}
전체 {{.Count}}개 중 {{len .Streams}}개 표시
{{- end}}
{{- range $i, $stream := .Streams}}
{{if gt $i 0}}──────────{{end}}
{{add $i 1}} · {{mdsafe (displayline .ChannelName)}}
⏰ {{mdsafe .TimeInfo}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Title))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$plain$, $rich${{- if eq .Count 0 -}}
📅 {{.Hours}}시간 이내 예정된 방송이 없습니다.
{{- else -}}
📅 **예정 방송** · {{.Count}}개
{{.Hours}}시간 이내{{if gt .Count (len .Streams)}} · 전체 {{.Count}}개 중 {{len .Streams}}개 표시{{end}}
{{- range $i, $stream := .Streams}}
{{if gt $i 0}}──────────{{end}}
{{with displayline .ChannelName}}**{{mdsafe .}}**{{end}} · {{mdsafe .TimeInfo}}
{{- with displayline .Title}}
{{mdsafe (truncate 64 .)}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$rich$),
    ('OUTBOX_COMMUNITY', 'aa5bce4cc60cdfa06b15e810086570e782a95bf59fc857faa7e3bd5f8b372545', $plain$🔔 {{mdsafe (displayline .MemberName)}} 커뮤니티 글
{{- if trim .ContentText}}
{{printf "\u200b"}}{{mdsafe (truncate 100 (displayline .ContentText))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$plain$, $rich$🔔 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 커뮤니티 글
{{- with displayline .ContentText}}
{{mdsafe (truncate 100 .)}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$rich$),
    ('OUTBOX_COMMUNITY_GROUP', '8b1cba950f4e03ef120ee5f8d5777f735c38506c88e5f06cb9fb0603b389bb06', $plain$🔔 {{mdsafe (displayline .MemberName)}} 커뮤니티 글 · {{.Count}}개
{{- $n := 0}}
{{- range $item := .Items}}
{{- if or (trim $item.ContentText) $item.URL}}
{{if gt $n 0}}──────────{{end}}
{{$n = add $n 1 -}}
{{$n}} · {{if trim $item.ContentText}}{{mdsafe (truncate 100 (displayline $item.ContentText))}}{{else}}커뮤니티 글{{end}}
{{- if $item.URL}}
{{$item.URL}}
{{- end}}
{{- end}}
{{- end}}
{{- if ne $n .Count}}

전체 {{.Count}}개 중 {{$n}}개 표시
{{- end}}$plain$, $rich$🔔 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 커뮤니티 글 · {{.Count}}개
{{- $n := 0}}
{{- range $item := .Items}}
{{- if or (displayline $item.ContentText) $item.URL}}
{{if gt $n 0}}──────────{{end}}
{{$n = add $n 1 -}}
{{with displayline $item.ContentText}}{{mdsafe (truncate 100 .)}}{{else}}커뮤니티 글{{end}}
{{- if $item.URL}}
{{$item.URL}}
{{- end}}
{{- end}}
{{- end}}
{{- if ne $n .Count}}

전체 {{.Count}}개 중 {{$n}}개 표시
{{- end}}$rich$),
    ('OUTBOX_SHORTS', '70c229c2ca579ed16c58449d42337b7181b8c1518203dab4884ac0881db79d74', $plain$🔔 {{mdsafe (displayline .MemberName)}} 새 쇼츠
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Title))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$plain$, $rich$🔔 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 새 쇼츠
{{- with displayline .Title}}
{{mdsafe (truncate 64 .)}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$rich$),
    ('OUTBOX_SHORTS_GROUP', 'f2879608717df6fc454c4c56821c3c558b00c2105aadb2caf70f1f75872509a1', $plain$🔔 {{mdsafe (displayline .MemberName)}} 새 쇼츠 · {{.Count}}개
{{- $n := 0}}
{{- range $item := .Items}}
{{- if or (trim $item.Title) $item.URL}}
{{if gt $n 0}}──────────{{end}}
{{$n = add $n 1 -}}
{{$n}} · {{if trim $item.Title}}{{mdsafe (truncate 64 (displayline $item.Title))}}{{else}}쇼츠{{end}}
{{- if $item.URL}}
{{$item.URL}}
{{- end}}
{{- end}}
{{- end}}
{{- if ne $n .Count}}

전체 {{.Count}}개 중 {{$n}}개 표시
{{- end}}$plain$, $rich$🔔 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 새 쇼츠 · {{.Count}}개
{{- $n := 0}}
{{- range $item := .Items}}
{{- if or (displayline $item.Title) $item.URL}}
{{if gt $n 0}}──────────{{end}}
{{$n = add $n 1 -}}
{{with displayline $item.Title}}{{mdsafe (truncate 64 .)}}{{else}}쇼츠{{end}}
{{- if $item.URL}}
{{$item.URL}}
{{- end}}
{{- end}}
{{- end}}
{{- if ne $n .Count}}

전체 {{.Count}}개 중 {{$n}}개 표시
{{- end}}$rich$),
    ('OUTBOX_VIDEO', '497f2a257583c88418161eccf822e383d1a1d5b382d54ae26506aa908200eeaf', $plain${{if eq .Kind "LIVE_STREAM"}}🔴 {{mdsafe (displayline .MemberName)}} 방송 시작{{else if .IsUpcomingPremiere}}🔔 {{mdsafe (displayline .MemberName)}} {{.MinutesUntilPremiere}}분 후 공개 예정{{else if .IsPremiere}}🔔 {{mdsafe (displayline .MemberName)}} 최초공개{{else}}🔔 {{mdsafe (displayline .MemberName)}} 새 영상{{end}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Title))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$plain$, $rich${{if eq .Kind "LIVE_STREAM"}}🔴 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 방송 시작{{else if .IsUpcomingPremiere}}🔔 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} {{.MinutesUntilPremiere}}분 후 공개 예정{{else if .IsPremiere}}🔔 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 최초공개{{else}}🔔 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 새 영상{{end}}
{{- with displayline .Title}}
{{mdsafe (truncate 64 .)}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$rich$),
    ('OUTBOX_VIDEO_GROUP', '9c0d28528d3856a7f310c2fab21fd2e09d9295467216a2a01ce49de23ace53a2', $plain${{if eq .Kind "LIVE_STREAM"}}🔴 {{mdsafe (displayline .MemberName)}} 방송 시작{{else if eq .Kind "NEW_VIDEO"}}🔔 {{mdsafe (displayline .MemberName)}} 새 영상{{else}}🔔 {{mdsafe (displayline .MemberName)}} 알림{{end}} · {{.Count}}개
{{- $n := 0}}
{{- range $item := .Items}}
{{- if or (trim $item.Title) $item.URL}}
{{if gt $n 0}}──────────{{end}}
{{$n = add $n 1 -}}
{{$n}} · {{if trim $item.Title}}{{mdsafe (truncate 64 (displayline $item.Title))}}{{else}}영상{{end}}
{{- if $item.URL}}
{{$item.URL}}
{{- end}}
{{- end}}
{{- end}}
{{- if ne $n .Count}}

전체 {{.Count}}개 중 {{$n}}개 표시
{{- end}}$plain$, $rich${{if eq .Kind "LIVE_STREAM"}}🔴 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 방송 시작{{else if eq .Kind "NEW_VIDEO"}}🔔 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 새 영상{{else}}🔔 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 알림{{end}} · {{.Count}}개
{{- $n := 0}}
{{- range $item := .Items}}
{{- if or (displayline $item.Title) $item.URL}}
{{if gt $n 0}}──────────{{end}}
{{$n = add $n 1 -}}
{{with displayline $item.Title}}{{mdsafe (truncate 64 .)}}{{else}}영상{{end}}
{{- if $item.URL}}
{{$item.URL}}
{{- end}}
{{- end}}
{{- end}}
{{- if ne $n .Count}}

전체 {{.Count}}개 중 {{$n}}개 표시
{{- end}}$rich$),
    ('X_SPACE_STARTED', 'c6bb390f6524f91ac29fcf124f4bdb971c517a4e18d615c3c4dccba6f1f5910e', $plain$🔴 {{mdsafe (displayline .MemberName)}} 스페이스 시작
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Title))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$plain$, $rich$🔴 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 스페이스 시작
{{- with displayline .Title}}
{{mdsafe (truncate 64 .)}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$rich$);

INSERT INTO notification_templates (template_key, channel_id, body, format_variant)
SELECT source.template_key,
       source.channel_id,
       CASE
           WHEN source.channel_id IS NULL
            AND encode(sha256(convert_to(source.body, 'UTF8')), 'hex') = seed.standard_sha256
           THEN seed.richtext_body
           ELSE source.body
       END,
       'richtext'
FROM notification_templates AS source
LEFT JOIN template_format_seed AS seed ON seed.template_key = source.template_key
WHERE source.format_variant = 'plain'
ON CONFLICT DO NOTHING;

UPDATE notification_templates AS target
SET body = seed.plain_body, updated_at = now()
FROM template_format_seed AS seed
WHERE target.template_key = seed.template_key
  AND target.channel_id IS NULL
  AND target.format_variant = 'plain'
  AND encode(sha256(convert_to(target.body, 'UTF8')), 'hex') = seed.standard_sha256;
COMMIT;
