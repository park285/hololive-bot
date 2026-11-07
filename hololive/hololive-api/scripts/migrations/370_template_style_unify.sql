-- 사용자 노출 기본 템플릿을 MESSAGE_STYLE_GUIDE에 맞춰 통일하고 알림 템플릿의 링크 판정을 줄인다.
-- - 알림 4개: URL마다 반복하던 신뢰 출처 판정을 trustedlink 함수로 바꾼다(링크 표시 결과는 같다).
-- - plain 변형을 richtext와 같은 머리 문단·항목 구성으로 맞춘다(방송 목록·알림 묶음의 요약 줄, 시간·날짜 위치,
--   뉴스 항목 한 줄). 두 변형은 강조·번호·일반채팅용 ZWSP 보호만 다르다.
-- - 허용 목록 밖 글리프(📖·⏱)를 지우고, 빈 결과와 행사·뉴스 구독 문구를 같은 형식으로 맞춘다.
-- trustedlink는 1854eb0af(#754) 바이너리부터 있으므로 API와 alarm-worker가 그 이후 revision으로 배포된 뒤 적용한다.
-- 직전 표준 기본값(sha256으로 판정)만 바꾸고 사용자 지정 기본값과 채널 override는 그대로 둔다.
-- 반복 적용은 해시 불일치로 아무것도 바꾸지 않는다. 본문이 바뀐 행은 trigger가 row_version을 올려 렌더러 캐시가 새 본문을 읽는다.
BEGIN;
CREATE TEMP TABLE template_style_unify_seed (
    template_key TEXT NOT NULL,
    format_variant TEXT NOT NULL,
    standard_sha256 TEXT NOT NULL,
    body TEXT NOT NULL,
    PRIMARY KEY (template_key, format_variant)
) ON COMMIT DROP;

INSERT INTO template_style_unify_seed (template_key, format_variant, standard_sha256, body) VALUES
    ('ALARM_DISPATCH_NOTIFICATION', 'plain', 'ef52fa14403c66e0f85a763379bebc0cb6be24ec9da0edd171c5b05211b6f24d', $body${{if .IsStarting}}🔴 {{mdsafe (displayline .MemberName)}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} 시작{{else if .IsScheduled}}⏰ {{mdsafe (displayline .MemberName)}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} 예정{{else}}⏰ {{mdsafe (displayline .MemberName)}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전{{end}}
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
{{with trustedlink .URL}}{{.}}{{else}}{{printf "\u200b"}}{{mdsafe (replace (replace $.URL "\n" " ") "\r" " ")}}{{end}}
{{- end}}$body$),
    ('ALARM_DISPATCH_NOTIFICATION', 'richtext', '2b0f7fd1cb7d766714ee9478533567f03337245a02d7f2789628bc2a2bd8af3d', $body${{if .IsStarting}}🔴 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} 시작{{else if .IsScheduled}}⏰ {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} 예정{{else}}⏰ {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전{{end}}
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
{{with trustedlink .URL}}{{.}}{{else}}{{mdsafe (replace (replace $.URL "\n" " ") "\r" " ")}}{{end}}
{{- end}}$body$),
    ('ALARM_DISPATCH_NOTIFICATION_GROUP', 'plain', '7b9bbdadb2762c1dba4a136fbdb2d8249edbad993297dcf7092ce6b10d432167', $body${{if .IsStarting}}🔴 {{if .AllPremiere}}선행공개{{else}}방송{{end}} 시작{{else}}⏰ {{if .AllPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전{{end}} · {{len .Entries}}개
{{- $names := 0}}
{{- range .Entries}}{{with displayline .MemberName}}{{if $names}} · {{else}}
{{printf "\u200b"}}{{end}}{{mdsafe .}}{{$names = add $names 1}}{{end}}{{end}}
{{- range $i, $entry := .Entries}}
{{if gt $i 0}}──────────{{end}}
{{add $i 1}} · {{with displayline .MemberName}}{{mdsafe .}} · {{end}}{{if .IsStarting}}{{if .IsPremiere}}선행공개{{else}}방송{{end}} 시작{{else if .IsScheduled}}{{if .IsPremiere}}선행공개{{else}}방송{{end}} 예정{{else}}{{if .IsPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전{{end}}
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
{{with trustedlink .URL}}{{.}}{{else}}{{printf "\u200b"}}{{mdsafe (replace (replace $entry.URL "\n" " ") "\r" " ")}}{{end}}
{{- end}}
{{- end}}$body$),
    ('ALARM_DISPATCH_NOTIFICATION_GROUP', 'richtext', 'dacd414548921458e806c4ddd8cfa203008c58fbe5abe7f592054927c2ccbab2', $body${{if .IsStarting}}🔴 **{{if .AllPremiere}}선행공개{{else}}방송{{end}} 시작**{{else}}⏰ **{{if .AllPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전**{{end}} · {{len .Entries}}개
{{- $names := 0}}
{{- range .Entries}}{{with displayline .MemberName}}{{if $names}} · {{else}}
{{end}}{{mdsafe .}}{{$names = add $names 1}}{{end}}{{end}}
{{- range $i, $entry := .Entries}}
{{if gt $i 0}}──────────{{end}}
{{with displayline .MemberName}}**{{mdsafe .}}** · {{end}}{{if .IsStarting}}{{if .IsPremiere}}선행공개{{else}}방송{{end}} 시작{{else if .IsScheduled}}{{if .IsPremiere}}선행공개{{else}}방송{{end}} 예정{{else}}{{if .IsPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전{{end}}
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
{{with trustedlink .URL}}{{.}}{{else}}{{mdsafe (replace (replace $entry.URL "\n" " ") "\r" " ")}}{{end}}
{{- end}}
{{- end}}$body$),
    ('CELEBRATION_BIRTHDAY_STREAM', 'plain', '390ca59b545dea7af5053875b5db1bb4f4df7d5439d20621df9e9852de11ebdb', $body$🎂 {{mdsafe (displayline .MemberName)}} 생일 방송{{if .ScheduledStartKST}} · {{mdsafe .ScheduledStartKST}}{{end}}
{{- if trim .StreamTitle}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .StreamTitle))}}
{{- end}}
{{- if .StreamURL}}
{{.StreamURL}}
{{- end}}$body$),
    ('CMD_AMBIGUOUS_MEMBER', 'plain', 'd89e08ba316ddb3c4cd01dc0f35b4ea67fd645d56745000a7b8b0180391474bb', $body$같은 이름의 멤버가 여러 명 있습니다.
{{range .Candidates}}{{.Index}} · {{mdsafe (displayline .Name)}}
{{end}}
예) {{mdescape (printf "%s%s %s" .Prefix .CommandExample (displayline .FirstName))}}$body$),
    ('CMD_CHANNEL_SCHEDULE', 'plain', 'e181897e601cf461f0e81d773cbe11d31f519c3daac3a8a8f893b0bf2471f4bc', $body${{- if not .ChannelName -}}
❌ 채널 정보를 찾을 수 없습니다.
{{- else if eq .Count 0 -}}
📅 {{mdsafe (displayline .ChannelName)}}: {{.Days}}일 이내 예정된 방송이 없습니다.
{{- else -}}
📅 {{mdsafe (displayline .ChannelName)}} 일정 · {{.Count}}개 · {{.Days}}일 이내
{{- range $i, $stream := .Streams}}{{if eq $i 0}}
{{if .IsLive}}지금 방송 중{{else}}다음: {{mdsafe .TimeInfo}}{{end}}{{end}}{{end}}
{{- if gt .Count (len .Streams)}}
전체 {{.Count}}개 중 {{len .Streams}}개 표시
{{- end}}
{{- range $i, $stream := .Streams}}
{{if gt $i 0}}──────────{{end}}
{{add $i 1}} · {{if .IsLive}}🔴 방송 중{{else}}{{mdsafe .TimeInfo}}{{end}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Title))}}
{{- end}}
{{- if .CollabMembers}}
콜라보: {{mdsafe (displayline .CollabMembers)}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$body$),
    ('CMD_CHANNEL_SCHEDULE', 'richtext', 'ae13e6d0e4ecc8d4ea9f012b9c201dd8757441c41f295818e05e5220e36e3ab2', $body${{- if not (displayline .ChannelName) -}}
❌ 채널 정보를 찾을 수 없습니다.
{{- else if eq .Count 0 -}}
📅 {{with displayline .ChannelName}}**{{mdsafe .}}**{{end}}: {{.Days}}일 이내 예정된 방송이 없습니다.
{{- else -}}
📅 {{with displayline .ChannelName}}**{{mdsafe .}}**{{end}} 일정 · {{.Count}}개 · {{.Days}}일 이내
{{- range $i, $stream := .Streams}}{{if eq $i 0}}
{{if .IsLive}}지금 방송 중{{else}}다음: {{mdsafe .TimeInfo}}{{end}}{{end}}{{end}}
{{- if gt .Count (len .Streams)}}
전체 {{.Count}}개 중 {{len .Streams}}개 표시
{{- end}}
{{- range $i, $stream := .Streams}}
{{if gt $i 0}}──────────{{end}}
{{if .IsLive}}🔴 **방송 중**{{else}}**{{mdsafe .TimeInfo}}**{{end}}
{{- with displayline .Title}}
{{mdsafe (truncate 64 .)}}
{{- end}}
{{- if .CollabMembers}}
콜라보: {{mdsafe (displayline .CollabMembers)}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$body$),
    ('CMD_HELP', 'plain', '50920f12df007366ce132a53df2c69b9052f8580c12eb963603640bc67ec73cb', $body$홀로라이브 봇 명령어

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
{{mdescape .Prefix}}도움말 - 도움말$body$),
    ('CMD_HELP', 'richtext', 'b9433fc5d3bb4e0e9c09557457eefbf8a13bc92f0182f85bde2276e5bbd3ede7', $body$**홀로라이브 봇 명령어**

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
{{mdescape .Prefix}}도움말 - 도움말$body$),
    ('CMD_LIVE_STREAMS', 'plain', '9aca655fb7474e56d632e4e8ca5052d1963d27a751c0ed0f06d2fc560bbdc67a', $body${{- if eq .Count 0 -}}
현재 방송 중인 멤버가 없습니다.
{{- else -}}
🔴 방송 중 · {{.Count}}개
{{- $names := 0}}
{{- range .Streams}}{{with displayline .ChannelName}}{{if $names}} · {{else}}
{{printf "\u200b"}}{{end}}{{mdsafe .}}{{$names = add $names 1}}{{end}}{{end}}
{{- if gt .Count (len .Streams)}}
전체 {{.Count}}개 중 {{len .Streams}}개 표시
{{- end}}
{{- range $i, $stream := .Streams}}
{{if gt $i 0}}──────────{{end}}
{{add $i 1}} · {{mdsafe (displayline .ChannelName)}}{{- if .Elapsed}} · {{.Elapsed}}{{- end}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Title))}}
{{- end}}
{{- if .CollabMembers}}
콜라보: {{mdsafe (displayline .CollabMembers)}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$body$),
    ('CMD_MAJOR_EVENT_MONTHLY_SUMMARY', 'plain', '12f545f5cbd23673eadc5b64e72e014dfa8b9849d54e5607d96c43547a874606', $body$📅 이번 달 행사 · {{.Count}}개
{{- if .LLMSummary}}

{{.LLMSummary}}
{{- end}}
{{- range $i, $event := .Events}}
{{if gt $i 0}}──────────{{end}}
{{add $i 1}} · {{if trim $event.Title}}{{mdsafe (truncate 64 (displayline $event.Title))}}{{else}}행사{{end}}
{{- if $event.DateStr}}
{{printf "\u200b"}}{{mdsafe (displayline $event.DateStr)}}
{{- end}}
{{- if $event.Members}}
참여: {{mdsafe (displayline $event.Members)}}
{{- end}}
{{- if $event.Link}}
{{$event.Link}}
{{- end}}
{{- end}}$body$),
    ('CMD_MAJOR_EVENT_NOT_SUB', 'plain', 'd847330052c8ebc233709e838a9426e428377d55c3a3bc8599fe16ed86b4fd34', $body$ℹ️ 행사 알림이 이미 꺼져 있습니다.$body$),
    ('CMD_MAJOR_EVENT_NOT_SUB', 'richtext', 'd847330052c8ebc233709e838a9426e428377d55c3a3bc8599fe16ed86b4fd34', $body$ℹ️ 행사 알림이 이미 꺼져 있습니다.$body$),
    ('CMD_MAJOR_EVENT_STATUS', 'plain', '3e26ff4f791240b348ade1dbb8e47b5261091789067c10f33cccc726a94e981b', $body${{if .IsSubscribed}}🔔{{else}}🔕{{end}} 행사 알림: {{if .IsSubscribed}}켜짐{{else}}꺼짐{{end}}
{{- if .IsSubscribed}}
발송: 매주 월요일 09:00 · 매월 1일 10:00 KST
해제: {{mdescape .Prefix}}행사 끄기
{{- else}}
설정: {{mdescape .Prefix}}행사 켜기
{{- end}}$body$),
    ('CMD_MAJOR_EVENT_STATUS', 'richtext', '1ed0aef8f84e24f388ffa3124bf9b12c8cc3774304c8bad3802277c4b8e65d79', $body${{if .IsSubscribed}}🔔{{else}}🔕{{end}} 행사 알림: **{{if .IsSubscribed}}켜짐{{else}}꺼짐{{end}}**
{{- if .IsSubscribed}}
발송: 매주 월요일 09:00 · 매월 1일 10:00 KST
해제: {{mdescape .Prefix}}행사 끄기
{{- else}}
설정: {{mdescape .Prefix}}행사 켜기
{{- end}}$body$),
    ('CMD_MAJOR_EVENT_SUBSCRIBED', 'plain', '6ba1b29bb10e702ced4d04b7cda54a1b7a5ebeaa6af6bb2048a146a5037154ed', $body$✅ 행사 알림을 켰습니다.
발송: 매주 월요일 09:00 · 매월 1일 10:00 KST$body$),
    ('CMD_MAJOR_EVENT_SUBSCRIBED', 'richtext', '6ba1b29bb10e702ced4d04b7cda54a1b7a5ebeaa6af6bb2048a146a5037154ed', $body$✅ 행사 알림을 켰습니다.
발송: 매주 월요일 09:00 · 매월 1일 10:00 KST$body$),
    ('CMD_MAJOR_EVENT_WEEKLY_SUMMARY', 'plain', '40d8c09506f8d63bfe050ab0f9637efff9c9b96db7256d1f84dda29d7b7148d5', $body$📅 이번 주 행사 · {{.Count}}개
{{- if .LLMSummary}}

{{.LLMSummary}}
{{- end}}
{{- range $i, $event := .Events}}
{{if gt $i 0}}──────────{{end}}
{{add $i 1}} · {{if trim $event.Title}}{{mdsafe (truncate 64 (displayline $event.Title))}}{{else}}행사{{end}}
{{- if $event.DateStr}}
{{printf "\u200b"}}{{mdsafe (displayline $event.DateStr)}}
{{- end}}
{{- if $event.Members}}
참여: {{mdsafe (displayline $event.Members)}}
{{- end}}
{{- if $event.Link}}
{{$event.Link}}
{{- end}}
{{- end}}$body$),
    ('CMD_MEMBER_NEWS_DIGEST', 'plain', '9d205871c096be92d5863bccb530e8d1686a530181779f82db90415c3647972e', $body${{- if trim .Headline -}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Headline))}}
{{- else -}}
📰 멤버 뉴스
{{- end}}
{{- if eq (len .TopItems) 0}}
표시할 뉴스가 없습니다.
{{- else}}
{{- range $i, $item := .TopItems}}
{{if gt $i 0}}──────────{{end}}
{{add $i 1}} · {{with displayline $item.Member}}{{mdsafe .}}{{else}}뉴스{{end}}{{if $item.DateText}} · {{mdsafe (displayline $item.DateText)}}{{end}}{{if $item.Category}} · {{mdsafe (displayline $item.Category)}}{{end}}
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
{{- end}}$body$),
    ('CMD_MEMBER_NO_UPCOMING', 'plain', '0dc6bc65813e0778750646b746015740b02615b91bbecabe46eb33d27f144e51', $body$📅 {{mdsafe (displayline .MemberName)}}: {{.Hours}}시간 이내 예정된 방송이 없습니다.$body$),
    ('CMD_MEMBER_NO_UPCOMING', 'richtext', '8dda01b69f46230e62216122893eae2662282264980637230909f8f05c82c2ad', $body$📅 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}}: {{.Hours}}시간 이내 예정된 방송이 없습니다.$body$),
    ('CMD_UPCOMING_STREAMS', 'plain', '35d286826849876d22e2212342185ac588d23550f8b041ed592847a0e3ddd9b5', $body${{- if eq .Count 0 -}}
📅 {{.Hours}}시간 이내 예정된 방송이 없습니다.
{{- else -}}
📅 예정 방송 · {{.Count}}개 · {{.Hours}}시간 이내
{{- range $i, $stream := .Streams}}{{if eq $i 0}}
다음: {{with displayline .ChannelName}}{{mdsafe .}} · {{end}}{{mdsafe .TimeInfo}}{{end}}{{end}}
{{- if gt .Count (len .Streams)}}
전체 {{.Count}}개 중 {{len .Streams}}개 표시
{{- end}}
{{- range $i, $stream := .Streams}}
{{if gt $i 0}}──────────{{end}}
{{add $i 1}} · {{with displayline .ChannelName}}{{mdsafe .}} · {{end}}{{mdsafe .TimeInfo}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Title))}}
{{- end}}
{{- if .CollabMembers}}
콜라보: {{mdsafe (displayline .CollabMembers)}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$body$),
    ('CMD_UPCOMING_STREAMS', 'richtext', '1a970ed48296814684770303b890364472dc0b7457af006324e08a350cd65742', $body${{- if eq .Count 0 -}}
📅 {{.Hours}}시간 이내 예정된 방송이 없습니다.
{{- else -}}
📅 **예정 방송** · {{.Count}}개 · {{.Hours}}시간 이내
{{- range $i, $stream := .Streams}}{{if eq $i 0}}
다음: {{with displayline .ChannelName}}{{mdsafe .}} · {{end}}{{mdsafe .TimeInfo}}{{end}}{{end}}
{{- if gt .Count (len .Streams)}}
전체 {{.Count}}개 중 {{len .Streams}}개 표시
{{- end}}
{{- range $i, $stream := .Streams}}
{{if gt $i 0}}──────────{{end}}
{{with displayline .ChannelName}}**{{mdsafe .}}** · {{end}}{{mdsafe .TimeInfo}}
{{- with displayline .Title}}
{{mdsafe (truncate 64 .)}}
{{- end}}
{{- if .CollabMembers}}
콜라보: {{mdsafe (displayline .CollabMembers)}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$body$);

UPDATE notification_templates AS target
SET body = seed.body, updated_at = now()
FROM template_style_unify_seed AS seed
WHERE target.template_key = seed.template_key
  AND target.format_variant = seed.format_variant
  AND target.channel_id IS NULL
  AND encode(sha256(convert_to(target.body, 'UTF8')), 'hex') = seed.standard_sha256;
COMMIT;
