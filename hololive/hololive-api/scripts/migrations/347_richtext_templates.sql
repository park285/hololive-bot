-- 기본 템플릿 42개의 제목·대상·핵심 상태를 RichText 강조로 구분한다.
-- manifest 순서의 최종 표준 본문(322까지)만 전환하며 사용자 지정 기본값과 채널 override는 보존한다.
-- 도움말은 206 본문에서 234가 제거한 구독자 명령을 제외한 값이다. 폐기된 234/268 키는 포함하지 않는다.
-- 동적 데이터는 기존 mdsafe/mdescape 경계를 유지한다. 행사 LLMSummary는 호출자가 한 번 보호한 값을 그대로 받는다.
-- 강조와 제목 유무는 displayline으로 정규화한 값으로 판단해 공백·ZWSP만 있는 입력이 빈 강조 구문을 만들지 않게 한다.
-- 필드 literal 보호와 들여쓰기 처리는 mdsafe가 소유하므로 정적 강조까지 막는 구형 선행/줄별 ZWSP는 새 본문에서 제거한다. 접기 패딩은 변경하지 않는다.
-- URL·복사 명령·저장된 outbox 요청은 바꾸지 않는다. 기존 데이터 migration처럼 body/updated_at만 갱신한다.
UPDATE notification_templates AS target
SET body = seed.new_body, updated_at = now()
FROM (VALUES
    ('ALARM_DISPATCH_NOTIFICATION', $old${{if .IsStarting}}🔴 {{mdsafe (displayline .MemberName)}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} 시작{{else if .IsScheduled}}⏰ {{mdsafe (displayline .MemberName)}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} 예정{{else}}⏰ {{mdsafe (displayline .MemberName)}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전{{end}}
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
{{- end}}$old$, $new${{if .IsStarting}}🔴 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} 시작{{else if .IsScheduled}}⏰ {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} 예정{{else}}⏰ {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전{{end}}
{{- $url := .URL}}
{{- $parts := split $url " | "}}
{{- $shortLinkURL := hasPrefix $url "https://short.holoshi.com/l/"}}
{{- $youtubeURL := or $shortLinkURL (hasPrefix $url "https://www.youtube.com/watch?") (hasPrefix $url "https://youtube.com/watch?") (hasPrefix $url "https://m.youtube.com/watch?") (hasPrefix $url "https://www.youtube.com/live/") (hasPrefix $url "https://youtube.com/live/") (hasPrefix $url "https://youtu.be/")}}
{{- $trustedURL := or $youtubeURL (hasPrefix $url "https://www.twitch.tv/") (hasPrefix $url "https://twitch.tv/") (hasPrefix $url "https://chzzk.naver.com/live/")}}
{{- $delimiterSafe := and (not (contains $url "\t")) (not (contains $url "\n")) (not (contains $url "\r")) (not (contains $url "(")) (not (contains $url ")")) (not (contains $url "[")) (not (contains $url "]")) (not (contains $url "<")) (not (contains $url ">")) (not (contains $url "\\"))}}
{{- $safeURL := and $url $trustedURL $delimiterSafe (not (contains $url " ")) (not (contains $url "|"))}}
{{- $composite := and (eq (len $parts) 2) $youtubeURL (hasPrefix (index $parts 1) "https://chzzk.naver.com/live/") $delimiterSafe (not (contains (index $parts 0) " ")) (not (contains (index $parts 1) " "))}}
{{- with displayline .Title}}
**{{mdsafe (truncate 64 .)}}**
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
{{- end}}$new$),
    ('ALARM_DISPATCH_NOTIFICATION_GROUP', $old${{if .IsStarting}}🔴 {{if .AllPremiere}}선행공개{{else}}방송{{end}} 시작{{else}}⏰ {{if .AllPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전{{end}} · {{len .Entries}}개
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
{{- end}}$old$, $new$**{{if .IsStarting}}🔴 {{if .AllPremiere}}선행공개{{else}}방송{{end}} 시작{{else}}⏰ {{if .AllPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전{{end}} · {{len .Entries}}개**
{{- range $i, $entry := .Entries}}
{{if gt $i 0}}──────────{{end}}
{{add $i 1}} · {{if .IsStarting}}🔴 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} 시작{{else if .IsScheduled}}⏰ {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} 예정{{else}}⏰ {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전{{end}}
{{- $url := .URL}}
{{- $parts := split $url " | "}}
{{- $shortLinkURL := hasPrefix $url "https://short.holoshi.com/l/"}}
{{- $youtubeURL := or $shortLinkURL (hasPrefix $url "https://www.youtube.com/watch?") (hasPrefix $url "https://youtube.com/watch?") (hasPrefix $url "https://m.youtube.com/watch?") (hasPrefix $url "https://www.youtube.com/live/") (hasPrefix $url "https://youtube.com/live/") (hasPrefix $url "https://youtu.be/")}}
{{- $trustedURL := or $youtubeURL (hasPrefix $url "https://www.twitch.tv/") (hasPrefix $url "https://twitch.tv/") (hasPrefix $url "https://chzzk.naver.com/live/")}}
{{- $delimiterSafe := and (not (contains $url "\t")) (not (contains $url "\n")) (not (contains $url "\r")) (not (contains $url "(")) (not (contains $url ")")) (not (contains $url "[")) (not (contains $url "]")) (not (contains $url "<")) (not (contains $url ">")) (not (contains $url "\\"))}}
{{- $safeURL := and $url $trustedURL $delimiterSafe (not (contains $url " ")) (not (contains $url "|"))}}
{{- $composite := and (eq (len $parts) 2) $youtubeURL (hasPrefix (index $parts 1) "https://chzzk.naver.com/live/") $delimiterSafe (not (contains (index $parts 0) " ")) (not (contains (index $parts 1) " "))}}
{{- with displayline .Title}}
**{{mdsafe (truncate 64 .)}}**
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
{{- end}}$new$),
    ('CELEBRATION_ANNIVERSARY', $old$🎉 {{mdsafe (displayline .MemberName)}} 데뷔 {{.Years}}주년 축하합니다!{{if .ChannelID}}
https://youtube.com/channel/{{.ChannelID}}{{end}}$old$, $new$🎉 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 데뷔 {{.Years}}주년 축하합니다!{{if .ChannelID}}
https://youtube.com/channel/{{.ChannelID}}{{end}}$new$),
    ('CELEBRATION_BIRTHDAY', $old$🎂 {{mdsafe (displayline .MemberName)}}{{if gt .Ordinal 0}} {{.Ordinal}}번째{{end}} 생일 축하합니다!{{if .ChannelID}}
https://youtube.com/channel/{{.ChannelID}}{{end}}$old$, $new$🎂 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}}{{if gt .Ordinal 0}} {{.Ordinal}}번째{{end}} 생일 축하합니다!{{if .ChannelID}}
https://youtube.com/channel/{{.ChannelID}}{{end}}$new$),
    ('CELEBRATION_BIRTHDAY_STREAM', $old$🎂 {{mdsafe (displayline .MemberName)}} 생일 방송
{{- if .ScheduledStartKST}}
⏰ {{mdsafe .ScheduledStartKST}}
{{- end}}
{{- if trim .StreamTitle}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .StreamTitle))}}
{{- end}}
{{- if .StreamURL}}
{{.StreamURL}}
{{- end}}$old$, $new$🎂 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 생일 방송
{{- if .ScheduledStartKST}}
⏰ {{mdsafe .ScheduledStartKST}}
{{- end}}
{{- with displayline .StreamTitle}}
**{{mdsafe (truncate 64 .)}}**
{{- end}}
{{- if .StreamURL}}
{{.StreamURL}}
{{- end}}$new$),
    ('CMD_ALARM_ADDED', $old${{- if .Added -}}
✅ {{mdsafe (displayline .MemberName)}} 알람을 설정했습니다.
방송 시작 5분 전에 알립니다.
{{- else -}}
ℹ️ {{mdsafe (displayline .MemberName)}} 알람이 이미 설정되어 있습니다.
{{- end -}}$old$, $new${{- if .Added -}}
✅ {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 알람을 설정했습니다.
방송 시작 5분 전에 알립니다.
{{- else -}}
ℹ️ {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 알람이 이미 설정되어 있습니다.
{{- end -}}$new$),
    ('CMD_ALARM_CLEARED', $old${{- if eq .Count 0 -}}
🔔 설정된 알람이 없습니다.
{{- else -}}
✅ 알람 {{.Count}}개를 모두 해제했습니다.
{{- end -}}$old$, $new${{- if eq .Count 0 -}}
🔔 **설정된 알람이 없습니다**.
{{- else -}}
✅ **알람 {{.Count}}개를 모두 해제했습니다**.
{{- end -}}$new$),
    ('CMD_ALARM_LIST', $old${{- if eq .Count 0 -}}
🔔 설정된 알람이 없습니다.
예) {{mdescape .Prefix}}알람 추가 페코라
{{- else -}}
🔔 설정된 알람 · {{.Count}}개
{{- range $i, $alarm := .Alarms}}
{{if eq $i 0}}
{{end}}{{add $i 1}} · {{mdsafe (displayline $alarm.MemberName)}}{{if $alarm.TypesLabel}} ({{mdsafe (displayline $alarm.TypesLabel)}}){{end}}
{{- end -}}
{{- end -}}$old$, $new${{- if eq .Count 0 -}}
🔔 **설정된 알람이 없습니다**.
예) {{mdescape .Prefix}}알람 추가 페코라
{{- else -}}
🔔 **설정된 알람 · {{.Count}}개**
{{- range $i, $alarm := .Alarms}}
{{if eq $i 0}}
{{end}}{{add $i 1}} · {{with displayline $alarm.MemberName}}**{{mdsafe .}}**{{end}}{{if $alarm.TypesLabel}} ({{mdsafe (displayline $alarm.TypesLabel)}}){{end}}
{{- end -}}
{{- end -}}$new$),
    ('CMD_ALARM_REMOVED', $old${{- if .Removed -}}
✅ {{mdsafe (displayline .MemberName)}} 알람을 해제했습니다.
{{- else -}}
ℹ️ {{mdsafe (displayline .MemberName)}} 알람이 설정되어 있지 않습니다.
{{- end -}}$old$, $new${{- if .Removed -}}
✅ {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 알람을 해제했습니다.
{{- else -}}
ℹ️ {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 알람이 설정되어 있지 않습니다.
{{- end -}}$new$),
    ('CMD_AMBIGUOUS_MEMBER', $old$동일한 이름의 멤버가 여러 명 있습니다.
{{range .Candidates}}{{.Index}} · {{mdsafe (displayline .Name)}}
{{end}}
예) {{mdescape (printf "%s%s %s" .Prefix .CommandExample (displayline .FirstName))}}$old$, $new$**동일한 이름의 멤버가 여러 명** 있습니다.
{{range .Candidates}}{{.Index}} · {{with displayline .Name}}**{{mdsafe .}}**{{end}}
{{end}}
예) {{mdescape (printf "%s%s %s" .Prefix .CommandExample (displayline .FirstName))}}$new$),
    ('CMD_CALENDAR', $old${{- if eq .Count 0 -}}
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
{{- end -}}$old$, $new${{- if eq .Count 0 -}}
📅 {{.Year}}년 {{.Month}}월 **등록된 기념일이 없습니다**.
{{- else -}}
📅 **{{.Year}}년 {{.Month}}월 기념일 · {{.Count}}개**
{{- range $i, $day := .Days}}
{{if gt $i 0}}──────────{{end}}
[**{{printf "%02d/%02d" .Month .Day}}**]
{{- range .Entries}}
{{if .IsBirthday}}🎂 {{with displayline .Name}}**{{mdsafe .}}**{{end}} 생일{{else}}🎉 {{with displayline .Name}}**{{mdsafe .}}**{{end}} 데뷔 {{.Years}}주년{{end}}
{{- end}}
{{- end}}
{{- end -}}$new$),
    ('CMD_CHANNEL_SCHEDULE', $old${{- if not .ChannelName -}}
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
{{- end -}}$old$, $new${{- if not (displayline .ChannelName) -}}
❌ **채널 정보를 찾을 수 없습니다**.
{{- else if eq .Count 0 -}}
📅 {{with displayline .ChannelName}}**{{mdsafe .}}**{{end}}
{{.Days}}일 이내 **예정된 방송이 없습니다**.
{{- else -}}
📅 {{with displayline .ChannelName}}**{{mdsafe .}}**{{end}} 일정
{{.Days}}일 이내 · {{.Count}}개
{{- if gt .Count (len .Streams)}}
전체 {{.Count}}개 중 {{len .Streams}}개 표시
{{- end}}
{{- range $i, $stream := .Streams}}
{{if gt $i 0}}──────────{{end}}
{{add $i 1}} · {{if .IsLive}}🔴 **방송 중**{{else}}⏰ {{mdsafe .TimeInfo}}{{end}}
{{- with displayline .Title}}
**{{mdsafe (truncate 64 .)}}**
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$new$),
    ('CMD_HELP', $old$📖 홀로라이브 봇 명령어

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
{{mdescape .Prefix}}도움말 - 도움말$old$, $new$📖 **홀로라이브 봇 명령어**

**[방송]**
{{mdescape .Prefix}}라이브 - 방송 중 목록
{{mdescape .Prefix}}라이브 [멤버명] - 멤버 라이브 확인
{{mdescape .Prefix}}예정 - 예정 방송 목록
{{mdescape .Prefix}}예정 [멤버명] - 멤버 예정 방송
{{mdescape .Prefix}}멤버 [이름] - 일주일 이내 방송 일정

**[방송 이력]**
{{mdescape .Prefix}}방송이력/방송기록 [멤버명] [타입] - 종료된 방송 이력
{{mdescape .Prefix}}방송이력 경마 30 - 최근 30일 경마
{{mdescape .Prefix}}방송기록 페코라 게임 - 멤버·타입 필터
{{mdescape .Prefix}}방송이력 카테고리:게임 14일 개수:10 - 타입·기간·개수
타입: 게임/잡담/노래/ASMR/멤버십/이벤트/경마/동시시청/뉴스/기타/미분류
{{mdescape .Prefix}}방송이력 썸네일 [video_id] - 종료 방송 썸네일

**[멤버]**
{{mdescape .Prefix}}멤버 - 전체 멤버 목록
{{mdescape .Prefix}}정보 [멤버명] - 프로필 조회

**[알람]**
{{mdescape .Prefix}}알람 추가 [멤버명]
{{mdescape .Prefix}}알람 제거 [멤버명]
{{mdescape .Prefix}}알람 목록
{{mdescape .Prefix}}알람 초기화

**[뉴스]**
{{mdescape .Prefix}}뉴스 - 주간 뉴스 요약
{{mdescape .Prefix}}뉴스알림 켜기 / 끄기 / 상태

**[행사]**
{{mdescape .Prefix}}행사 - 행사 알림 상태
{{mdescape .Prefix}}행사 켜기 / 끄기

**[기념일]**
{{mdescape .Prefix}}기념일 - 이번 달 생일·주년
{{mdescape .Prefix}}기념일 다음달 / 저번달

**[기타]**
{{mdescape .Prefix}}도움말 - 도움말$new$),
    ('CMD_LIVE_STREAMS', $old${{- if eq .Count 0 -}}
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
{{- end -}}$old$, $new${{- if eq .Count 0 -}}
현재 **방송 중인 멤버가 없습니다**.
{{- else -}}
🔴 **방송 중 · {{.Count}}개**
{{- if gt .Count (len .Streams)}}
전체 {{.Count}}개 중 {{len .Streams}}개 표시
{{- end}}
{{- range $i, $stream := .Streams}}
{{if gt $i 0}}──────────{{end}}
{{add $i 1}} · {{with displayline .ChannelName}}**{{mdsafe .}}**{{end}}{{- if .Elapsed}} · ⏱ {{.Elapsed}}{{- end}}
{{- with displayline .Title}}
**{{mdsafe (truncate 64 .)}}**
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$new$),
    ('CMD_MAJOR_EVENT_ALREADY_SUB', $old$ℹ️ 행사 알림이 이미 켜져 있습니다.$old$, $new$ℹ️ 행사 알림이 **이미 켜져 있습니다**.$new$),
    ('CMD_MAJOR_EVENT_MONTHLY_SUMMARY', $old$📅 이번 달 행사 · {{.Count}}개
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
{{- end}}$old$, $new$📅 **이번 달 행사 · {{.Count}}개**
{{- if .LLMSummary}}

{{.LLMSummary}}
{{- end}}
{{- range $i, $event := .Events}}
{{if gt $i 0}}──────────{{end}}
{{add $i 1}} · {{with displayline $event.Title}}**{{mdsafe (truncate 64 .)}}**{{else}}행사{{end}}
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
    ('CMD_MAJOR_EVENT_NOT_SUB', $old$ℹ️ 행사 알림이 꺼져 있습니다.
설정: {{mdescape .Prefix}}행사 켜기$old$, $new$ℹ️ 행사 알림이 **꺼져 있습니다**.
설정: {{mdescape .Prefix}}행사 켜기$new$),
    ('CMD_MAJOR_EVENT_STATUS', $old${{if .IsSubscribed}}🔔{{else}}🔕{{end}} 행사 알림: {{if .IsSubscribed}}켜짐{{else}}꺼짐{{end}}
{{- if .IsSubscribed}}
해제: {{mdescape .Prefix}}행사 끄기
{{- else}}
설정: {{mdescape .Prefix}}행사 켜기
{{- end}}$old$, $new${{if .IsSubscribed}}🔔{{else}}🔕{{end}} 행사 알림: **{{if .IsSubscribed}}켜짐{{else}}꺼짐{{end}}**
{{- if .IsSubscribed}}
해제: {{mdescape .Prefix}}행사 끄기
{{- else}}
설정: {{mdescape .Prefix}}행사 켜기
{{- end}}$new$),
    ('CMD_MAJOR_EVENT_SUBSCRIBED', $old$✅ 행사 알림을 켰습니다.
발송: 매주 행사 요약$old$, $new$✅ 행사 알림을 **켰습니다**.
발송: 매주 행사 요약$new$),
    ('CMD_MAJOR_EVENT_UNSUBSCRIBED', $old$✅ 행사 알림을 껐습니다.$old$, $new$✅ 행사 알림을 **껐습니다**.$new$),
    ('CMD_MAJOR_EVENT_USAGE', $old$🔔 행사 알림 명령어
{{mdescape .Prefix}}행사 켜기 / 끄기 / 상태$old$, $new$🔔 **행사 알림 명령어**
{{mdescape .Prefix}}행사 켜기 / 끄기 / 상태$new$),
    ('CMD_MAJOR_EVENT_WEEKLY_SUMMARY', $old$📅 이번 주 행사 · {{.Count}}개
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
{{- end}}$old$, $new$📅 **이번 주 행사 · {{.Count}}개**
{{- if .LLMSummary}}

{{.LLMSummary}}
{{- end}}
{{- range $i, $event := .Events}}
{{if gt $i 0}}──────────{{end}}
{{add $i 1}} · {{with displayline $event.Title}}**{{mdsafe (truncate 64 .)}}**{{else}}행사{{end}}
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
    ('CMD_MEMBER_DIRECTORY', $old${{- if eq (len .Groups) 0 -}}
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
{{- end -}}$old$, $new${{- if eq (len .Groups) 0 -}}
👤 **등록된 멤버가 없습니다**.
{{- else -}}
👤 **멤버 목록 · {{.Total}}명**
{{- range $i, $group := .Groups}}
{{if gt $i 0}}──────────{{end}}
{{with displayline .GroupName}}[**{{mdsafe .}}**]{{end}}
{{- range .Members}}
{{- if or (displayline .Primary) (displayline .Secondary)}}
· {{if and .ShowBoth (displayline .Primary) (displayline .Secondary)}}{{with displayline .Primary}}**{{mdsafe .}}**{{end}} ({{mdsafe (displayline .Secondary)}}){{else if displayline .Primary}}{{with displayline .Primary}}**{{mdsafe .}}**{{end}}{{else if displayline .Secondary}}{{mdsafe (displayline .Secondary)}}{{end}}
{{- end}}
{{- end}}
{{- end}}
{{- end -}}$new$),
    ('CMD_MEMBER_NEWS_ALREADY_SUB', $old$ℹ️ 뉴스 알림이 이미 켜져 있습니다.$old$, $new$ℹ️ 뉴스 알림이 **이미 켜져 있습니다**.$new$),
    ('CMD_MEMBER_NEWS_DIGEST', $old${{- if trim .Headline -}}
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
{{- end}}$old$, $new${{- with displayline .Headline -}}
**{{mdsafe (truncate 64 .)}}**
{{- else -}}
📰 **멤버 뉴스**
{{- end}}
{{- if eq (len .TopItems) 0}}
**표시할 뉴스가 없습니다**.
{{- else}}
{{- range $i, $item := .TopItems}}
{{if gt $i 0}}──────────{{end}}
{{add $i 1}} · {{with displayline $item.Member}}**{{mdsafe .}}**{{end}}
{{- if or $item.DateText $item.Category}}
{{if $item.DateText}}{{mdsafe (displayline $item.DateText)}}{{end}}{{if and $item.DateText $item.Category}} · {{end}}{{if $item.Category}}{{mdsafe (displayline $item.Category)}}{{end}}
{{- end}}
{{- with displayline $item.Title}}
**{{mdsafe (truncate 64 .)}}**
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
{{- end}}$new$),
    ('CMD_MEMBER_NEWS_NOT_SUB', $old$ℹ️ 뉴스 알림이 이미 꺼져 있습니다.$old$, $new$ℹ️ 뉴스 알림이 **이미 꺼져 있습니다**.$new$),
    ('CMD_MEMBER_NEWS_NO_MEMBERS', $old$📰 뉴스 대상 멤버가 없습니다.
예) {{mdescape .Prefix}}알람 추가 페코라$old$, $new$📰 **뉴스 대상 멤버가 없습니다**.
예) {{mdescape .Prefix}}알람 추가 페코라$new$),
    ('CMD_MEMBER_NEWS_STATUS', $old${{if .IsSubscribed}}🔔{{else}}🔕{{end}} 뉴스 알림: {{if .IsSubscribed}}켜짐{{else}}꺼짐{{end}}
{{- if .IsSubscribed}}
발송: 매주 월요일 09:00 KST
해제: {{mdescape .Prefix}}뉴스알림 끄기
{{- else}}
설정: {{mdescape .Prefix}}뉴스알림 켜기
{{- end}}$old$, $new${{if .IsSubscribed}}🔔{{else}}🔕{{end}} 뉴스 알림: **{{if .IsSubscribed}}켜짐{{else}}꺼짐{{end}}**
{{- if .IsSubscribed}}
발송: 매주 월요일 09:00 KST
해제: {{mdescape .Prefix}}뉴스알림 끄기
{{- else}}
설정: {{mdescape .Prefix}}뉴스알림 켜기
{{- end}}$new$),
    ('CMD_MEMBER_NEWS_SUBSCRIBED', $old$✅ 뉴스 알림을 켰습니다.
발송: 매주 월요일 09:00 KST$old$, $new$✅ 뉴스 알림을 **켰습니다**.
발송: 매주 월요일 09:00 KST$new$),
    ('CMD_MEMBER_NEWS_UNSUBSCRIBED', $old$✅ 뉴스 알림을 껐습니다.$old$, $new$✅ 뉴스 알림을 **껐습니다**.$new$),
    ('CMD_MEMBER_NOT_FOUND', $old$❌ '{{mdsafe (displayline .MemberName)}}' 멤버를 찾을 수 없습니다.$old$, $new$❌ '{{with displayline .MemberName}}**{{mdsafe .}}**{{end}}' 멤버를 찾을 수 없습니다.$new$),
    ('CMD_MEMBER_NOT_LIVE', $old${{printf "\u200b"}}{{mdsafe (displayline .MemberName)}}: 현재 방송 중이 아닙니다.$old$, $new${{with displayline .MemberName}}**{{mdsafe .}}**{{end}}: 현재 방송 중이 아닙니다.$new$),
    ('CMD_MEMBER_NO_UPCOMING', $old${{printf "\u200b"}}{{mdsafe (displayline .MemberName)}}: {{.Hours}}시간 이내 예정된 방송이 없습니다.$old$, $new${{with displayline .MemberName}}**{{mdsafe .}}**{{end}}: {{.Hours}}시간 이내 예정된 방송이 없습니다.$new$),
    ('CMD_PROFILE', $old${{- if eq (len .Names) 0 -}}
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
{{- end -}}$old$, $new${{- if eq (len .Names) 0 -}}
👤 **멤버 정보**
{{- else -}}
👤 {{with displayline (index .Names 0)}}**{{mdsafe .}}**{{else}}**멤버 정보**{{end}}
{{- if gt (len .Names) 1}}
별칭: {{mdsafe (displayline (join (slice .Names 1) " / "))}}
{{- end}}
{{- end}}
{{- if .Catchphrase}}
"{{mdsafe (displayline .Catchphrase)}}"
{{- end}}
{{- if .Summary}}

{{replace (mdsafe .Summary) "\r\n" "\n"}}
{{- end}}
{{- if .Highlights}}

**[하이라이트]**
{{- range .Highlights}}
· {{mdsafe (displayline .)}}
{{- end}}
{{- end}}
{{- if .DataRows}}

**[프로필]**
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

**[링크]**
{{- range $i, $link := .SocialLinks}}
{{if gt $i 0}}
{{end}}{{mdsafe (displayline .Label)}}
{{.URL}}
{{- end}}
{{- end}}
{{- if .OfficialURL}}

**공식 프로필**
{{.OfficialURL}}
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
{{- end -}}$old$, $new${{- if eq .Count 0 -}}
📅 {{.Hours}}시간 이내 **예정된 방송이 없습니다**.
{{- else -}}
📅 **예정 방송 · {{.Count}}개**
{{.Hours}}시간 이내
{{- if gt .Count (len .Streams)}}
전체 {{.Count}}개 중 {{len .Streams}}개 표시
{{- end}}
{{- range $i, $stream := .Streams}}
{{if gt $i 0}}──────────{{end}}
{{add $i 1}} · {{with displayline .ChannelName}}**{{mdsafe .}}**{{end}}
⏰ {{mdsafe .TimeInfo}}
{{- with displayline .Title}}
**{{mdsafe (truncate 64 .)}}**
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$new$),
    ('OUTBOX_COMMUNITY', $old$🔔 {{mdsafe (displayline .MemberName)}} 커뮤니티 글
{{- if trim .ContentText}}
{{printf "\u200b"}}{{mdsafe (truncate 100 (displayline .ContentText))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$old$, $new$🔔 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 커뮤니티 글
{{- with displayline .ContentText}}
{{mdsafe (truncate 100 .)}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$new$),
    ('OUTBOX_COMMUNITY_GROUP', $old$🔔 {{mdsafe (displayline .MemberName)}} 커뮤니티 글 · {{.Count}}개
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
{{- end}}$old$, $new$🔔 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 커뮤니티 글 · {{.Count}}개
{{- $n := 0}}
{{- range $item := .Items}}
{{- if or (displayline $item.ContentText) $item.URL}}
{{if gt $n 0}}──────────{{end}}
{{$n = add $n 1 -}}
{{$n}} · {{with displayline $item.ContentText}}{{mdsafe (truncate 100 .)}}{{else}}커뮤니티 글{{end}}
{{- if $item.URL}}
{{$item.URL}}
{{- end}}
{{- end}}
{{- end}}
{{- if ne $n .Count}}

전체 {{.Count}}개 중 {{$n}}개 표시
{{- end}}$new$),
    ('OUTBOX_SHORTS', $old$🔔 {{mdsafe (displayline .MemberName)}} 새 쇼츠
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Title))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$old$, $new$🔔 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 새 쇼츠
{{- with displayline .Title}}
**{{mdsafe (truncate 64 .)}}**
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$new$),
    ('OUTBOX_SHORTS_GROUP', $old$🔔 {{mdsafe (displayline .MemberName)}} 새 쇼츠 · {{.Count}}개
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
{{- end}}$old$, $new$🔔 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 새 쇼츠 · {{.Count}}개
{{- $n := 0}}
{{- range $item := .Items}}
{{- if or (displayline $item.Title) $item.URL}}
{{if gt $n 0}}──────────{{end}}
{{$n = add $n 1 -}}
{{$n}} · {{with displayline $item.Title}}**{{mdsafe (truncate 64 .)}}**{{else}}쇼츠{{end}}
{{- if $item.URL}}
{{$item.URL}}
{{- end}}
{{- end}}
{{- end}}
{{- if ne $n .Count}}

전체 {{.Count}}개 중 {{$n}}개 표시
{{- end}}$new$),
    ('OUTBOX_VIDEO', $old${{if eq .Kind "LIVE_STREAM"}}🔴 {{mdsafe (displayline .MemberName)}} 방송 시작{{else if .IsUpcomingPremiere}}🔔 {{mdsafe (displayline .MemberName)}} {{.MinutesUntilPremiere}}분 후 공개 예정{{else if .IsPremiere}}🔔 {{mdsafe (displayline .MemberName)}} 최초공개{{else}}🔔 {{mdsafe (displayline .MemberName)}} 새 영상{{end}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Title))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$old$, $new${{if eq .Kind "LIVE_STREAM"}}🔴 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 방송 시작{{else if .IsUpcomingPremiere}}🔔 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} {{.MinutesUntilPremiere}}분 후 공개 예정{{else if .IsPremiere}}🔔 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 최초공개{{else}}🔔 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 새 영상{{end}}
{{- with displayline .Title}}
**{{mdsafe (truncate 64 .)}}**
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$new$),
    ('OUTBOX_VIDEO_GROUP', $old${{if eq .Kind "LIVE_STREAM"}}🔴 {{mdsafe (displayline .MemberName)}} 방송 시작{{else if eq .Kind "NEW_VIDEO"}}🔔 {{mdsafe (displayline .MemberName)}} 새 영상{{else}}🔔 {{mdsafe (displayline .MemberName)}} 알림{{end}} · {{.Count}}개
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
{{- end}}$old$, $new${{if eq .Kind "LIVE_STREAM"}}🔴 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 방송 시작{{else if eq .Kind "NEW_VIDEO"}}🔔 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 새 영상{{else}}🔔 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 알림{{end}} · {{.Count}}개
{{- $n := 0}}
{{- range $item := .Items}}
{{- if or (displayline $item.Title) $item.URL}}
{{if gt $n 0}}──────────{{end}}
{{$n = add $n 1 -}}
{{$n}} · {{with displayline $item.Title}}**{{mdsafe (truncate 64 .)}}**{{else}}영상{{end}}
{{- if $item.URL}}
{{$item.URL}}
{{- end}}
{{- end}}
{{- end}}
{{- if ne $n .Count}}

전체 {{.Count}}개 중 {{$n}}개 표시
{{- end}}$new$),
    ('X_SPACE_STARTED', $old$🔴 {{mdsafe (displayline .MemberName)}} 스페이스 시작
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Title))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$old$, $new$🔴 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 스페이스 시작
{{- with displayline .Title}}
**{{mdsafe (truncate 64 .)}}**
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$new$)
) AS seed(template_key, old_body, new_body)
WHERE target.template_key = seed.template_key
  AND target.channel_id IS NULL
  AND target.body = seed.old_body
  AND target.body IS DISTINCT FROM seed.new_body;
