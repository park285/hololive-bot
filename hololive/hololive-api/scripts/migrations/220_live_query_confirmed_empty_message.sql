-- 확인이 끝난 빈 !라이브 결과만 새 표준 문구를 사용한다. 확인 불가는 formatter가 구분한다.
-- 217 표준 전역 본문만 바꾸며 사용자 지정 본문·채널 override·CMD_MEMBER_NOT_LIVE는 보존한다.
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
{{if gt $i 0}}──────────{{end}}
{{add $i 1}} · {{mdsafe (displayline .ChannelName)}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Title))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$old$, $new${{- if eq .Count 0 -}}
현재 방송 중인 멤버가 없습니다.
{{- else -}}
🔴 방송 중 · {{.Count}}개
{{- if gt .Count (len .Streams)}}
전체 {{.Count}}개 중 {{len .Streams}}개 표시
{{- end}}
{{- range $i, $stream := .Streams}}
{{if gt $i 0}}──────────{{end}}
{{add $i 1}} · {{mdsafe (displayline .ChannelName)}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Title))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}$new$)
) AS seed(template_key, old_body, new_body)
WHERE target.template_key = seed.template_key
  AND target.channel_id IS NULL
  AND target.body = seed.old_body
  AND target.body IS DISTINCT FROM seed.new_body;
