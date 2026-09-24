-- 기본 메시지의 항목 경계·독립 링크·보조 정보 표시를 정리합니다.
-- 203 이후 표준 본문만 갱신하며 커스텀 default 및 channel override는 보존합니다.
-- 기존 template 함수만 사용합니다. 단발 ZWSP는 줄 첫 문자의 목록·인용 해석을 막습니다.
UPDATE notification_templates AS target
SET body = seed.new_body,
    updated_at = now()
FROM (VALUES
    ('CMD_MILESTONE_APPROACHING', $old$📊 **{{mdsafe .MemberName}}** 구독자 {{.Milestone}}명까지 {{.Remaining}}명 남았습니다.$old$, $new$📊 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 구독자 {{.Milestone}}명까지 {{formatNumber .Remaining}}명 남았습니다.$new$),
    ('CELEBRATION_BIRTHDAY_STREAM', $old$🎂 {{mdsafe .MemberName}} 생일 방송
{{- if .ScheduledStartKST}}
⏰ {{mdsafe .ScheduledStartKST}}
{{- end}}
{{- if .StreamTitle}}
{{mdsafe (truncate 64 (trim (replace (replace (replace .StreamTitle "\r\n" " ") "\r" " ") "\n" " ")))}}
{{- end}}
{{- if .StreamURL}}
{{.StreamURL}}
{{- end}}$old$, $new$🎂 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 생일 방송
{{- if .ScheduledStartKST}}
⏰ {{mdsafe .ScheduledStartKST}}
{{- end}}
{{- if trim .StreamTitle}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (trim (replace (replace (replace (replace (replace .StreamTitle "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " ")))}}
{{- end}}
{{- if .StreamURL}}
{{.StreamURL}}
{{- end}}$new$),
    ('OUTBOX_COMMUNITY', $old$🔔 {{mdsafe .MemberName}} 커뮤니티 글
{{- if .ContentText}}
{{mdsafe (truncate 100 (trim (replace (replace (replace .ContentText "\r\n" " ") "\r" " ") "\n" " ")))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$old$, $new$🔔 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 커뮤니티 글
{{- if trim .ContentText}}
{{printf "\u200b"}}{{mdsafe (truncate 100 (trim (replace (replace (replace (replace (replace .ContentText "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " ")))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$new$),
    ('OUTBOX_VIDEO', $old${{if eq .Kind "LIVE_STREAM"}}🔴 {{mdsafe .MemberName}} 방송 시작{{else if .IsUpcomingPremiere}}🔔 {{mdsafe .MemberName}} {{.MinutesUntilPremiere}}분 후 공개 예정{{else if .IsPremiere}}🔔 {{mdsafe .MemberName}} 최초공개{{else}}🔔 {{mdsafe .MemberName}} 새 영상{{end}}
{{- if .Title}}
{{mdsafe (truncate 64 (trim (replace (replace (replace .Title "\r\n" " ") "\r" " ") "\n" " ")))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$old$, $new${{if eq .Kind "LIVE_STREAM"}}🔴 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 방송 시작{{else if .IsUpcomingPremiere}}🔔 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} {{.MinutesUntilPremiere}}분 후 공개 예정{{else if .IsPremiere}}🔔 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 최초공개{{else}}🔔 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 새 영상{{end}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (trim (replace (replace (replace (replace (replace .Title "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " ")))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$new$),
    ('OUTBOX_SHORTS', $old$🔔 {{mdsafe .MemberName}} 새 쇼츠
{{- if .Title}}
{{mdsafe (truncate 64 (trim (replace (replace (replace .Title "\r\n" " ") "\r" " ") "\n" " ")))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$old$, $new$🔔 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 새 쇼츠
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (trim (replace (replace (replace (replace (replace .Title "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " ")))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$new$),
    ('OUTBOX_MILESTONE', $old$🎉 **{{mdsafe .MemberName}}** {{mdsafe .Milestone}} 달성$old$, $new$🎉 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} {{mdsafe .Milestone}} 달성$new$),
    ('CMD_ALARM_REMOVED', $old${{- if .Removed -}}
✅ **{{mdsafe .MemberName}}** 알람을 해제했습니다.
{{- else -}}
ℹ️ **{{mdsafe .MemberName}}** 알람이 설정되어 있지 않습니다.
{{- end -}}$old$, $new${{- if .Removed -}}
✅ {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 알람을 해제했습니다.
{{- else -}}
ℹ️ {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 알람이 설정되어 있지 않습니다.
{{- end -}}$new$),
    ('CMD_STATS_COUNT', $old$📊 **{{mdsafe .MemberName}}** 구독자 {{.Subscribers}}명$old$, $new$📊 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 구독자 {{.Subscribers}}명$new$),
    ('CMD_PROFILE', $old${{- if eq (len .Names) 0 -}}
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
{{- end -}}$old$, $new${{- if eq (len .Names) 0 -}}
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
{{- end -}}$new$),
    ('CELEBRATION_BIRTHDAY', $old$🎂 **{{mdsafe .MemberName}}**{{if gt .Ordinal 0}} {{.Ordinal}}번째{{end}} 생일 축하합니다!{{if .ChannelID}}
[YouTube 채널 보기](https://youtube.com/channel/{{.ChannelID}}){{end}}$old$, $new$🎂 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}{{if gt .Ordinal 0}} {{.Ordinal}}번째{{end}} 생일 축하합니다!{{if .ChannelID}}
https://youtube.com/channel/{{.ChannelID}}{{end}}$new$),
    ('CELEBRATION_ANNIVERSARY', $old$🎉 **{{mdsafe .MemberName}}** 데뷔 {{.Years}}주년 축하합니다!{{if .ChannelID}}
[YouTube 채널 보기](https://youtube.com/channel/{{.ChannelID}}){{end}}$old$, $new$🎉 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 데뷔 {{.Years}}주년 축하합니다!{{if .ChannelID}}
https://youtube.com/channel/{{.ChannelID}}{{end}}$new$),
    ('CMD_CHANNEL_SCHEDULE', $old${{- if not .ChannelName -}}
❌ 채널 정보를 찾을 수 없습니다.
{{- else if eq .Count 0 -}}
📅 {{mdsafe .ChannelName}}
{{.Days}}일 이내 예정된 방송이 없습니다.
{{- else -}}
📅 {{mdsafe .ChannelName}} 일정
{{.Days}}일 이내 · {{.Count}}개
{{- range $i, $stream := .Streams}}
{{- if gt $i 0}}

──────────
{{- end}}

{{add $i 1}} · {{if .IsLive}}🔴 방송 중{{else}}⏰ {{mdsafe .TimeInfo}}{{end}}
{{- if .Title}}
{{mdsafe (truncate 64 (trim (replace (replace (replace .Title "\r\n" " ") "\r" " ") "\n" " ")))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$old$, $new${{- if not .ChannelName -}}
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
{{- end -}}$new$),
    ('CMD_MEMBER_DIRECTORY', $old${{- if eq (len .Groups) 0 -}}
👤 등록된 멤버가 없습니다.
{{- else -}}
## 👤 멤버 목록 ({{.Total}})
{{- range .Groups}}

**{{mdsafe .GroupName}}**
{{- range .Members}}
{{- if .ShowBoth}}
- {{mdsafe .Primary}} ({{mdsafe .Secondary}})
{{- else if .Primary}}
- {{mdsafe .Primary}}
{{- else if .Secondary}}
- {{mdsafe .Secondary}}
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

[{{mdsafe (trim (replace (replace (replace (replace (replace .GroupName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}]
{{- range .Members}}
{{- if or .Primary .Secondary}}
· {{if .ShowBoth}}{{mdsafe (trim (replace (replace (replace (replace (replace .Primary "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} ({{mdsafe (trim (replace (replace (replace (replace (replace .Secondary "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}){{else if .Primary}}{{mdsafe (trim (replace (replace (replace (replace (replace .Primary "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}{{else if .Secondary}}{{mdsafe (trim (replace (replace (replace (replace (replace .Secondary "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}{{end}}
{{- end}}
{{- end}}
{{- end}}
{{- end -}}$new$),
    ('CMD_MILESTONE_ACHIEVED', $old$🎉 **{{mdsafe .MemberName}}** 구독자 {{.Milestone}}명 달성!$old$, $new$🎉 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 구독자 {{.Milestone}}명 달성!$new$),
    ('CMD_HELP', $old$홀로라이브 봇 명령어

[방송]
  {{.Prefix}}라이브 - 방송 중 목록
  {{.Prefix}}라이브 [멤버명] - 멤버 라이브 확인
  {{.Prefix}}예정 - 예정 방송 목록
  {{.Prefix}}예정 [멤버명] - 멤버 예정 방송
  {{.Prefix}}멤버 [이름] - 일주일 이내 방송 일정
  {{.Prefix}}방송이력/방송기록 [멤버명] [타입] - 종료된 방송 이력
  {{.Prefix}}방송이력 경마 30 - 최근 30일 경마
  {{.Prefix}}방송기록 페코라 게임 - 멤버·타입 필터
  {{.Prefix}}방송이력 카테고리:게임 14일 개수:10 - 타입·기간·개수
  타입: 게임/잡담/노래/ASMR/멤버십/이벤트/경마/동시시청/뉴스/기타/미분류
  {{.Prefix}}방송이력 썸네일 [video_id] - 종료 방송 썸네일

[멤버]
  {{.Prefix}}멤버 - 전체 멤버 목록
  {{.Prefix}}정보 [멤버명] - 프로필 조회

[알람]
  {{.Prefix}}알람 추가 [멤버명]
  {{.Prefix}}알람 제거 [멤버명]
  {{.Prefix}}알람 목록
  {{.Prefix}}알람 초기화

[뉴스]
  {{.Prefix}}뉴스 - 주간 뉴스 요약
  {{.Prefix}}뉴스알림 켜기 / 끄기 / 상태

[행사]
  {{.Prefix}}행사 - 행사 알림 상태
  {{.Prefix}}행사 켜기 / 끄기

[기념일]
  {{.Prefix}}기념일 - 이번 달 생일·주년
  {{.Prefix}}기념일 다음달 / 저번달

[기타]
  {{.Prefix}}구독자 [멤버명] - 구독자 수
  {{.Prefix}}도움말 - 도움말$old$, $new$📖 홀로라이브 봇 명령어

[방송]
{{mdsafe .Prefix}}라이브 - 방송 중 목록
{{mdsafe .Prefix}}라이브 [멤버명] - 멤버 라이브 확인
{{mdsafe .Prefix}}예정 - 예정 방송 목록
{{mdsafe .Prefix}}예정 [멤버명] - 멤버 예정 방송
{{mdsafe .Prefix}}멤버 [이름] - 일주일 이내 방송 일정

[방송 이력]
{{mdsafe .Prefix}}방송이력/방송기록 [멤버명] [타입] - 종료된 방송 이력
{{mdsafe .Prefix}}방송이력 경마 30 - 최근 30일 경마
{{mdsafe .Prefix}}방송기록 페코라 게임 - 멤버·타입 필터
{{mdsafe .Prefix}}방송이력 카테고리:게임 14일 개수:10 - 타입·기간·개수
타입: 게임/잡담/노래/ASMR/멤버십/이벤트/경마/동시시청/뉴스/기타/미분류
{{mdsafe .Prefix}}방송이력 썸네일 [video_id] - 종료 방송 썸네일

[멤버]
{{mdsafe .Prefix}}멤버 - 전체 멤버 목록
{{mdsafe .Prefix}}정보 [멤버명] - 프로필 조회

[알람]
{{mdsafe .Prefix}}알람 추가 [멤버명]
{{mdsafe .Prefix}}알람 제거 [멤버명]
{{mdsafe .Prefix}}알람 목록
{{mdsafe .Prefix}}알람 초기화

[뉴스]
{{mdsafe .Prefix}}뉴스 - 주간 뉴스 요약
{{mdsafe .Prefix}}뉴스알림 켜기 / 끄기 / 상태

[행사]
{{mdsafe .Prefix}}행사 - 행사 알림 상태
{{mdsafe .Prefix}}행사 켜기 / 끄기

[기념일]
{{mdsafe .Prefix}}기념일 - 이번 달 생일·주년
{{mdsafe .Prefix}}기념일 다음달 / 저번달

[기타]
{{mdsafe .Prefix}}구독자 [멤버명] - 구독자 수
{{mdsafe .Prefix}}도움말 - 도움말$new$),
    ('CMD_ALARM_ADDED', $old${{- if .Added -}}
✅ **{{mdsafe .MemberName}}** 알람을 설정했습니다. 방송 시작 5분 전에 알립니다.
{{- if .NextStream}}
{{- if eq .NextStream.Status "live"}}
- 🔴 방송 중
{{- if and .NextStream.Title .NextStream.URL}}
- [{{mdsafe .NextStream.Title}}]({{.NextStream.URL}})
{{- else if .NextStream.Title}}
- {{mdsafe .NextStream.Title}}
{{- else if .NextStream.URL}}
- {{.NextStream.URL}}
{{- end}}
{{- else if eq .NextStream.Status "upcoming"}}
- ⏰ {{if .NextStream.StartingSoon}}곧 시작{{else}}{{.NextStream.ScheduledKST}}{{if .NextStream.TimeDetail}} ({{.NextStream.TimeDetail}}){{end}}{{end}}
{{- if and .NextStream.Title .NextStream.URL}}
- [{{mdsafe .NextStream.Title}}]({{.NextStream.URL}})
{{- else if .NextStream.Title}}
- {{mdsafe .NextStream.Title}}
{{- else if .NextStream.URL}}
- {{.NextStream.URL}}
{{- end}}
{{- end}}
{{- end}}
{{- else -}}
ℹ️ **{{mdsafe .MemberName}}** 알람이 이미 설정되어 있습니다.
{{- end -}}$old$, $new${{- if .Added -}}
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
{{- end -}}$new$),
    ('CMD_MAJOR_EVENT_MONTHLY_SUMMARY', $old$## 📅 이번 달 행사 ({{.Count}})
{{- if .LLMSummary}}

{{.LLMSummary}}
{{- end}}
{{range $index, $event := .Events}}
{{- if and $event.Title $event.Link}}
{{add $index 1}}. [{{mdsafe $event.Title}}]({{$event.Link}})
{{- else if $event.Title}}
{{add $index 1}}. {{mdsafe $event.Title}}
{{- else}}
{{add $index 1}}. {{$event.Link}}
{{- end}}
{{- if $event.DateStr}}
   ⏰ {{$event.DateStr}}
{{- end}}
{{- if $event.Members}}
   {{mdsafe $event.Members}}
{{- end}}
{{- end}}$old$, $new$📅 이번 달 행사 · {{.Count}}개
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
{{- end}}$new$),
    ('CMD_MAJOR_EVENT_NOT_SUB', $old$ℹ️ 행사 알림이 꺼져 있습니다.
- 설정: `{{.Prefix}}행사 켜기`$old$, $new$ℹ️ 행사 알림이 꺼져 있습니다.
설정: {{.Prefix}}행사 켜기$new$),
    ('CMD_MAJOR_EVENT_STATUS', $old${{if .IsSubscribed}}🔔{{else}}🔕{{end}} 행사 알림: **{{if .IsSubscribed}}켜짐{{else}}꺼짐{{end}}**
{{- if .IsSubscribed}}
- 해제: `{{.Prefix}}행사 끄기`
{{- else}}
- 설정: `{{.Prefix}}행사 켜기`
{{- end}}$old$, $new${{if .IsSubscribed}}🔔{{else}}🔕{{end}} 행사 알림: {{if .IsSubscribed}}켜짐{{else}}꺼짐{{end}}
{{- if .IsSubscribed}}
해제: {{.Prefix}}행사 끄기
{{- else}}
설정: {{.Prefix}}행사 켜기
{{- end}}$new$),
    ('CMD_MAJOR_EVENT_USAGE', $old$🔔 행사 알림 명령어
- `{{.Prefix}}행사 켜기 / 끄기 / 상태`$old$, $new$🔔 행사 알림 명령어
{{.Prefix}}행사 켜기 / 끄기 / 상태$new$),
    ('CMD_MEMBER_NEWS_DIGEST', $old${{- if .Headline -}}
## {{mdsafe .Headline}}
{{- else -}}
## 📰 멤버 뉴스
{{- end -}}
{{- if eq (len .TopItems) 0 }}
표시할 뉴스가 없습니다.
{{- else }}
{{range $index, $item := .TopItems}}
{{add $index 1}}. {{$item.DateText}} · **{{mdsafe $item.Member}}** · {{mdsafe $item.Category}}
{{- if and $item.Title $item.SourceURL}}
   [{{mdsafe $item.Title}}]({{$item.SourceURL}})
{{- else if $item.Title}}
   {{mdsafe $item.Title}}
{{- else if $item.SourceURL}}
   {{$item.SourceURL}}
{{- end}}
{{- if $item.Summary}}
   {{mdsafe $item.Summary}}
{{- end}}
{{- end}}
{{- if .MoreSummary }}

{{mdsafe .MoreSummary}}
{{- end }}
{{- end }}$old$, $new${{- if trim .Headline -}}
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
{{- end}}$new$),
    ('CMD_MEMBER_NEWS_NO_MEMBERS', $old$📰 뉴스 대상 멤버가 없습니다.
예) `{{.Prefix}}알람 추가 페코라`$old$, $new$📰 뉴스 대상 멤버가 없습니다.
예) {{.Prefix}}알람 추가 페코라$new$),
    ('CMD_MEMBER_NEWS_STATUS', $old${{if .IsSubscribed}}🔔{{else}}🔕{{end}} 뉴스 알림: **{{if .IsSubscribed}}켜짐{{else}}꺼짐{{end}}**
{{- if .IsSubscribed}}
- 발송: 매주 월요일 09:00 KST
- 해제: `{{.Prefix}}뉴스알림 끄기`
{{- else}}
- 설정: `{{.Prefix}}뉴스알림 켜기`
{{- end}}$old$, $new${{if .IsSubscribed}}🔔{{else}}🔕{{end}} 뉴스 알림: {{if .IsSubscribed}}켜짐{{else}}꺼짐{{end}}
{{- if .IsSubscribed}}
발송: 매주 월요일 09:00 KST
해제: {{.Prefix}}뉴스알림 끄기
{{- else}}
설정: {{.Prefix}}뉴스알림 켜기
{{- end}}$new$),
    ('ALARM_DISPATCH_NOTIFICATION_GROUP', $old$## {{if .IsStarting}}🔴 {{if .AllPremiere}}선행공개{{else}}방송{{end}} 시작{{else}}⏰ {{if .AllPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전{{end}}
{{- range .Entries}}

{{if .IsStarting}}🔴 **{{mdsafe .MemberName}}** {{if .IsPremiere}}선행공개{{else}}방송{{end}} 시작{{else if .IsScheduled}}⏰ **{{mdsafe .MemberName}}** {{if .IsPremiere}}선행공개{{else}}방송{{end}} 예정{{else}}⏰ **{{mdsafe .MemberName}}** {{if .IsPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전{{end}}
{{- $url := .URL}}
{{- $parts := split $url " | "}}
{{- $shortLinkURL := hasPrefix $url "https://short.holoshi.com/l/"}}
{{- $youtubeURL := or $shortLinkURL (hasPrefix $url "https://www.youtube.com/watch?") (hasPrefix $url "https://youtube.com/watch?") (hasPrefix $url "https://m.youtube.com/watch?") (hasPrefix $url "https://www.youtube.com/live/") (hasPrefix $url "https://youtube.com/live/") (hasPrefix $url "https://youtu.be/")}}
{{- $trustedURL := or $youtubeURL (hasPrefix $url "https://www.twitch.tv/") (hasPrefix $url "https://twitch.tv/") (hasPrefix $url "https://chzzk.naver.com/live/")}}
{{- $delimiterSafe := and (not (contains $url "\t")) (not (contains $url "\n")) (not (contains $url "\r")) (not (contains $url "(")) (not (contains $url ")")) (not (contains $url "[")) (not (contains $url "]")) (not (contains $url "<")) (not (contains $url ">")) (not (contains $url "\\"))}}
{{- $safeURL := and $url $trustedURL $delimiterSafe (not (contains $url " ")) (not (contains $url "|"))}}
{{- $composite := and (eq (len $parts) 2) $youtubeURL (hasPrefix (index $parts 1) "https://chzzk.naver.com/live/") $delimiterSafe (not (contains (index $parts 0) " ")) (not (contains (index $parts 1) " "))}}
{{- $linkable := and .Title $safeURL}}
{{- if $linkable}}
[{{mdsafe .Title}}]({{.URL}})
{{- else if .Title}}
{{printf "\u200b"}}{{mdsafe .Title}}
{{- end}}
{{- if .CollabMembers}}
콜라보: {{mdsafe .CollabMembers}}
{{- end}}
{{- if .ScheduleMessage}}
{{printf "\u200b"}}{{mdsafe .ScheduleMessage}}
{{- end}}
{{- if and .URL (not $linkable)}}
{{if or $safeURL $composite}}{{.URL}}{{else}}{{printf "\u200b"}}{{mdsafe (replace (replace .URL "\n" " ") "\r" " ")}}{{end}}
{{- end}}
{{- end}}$old$, $new${{if .IsStarting}}🔴 {{if .AllPremiere}}선행공개{{else}}방송{{end}} 시작{{else}}⏰ {{if .AllPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전{{end}} · {{len .Entries}}개
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
{{- end}}$new$),
    ('OUTBOX_VIDEO_GROUP', $old$## {{if eq .Kind "LIVE_STREAM"}}🔴 {{mdsafe .MemberName}} 방송 시작 ({{.Count}}){{else if eq .Kind "NEW_VIDEO"}}🔔 {{mdsafe .MemberName}} 새 영상 ({{.Count}}){{else}}🔔 {{mdsafe .MemberName}} 알림 ({{.Count}}){{end}}
{{- $n := 0}}
{{- range $item := .Items}}
{{- if and $item.Title $item.URL}}
{{- $n = add $n 1}}
{{$n}}. [{{mdsafe (truncate 40 $item.Title)}}]({{$item.URL}})
{{- else if $item.Title}}
{{- $n = add $n 1}}
{{$n}}. {{mdsafe (truncate 40 $item.Title)}}
{{- else if $item.URL}}
{{- $n = add $n 1}}
{{$n}}. {{$item.URL}}
{{- end}}
{{- end}}$old$, $new${{if eq .Kind "LIVE_STREAM"}}🔴 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 방송 시작{{else if eq .Kind "NEW_VIDEO"}}🔔 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 새 영상{{else}}🔔 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 알림{{end}} · {{.Count}}개
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
{{- end}}$new$),
    ('OUTBOX_SHORTS_GROUP', $old$## 🔔 {{mdsafe .MemberName}} 새 쇼츠 ({{.Count}})
{{- $n := 0}}
{{- range $item := .Items}}
{{- if and $item.Title $item.URL}}
{{- $n = add $n 1}}
{{$n}}. [{{mdsafe (truncate 40 $item.Title)}}]({{$item.URL}})
{{- else if $item.Title}}
{{- $n = add $n 1}}
{{$n}}. {{mdsafe (truncate 40 $item.Title)}}
{{- else if $item.URL}}
{{- $n = add $n 1}}
{{$n}}. {{$item.URL}}
{{- end}}
{{- end}}$old$, $new$🔔 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 새 쇼츠 · {{.Count}}개
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
{{- end}}$new$),
    ('OUTBOX_COMMUNITY_GROUP', $old$## 🔔 {{mdsafe .MemberName}} 커뮤니티 글 ({{.Count}})
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
{{- end}}$old$, $new$🔔 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 커뮤니티 글 · {{.Count}}개
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
{{- end}}$new$),
    ('CMD_LIVE_STREAMS', $old${{- if eq .Count 0 -}}
🔴 방송 중인 스트림이 없습니다.
{{- else -}}
🔴 방송 중 · {{.Count}}개
{{- range $i, $stream := .Streams}}
{{- if gt $i 0}}

──────────
{{- end}}

{{add $i 1}} · {{mdsafe .ChannelName}}
{{- if .Title}}
{{mdsafe (truncate 64 (trim (replace (replace (replace .Title "\r\n" " ") "\r" " ") "\n" " ")))}}
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

{{add $i 1}} · {{mdsafe (trim (replace (replace (replace (replace (replace .ChannelName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (trim (replace (replace (replace (replace (replace .Title "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " ")))}}
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
{{- range $i, $stream := .Streams}}
{{- if gt $i 0}}

──────────
{{- end}}

{{add $i 1}} · {{mdsafe .ChannelName}}
⏰ {{mdsafe .TimeInfo}}
{{- if .Title}}
{{mdsafe (truncate 64 (trim (replace (replace (replace .Title "\r\n" " ") "\r" " ") "\n" " ")))}}
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

{{add $i 1}} · {{mdsafe (trim (replace (replace (replace (replace (replace .ChannelName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}
⏰ {{mdsafe .TimeInfo}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (trim (replace (replace (replace (replace (replace .Title "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " ")))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$new$),
    ('CMD_STATS_GAINERS', $old$## 📊 구독자 증가 순위{{if .Period}} ({{.Period}}){{end}}
{{range .Gainers}}
{{.Rank}}. **{{mdsafe .MemberName}}** +{{.Delta}}명{{if .Current}} (현재 {{.Current}}명){{end}}
{{- end}}$old$, $new$📊 구독자 증가 순위{{if .Period}} · {{mdsafe (trim (replace (replace (replace (replace (replace .Period "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}{{end}}
{{- range $i, $item := .Gainers}}
{{- if gt $i 0}}

──────────
{{- end}}

{{.Rank}} · {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}
증가: +{{.Delta}}명{{if .Current}} · 현재: {{.Current}}명{{end}}
{{- end}}$new$),
    ('CMD_CALENDAR', $old${{- if eq .Count 0 -}}
📅 {{.Year}}년 {{.Month}}월 등록된 기념일이 없습니다.
{{- else -}}
## 📅 {{.Year}}년 {{.Month}}월 기념일 ({{.Count}})
{{- range .Days}}

**{{printf "%02d/%02d" .Month .Day}}**
{{- range .Entries}}
{{- if .IsBirthday}}
- 🎂 {{mdsafe .Name}} 생일
{{- else}}
- 🎉 {{mdsafe .Name}} 데뷔 {{.Years}}주년
{{- end}}
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
{{if .IsBirthday}}🎂 {{mdsafe (trim (replace (replace (replace (replace (replace .Name "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 생일{{else}}🎉 {{mdsafe (trim (replace (replace (replace (replace (replace .Name "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 데뷔 {{.Years}}주년{{end}}
{{- end}}
{{- end}}
{{- end -}}$new$),
    ('CMD_MEMBER_NOT_LIVE', $old${{mdsafe .MemberName}}은(는) 현재 방송 중이 아닙니다.$old$, $new${{printf "\u200b"}}{{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}: 현재 방송 중이 아닙니다.$new$),
    ('CMD_MEMBER_NO_UPCOMING', $old${{mdsafe .MemberName}}은(는) {{.Hours}}시간 이내 예정된 방송이 없습니다.$old$, $new${{printf "\u200b"}}{{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}: {{.Hours}}시간 이내 예정된 방송이 없습니다.$new$),
    ('CMD_MEMBER_NOT_FOUND', $old$❌ '{{mdsafe .MemberName}}' 멤버를 찾을 수 없습니다.$old$, $new$❌ '{{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}' 멤버를 찾을 수 없습니다.$new$),
    ('CMD_AMBIGUOUS_MEMBER', $old$동일한 이름의 멤버가 여러 명 있습니다.
{{range .Candidates}}{{.Index}}. {{mdsafe .Name}}
{{end}}
예) `{{.Prefix}}{{.CommandExample}}` {{mdsafe .FirstName}}$old$, $new$동일한 이름의 멤버가 여러 명 있습니다.
{{range .Candidates}}{{.Index}} · {{mdsafe (trim (replace (replace (replace (replace (replace .Name "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}
{{end}}
예) {{mdsafe .Prefix}}{{mdsafe .CommandExample}} {{mdsafe (trim (replace (replace (replace (replace (replace .FirstName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}$new$),
    ('CMD_MAJOR_EVENT_WEEKLY_SUMMARY', $old$## 📅 이번 주 행사 ({{.Count}})
{{- if .LLMSummary}}

{{.LLMSummary}}
{{- end}}
{{range $index, $event := .Events}}
{{- if and $event.Title $event.Link}}
{{add $index 1}}. [{{mdsafe $event.Title}}]({{$event.Link}})
{{- else if $event.Title}}
{{add $index 1}}. {{mdsafe $event.Title}}
{{- else}}
{{add $index 1}}. {{$event.Link}}
{{- end}}
{{- if $event.DateStr}}
   ⏰ {{$event.DateStr}}
{{- end}}
{{- if $event.Members}}
   {{mdsafe $event.Members}}
{{- end}}
{{- end}}$old$, $new$📅 이번 주 행사 · {{.Count}}개
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
{{- end}}$new$),
    ('ALARM_DISPATCH_NOTIFICATION', $old$## {{if .IsStarting}}🔴 **{{mdsafe .MemberName}}** {{if .IsPremiere}}선행공개{{else}}방송{{end}} 시작{{else if .IsScheduled}}⏰ **{{mdsafe .MemberName}}** {{if .IsPremiere}}선행공개{{else}}방송{{end}} 예정{{else}}⏰ **{{mdsafe .MemberName}}** {{if .IsPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전{{end}}
{{- $url := .URL}}
{{- $parts := split $url " | "}}
{{- $shortLinkURL := hasPrefix $url "https://short.holoshi.com/l/"}}
{{- $youtubeURL := or $shortLinkURL (hasPrefix $url "https://www.youtube.com/watch?") (hasPrefix $url "https://youtube.com/watch?") (hasPrefix $url "https://m.youtube.com/watch?") (hasPrefix $url "https://www.youtube.com/live/") (hasPrefix $url "https://youtube.com/live/") (hasPrefix $url "https://youtu.be/")}}
{{- $trustedURL := or $youtubeURL (hasPrefix $url "https://www.twitch.tv/") (hasPrefix $url "https://twitch.tv/") (hasPrefix $url "https://chzzk.naver.com/live/")}}
{{- $delimiterSafe := and (not (contains $url "\t")) (not (contains $url "\n")) (not (contains $url "\r")) (not (contains $url "(")) (not (contains $url ")")) (not (contains $url "[")) (not (contains $url "]")) (not (contains $url "<")) (not (contains $url ">")) (not (contains $url "\\"))}}
{{- $safeURL := and $url $trustedURL $delimiterSafe (not (contains $url " ")) (not (contains $url "|"))}}
{{- $composite := and (eq (len $parts) 2) $youtubeURL (hasPrefix (index $parts 1) "https://chzzk.naver.com/live/") $delimiterSafe (not (contains (index $parts 0) " ")) (not (contains (index $parts 1) " "))}}
{{- $linkable := and .Title $safeURL}}
{{- if $linkable}}
[{{mdsafe .Title}}]({{.URL}})
{{- else if .Title}}
{{printf "\u200b"}}{{mdsafe .Title}}
{{- end}}
{{- if .CollabMembers}}
콜라보: {{mdsafe .CollabMembers}}
{{- end}}
{{- if .ScheduleMessage}}
{{printf "\u200b"}}{{mdsafe .ScheduleMessage}}
{{- end}}
{{- if and .URL (not $linkable)}}
{{if or $safeURL $composite}}{{.URL}}{{else}}{{printf "\u200b"}}{{mdsafe (replace (replace .URL "\n" " ") "\r" " ")}}{{end}}
{{- end}}$old$, $new${{if .IsStarting}}🔴 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} 시작{{else if .IsScheduled}}⏰ {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} 예정{{else}}⏰ {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전{{end}}
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
{{- end}}$new$),
    ('CMD_ALARM_LIST', $old${{- if eq .Count 0 -}}
🔔 설정된 알람이 없습니다.
예) `{{.Prefix}}알람 추가 페코라`
{{- else -}}
## 🔔 알람 ({{.Count}})
{{range $index, $alarm := .Alarms}}
{{add $index 1}}. **{{mdsafe $alarm.MemberName}}**{{if $alarm.TypesLabel}} ({{mdsafe $alarm.TypesLabel}}){{end}}
{{- if $alarm.NextStream}}
{{- if eq $alarm.NextStream.Status "live"}}
   🔴 방송 중
{{- if and $alarm.NextStream.Title $alarm.NextStream.URL}}
   [{{mdsafe $alarm.NextStream.Title}}]({{$alarm.NextStream.URL}})
{{- else if $alarm.NextStream.Title}}
   {{mdsafe $alarm.NextStream.Title}}
{{- else if $alarm.NextStream.URL}}
   {{$alarm.NextStream.URL}}
{{- end}}
{{- else if eq $alarm.NextStream.Status "upcoming"}}
   ⏰ {{if $alarm.NextStream.StartingSoon}}곧 시작{{else}}{{$alarm.NextStream.ScheduledKST}}{{if $alarm.NextStream.TimeDetail}} ({{$alarm.NextStream.TimeDetail}}){{end}}{{end}}
{{- if and $alarm.NextStream.Title $alarm.NextStream.URL}}
   [{{mdsafe $alarm.NextStream.Title}}]({{$alarm.NextStream.URL}})
{{- else if $alarm.NextStream.Title}}
   {{mdsafe $alarm.NextStream.Title}}
{{- else if $alarm.NextStream.URL}}
   {{$alarm.NextStream.URL}}
{{- end}}
{{- end}}
{{- end}}
{{- end}}
{{- end -}}$old$, $new${{- if eq .Count 0 -}}
🔔 설정된 알람이 없습니다.
예) {{mdsafe .Prefix}}알람 추가 페코라
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
{{- end -}}$new$),
    ('CMD_ALARM_NOTIFICATION', $old$⏰ {{mdsafe .ChannelName}} 방송 예정
{{if .ScheduledTimeKST}}{{mdsafe .ScheduledTimeKST}} 시작{{else}}곧 시작{{end}}
{{- if .ScheduleMessage}}
{{mdsafe .ScheduleMessage}}
{{- end}}
{{- if .Title}}
{{mdsafe (truncate 64 (trim (replace (replace (replace .Title "\r\n" " ") "\r" " ") "\n" " ")))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$old$, $new$⏰ {{mdsafe (trim (replace (replace (replace (replace (replace .ChannelName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 방송 예정
{{if .ScheduledTimeKST}}{{mdsafe .ScheduledTimeKST}} 시작{{else}}곧 시작{{end}}
{{- if .ScheduleMessage}}
{{printf "\u200b"}}{{replace (replace (mdsafe .ScheduleMessage) "\r\n" "\n") "\n" "\n\u200b"}}
{{- end}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (trim (replace (replace (replace (replace (replace .Title "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " ")))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$new$),
    ('CMD_ALARM_LIVE_STARTED', $old$🔴 {{mdsafe .ChannelName}} 방송 시작
{{- if .ScheduledTimeKST}}
{{mdsafe .ScheduledTimeKST}} 시작
{{- end}}
{{- if .Title}}
{{mdsafe (truncate 64 (trim (replace (replace (replace .Title "\r\n" " ") "\r" " ") "\n" " ")))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$old$, $new$🔴 {{mdsafe (trim (replace (replace (replace (replace (replace .ChannelName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 방송 시작
{{- if .ScheduledTimeKST}}
{{mdsafe .ScheduledTimeKST}} 시작
{{- end}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (trim (replace (replace (replace (replace (replace .Title "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " ")))}}
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
{{- if .Title}}
{{mdsafe (truncate 64 (trim (replace (replace (replace .Title "\r\n" " ") "\r" " ") "\n" " ")))}}
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
{{printf "\u200b"}}{{mdsafe (truncate 64 (trim (replace (replace (replace (replace (replace .Title "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " ")))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end}}$new$),
    ('CMD_ALARM_CLEARED', $old${{- if eq .Count 0 -}}
🔔 설정된 알람이 없습니다.
{{- else -}}
✅ 알람 **{{.Count}}개**를 모두 해제했습니다.
{{- end -}}$old$, $new${{- if eq .Count 0 -}}
🔔 설정된 알람이 없습니다.
{{- else -}}
✅ 알람 {{.Count}}개를 모두 해제했습니다.
{{- end -}}$new$),
    ('CMD_MEMBER_NEWS_SUBSCRIBED', $old$✅ 뉴스 알림을 켰습니다.
- 발송: **매주 월요일 09:00 KST**$old$, $new$✅ 뉴스 알림을 켰습니다.
발송: 매주 월요일 09:00 KST$new$),
    ('CMD_MAJOR_EVENT_SUBSCRIBED', $old$✅ 행사 알림을 켰습니다.
- 발송: **매주 행사 요약**$old$, $new$✅ 행사 알림을 켰습니다.
발송: 매주 행사 요약$new$),
    ('X_SPACE_STARTED', $old$🔴 {{mdsafe .MemberName}} 스페이스 시작
{{- if .Title}}
{{mdsafe (truncate 64 (trim (replace (replace (replace .Title "\r\n" " ") "\r" " ") "\n" " ")))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$old$, $new$🔴 {{mdsafe (trim (replace (replace (replace (replace (replace .MemberName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}} 스페이스 시작
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (trim (replace (replace (replace (replace (replace .Title "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " ")))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}$new$)
) AS seed(template_key, old_body, new_body)
WHERE target.template_key = seed.template_key
  AND target.channel_id IS NULL
  AND target.body = seed.old_body
  AND target.body IS DISTINCT FROM seed.new_body;
