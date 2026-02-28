-- Migration: member news digest 템플릿에 Summary 표시 추가
-- Date: 2026-02-19

UPDATE notification_templates
SET body = '{{- if .Headline -}}
{{.Headline}}
{{- else -}}
🗞️ 구독 멤버 뉴스
{{- end -}}
{{- if eq (len .TopItems) 0 }}
- 표시할 뉴스가 없습니다.
{{- else }}
{{range $index, $item := .TopItems}}
{{- if gt $index 0 }}

{{- end -}}
{{add $index 1}}. [{{$item.DateText}}] {{$item.Member}} | {{$item.Category}}
   📌 {{$item.Title}}
   {{- if $item.Summary}}
   💬 {{$item.Summary}}
   {{- end}}
   🔗 {{$item.SourceURL}}
{{- end}}
{{- if .MoreSummary }}

{{.MoreSummary}}
{{- end }}
{{- end }}'
WHERE template_key = 'CMD_MEMBER_NEWS_DIGEST'
  AND channel_id IS NULL
  AND body NOT LIKE '%Summary%';
