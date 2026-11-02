-- 오픈채팅의 긴 RichText 메시지는 말풍선에 첫 줄 묶음만 보이고 나머지는 "전체 보기" 뒤로 접힌다. 방송 목록·예정 방송·
-- 멤버 일정·알람 묶음은 머리말 한 줄만 보여 누가 방송하는지 펼쳐야 알 수 있었으므로, 머리말 바로 다음 줄에 요약
-- (방송 중·알림 대상 멤버 이름, 다음 예정 방송)을 둔다. 기간·표시 개수 안내는 머리말이나 그 아래로 옮긴다.
-- 뉴스 요약은 제목(DefaultHeadline 등)이 이미 이모지로 시작해 앞에 붙이던 📰가 겹쳤으므로, 제목이 있으면 그대로 쓴다.
-- 358이 쓴 표준 richtext 본문(sha256으로 판정)인 전역 기본값만 바꾼다. 사용자 지정 기본값, 채널 override와 plain 행은
-- 그대로 두고, 반복 적용은 해시 불일치로 아무것도 바꾸지 않는다. 본문이 바뀐 행은 trigger가 row_version을 올려 실행 중인
-- 렌더러 캐시가 새 본문을 읽는다.
BEGIN;
CREATE TEMP TABLE template_richtext_summary_seed (
    template_key TEXT PRIMARY KEY,
    standard_sha256 TEXT NOT NULL,
    richtext_body TEXT NOT NULL
) ON COMMIT DROP;

INSERT INTO template_richtext_summary_seed (template_key, standard_sha256, richtext_body) VALUES
    ('ALARM_DISPATCH_NOTIFICATION_GROUP', 'b3147a4d75d165bc00d23171b05cf54673855768ab31e9365b4aada2751bd4a6', $rich${{if .IsStarting}}🔴 **{{if .AllPremiere}}선행공개{{else}}방송{{end}} 시작**{{else}}⏰ **{{if .AllPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전**{{end}} · {{len .Entries}}개
{{- $names := 0}}
{{- range .Entries}}{{with displayline .MemberName}}{{if $names}} · {{else}}
{{end}}{{mdsafe .}}{{$names = add $names 1}}{{end}}{{end}}
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
    ('CMD_CHANNEL_SCHEDULE', '17e998c818d83598775aff11bc2cfe1394a0ea777c7c5eb0c0b14ec52bebcfef', $rich${{- if not (displayline .ChannelName) -}}
❌ 채널 정보를 찾을 수 없습니다.
{{- else if eq .Count 0 -}}
📅 {{with displayline .ChannelName}}**{{mdsafe .}}**{{end}}
{{.Days}}일 이내 예정된 방송이 없습니다.
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
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$rich$),
    ('CMD_LIVE_STREAMS', '7d08ab96b316b5ed85d4dfc45ccbb08563c481e51644d63afe503213280a0d66', $rich${{- if eq .Count 0 -}}
현재 방송 중인 멤버가 없습니다.
{{- else -}}
🔴 **방송 중** · {{.Count}}개
{{- $names := 0}}
{{- range .Streams}}{{with displayline .ChannelName}}{{if $names}} · {{else}}
{{end}}{{mdsafe .}}{{$names = add $names 1}}{{end}}{{end}}
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
    ('CMD_MEMBER_NEWS_DIGEST', 'e00e93e210f5e4e199c0da1b58ad9cb9efff5f7ae713c20b55122450de6dbefb', $rich${{- with displayline .Headline -}}
**{{mdsafe (truncate 64 .)}}**
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
    ('CMD_UPCOMING_STREAMS', '56c3eedabba2aae0c3b9da56efda5642671c4c3000397b6764f8638024a14877', $rich${{- if eq .Count 0 -}}
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
{{with displayline .ChannelName}}**{{mdsafe .}}**{{end}} · {{mdsafe .TimeInfo}}
{{- with displayline .Title}}
{{mdsafe (truncate 64 .)}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$rich$);

UPDATE notification_templates AS target
SET body = seed.richtext_body, updated_at = now()
FROM template_richtext_summary_seed AS seed
WHERE target.template_key = seed.template_key
  AND target.channel_id IS NULL
  AND target.format_variant = 'richtext'
  AND encode(sha256(convert_to(target.body, 'UTF8')), 'hex') = seed.standard_sha256;
COMMIT;
