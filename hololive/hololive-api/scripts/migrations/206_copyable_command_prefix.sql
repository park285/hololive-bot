-- 명령어를 복사할 때 접두사 뒤 ZWSP가 남지 않도록 Markdown 이스케이프를 사용합니다.
-- mdescape 등록을 포함한 API/worker 바이너리 배포 후 적용하고 template cache를 갱신해야 합니다.
-- 204 표준 본문만 갱신하며 사용자 지정 default와 channel override는 보존합니다.
UPDATE notification_templates AS target
SET body = seed.new_body, updated_at = now()
FROM (VALUES
    ('CMD_HELP', $old$📖 홀로라이브 봇 명령어

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
{{mdsafe .Prefix}}도움말 - 도움말$old$, $new$📖 홀로라이브 봇 명령어

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
{{mdescape .Prefix}}구독자 [멤버명] - 구독자 수
{{mdescape .Prefix}}도움말 - 도움말$new$),
    ('CMD_MAJOR_EVENT_NOT_SUB', $old$ℹ️ 행사 알림이 꺼져 있습니다.
설정: {{.Prefix}}행사 켜기$old$, $new$ℹ️ 행사 알림이 꺼져 있습니다.
설정: {{mdescape .Prefix}}행사 켜기$new$),
    ('CMD_MAJOR_EVENT_STATUS', $old${{if .IsSubscribed}}🔔{{else}}🔕{{end}} 행사 알림: {{if .IsSubscribed}}켜짐{{else}}꺼짐{{end}}
{{- if .IsSubscribed}}
해제: {{.Prefix}}행사 끄기
{{- else}}
설정: {{.Prefix}}행사 켜기
{{- end}}$old$, $new${{if .IsSubscribed}}🔔{{else}}🔕{{end}} 행사 알림: {{if .IsSubscribed}}켜짐{{else}}꺼짐{{end}}
{{- if .IsSubscribed}}
해제: {{mdescape .Prefix}}행사 끄기
{{- else}}
설정: {{mdescape .Prefix}}행사 켜기
{{- end}}$new$),
    ('CMD_MAJOR_EVENT_USAGE', $old$🔔 행사 알림 명령어
{{.Prefix}}행사 켜기 / 끄기 / 상태$old$, $new$🔔 행사 알림 명령어
{{mdescape .Prefix}}행사 켜기 / 끄기 / 상태$new$),
    ('CMD_MEMBER_NEWS_NO_MEMBERS', $old$📰 뉴스 대상 멤버가 없습니다.
예) {{.Prefix}}알람 추가 페코라$old$, $new$📰 뉴스 대상 멤버가 없습니다.
예) {{mdescape .Prefix}}알람 추가 페코라$new$),
    ('CMD_MEMBER_NEWS_STATUS', $old${{if .IsSubscribed}}🔔{{else}}🔕{{end}} 뉴스 알림: {{if .IsSubscribed}}켜짐{{else}}꺼짐{{end}}
{{- if .IsSubscribed}}
발송: 매주 월요일 09:00 KST
해제: {{.Prefix}}뉴스알림 끄기
{{- else}}
설정: {{.Prefix}}뉴스알림 켜기
{{- end}}$old$, $new${{if .IsSubscribed}}🔔{{else}}🔕{{end}} 뉴스 알림: {{if .IsSubscribed}}켜짐{{else}}꺼짐{{end}}
{{- if .IsSubscribed}}
발송: 매주 월요일 09:00 KST
해제: {{mdescape .Prefix}}뉴스알림 끄기
{{- else}}
설정: {{mdescape .Prefix}}뉴스알림 켜기
{{- end}}$new$),
    ('CMD_AMBIGUOUS_MEMBER', $old$동일한 이름의 멤버가 여러 명 있습니다.
{{range .Candidates}}{{.Index}} · {{mdsafe (trim (replace (replace (replace (replace (replace .Name "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}
{{end}}
예) {{mdsafe .Prefix}}{{mdsafe .CommandExample}} {{mdsafe (trim (replace (replace (replace (replace (replace .FirstName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}$old$, $new$동일한 이름의 멤버가 여러 명 있습니다.
{{range .Candidates}}{{.Index}} · {{mdsafe (trim (replace (replace (replace (replace (replace .Name "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}
{{end}}
예) {{mdescape .Prefix}}{{mdsafe .CommandExample}} {{mdsafe (trim (replace (replace (replace (replace (replace .FirstName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}$new$),
    ('CMD_ALARM_LIST', $old${{- if eq .Count 0 -}}
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
{{- end -}}$old$, $new${{- if eq .Count 0 -}}
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
{{- end -}}$new$)
) AS seed(template_key, old_body, new_body)
WHERE target.template_key = seed.template_key
  AND target.channel_id IS NULL
  AND target.body = seed.old_body
  AND target.body IS DISTINCT FROM seed.new_body;
