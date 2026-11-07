-- !일정·!라이브·!예정 목록의 각 방송 제목 아래에 공식 스케줄의 콜라보 출연자 줄을 둔다. 알림(ALARM_DISPATCH_*)과 같은
-- "콜라보: 이름, 이름" 표기이며 출연자가 없으면 줄을 생략한다. 본문은 CollabMembers 뷰 필드에 기대므로 그 필드를 가진
-- 바이너리가 먼저 배포된 뒤에 적용한다(CONVENTIONS의 바이너리 선배포 규칙, 322 선례).
-- 358(plain)·359(richtext)가 쓴 표준 기본값(sha256으로 판정)만 바꾸고 사용자 지정 기본값과 채널 override는 그대로 둔다.
-- 반복 적용은 해시 불일치로 아무것도 바꾸지 않는다. 본문이 바뀐 행은 trigger가 row_version을 올려 렌더러 캐시가 새 본문을 읽는다.
BEGIN;
CREATE TEMP TABLE template_collab_members_seed (
    template_key TEXT NOT NULL,
    format_variant TEXT NOT NULL,
    standard_sha256 TEXT NOT NULL,
    body TEXT NOT NULL,
    PRIMARY KEY (template_key, format_variant)
) ON COMMIT DROP;

INSERT INTO template_collab_members_seed (template_key, format_variant, standard_sha256, body) VALUES
    ('CMD_CHANNEL_SCHEDULE', 'plain', 'bcd3477f46d0e756658b5c820def2b79f76376e248918168d8f70a8d96ebf261', $body${{- if not .ChannelName -}}
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
{{- if .CollabMembers}}
콜라보: {{mdsafe (displayline .CollabMembers)}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$body$),
    ('CMD_CHANNEL_SCHEDULE', 'richtext', 'df926f2469774222a153ee626593877e0a8523d86e6065508c0a350d21df1fb4', $body${{- if not (displayline .ChannelName) -}}
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
{{- if .CollabMembers}}
콜라보: {{mdsafe (displayline .CollabMembers)}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$body$),
    ('CMD_LIVE_STREAMS', 'plain', '7f1cbf78be06f3104dcc5403329a8900fd871c9be75d7ad96bebb22e8f2ab9cf', $body${{- if eq .Count 0 -}}
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
{{- if .CollabMembers}}
콜라보: {{mdsafe (displayline .CollabMembers)}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$body$),
    ('CMD_LIVE_STREAMS', 'richtext', 'e21da28de811dcfe2a824d50d42005357af21c8a34d265afcf80e224d462b15a', $body${{- if eq .Count 0 -}}
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
{{- if .CollabMembers}}
콜라보: {{mdsafe (displayline .CollabMembers)}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$body$),
    ('CMD_UPCOMING_STREAMS', 'plain', 'cf1f70c86522a2ad5b4f14c57ced3e5e3ecf040cd5a7b50121785edfad5af5bf', $body${{- if eq .Count 0 -}}
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
{{- if .CollabMembers}}
콜라보: {{mdsafe (displayline .CollabMembers)}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$body$),
    ('CMD_UPCOMING_STREAMS', 'richtext', '15196d5a00289b5978d1d79f0805f204036700dddadf5c899b83a317e220b7d1', $body${{- if eq .Count 0 -}}
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
FROM template_collab_members_seed AS seed
WHERE target.template_key = seed.template_key
  AND target.format_variant = seed.format_variant
  AND target.channel_id IS NULL
  AND encode(sha256(convert_to(target.body, 'UTF8')), 'hex') = seed.standard_sha256;
COMMIT;
