-- `alarm:next_stream:*`는 writer 없이 reader만 남아 있던 퇴역 Valkey key입니다. 코드가 알람 추가·목록 템플릿에
-- `NextStream`을 더 넘기지 않으므로, 한 번도 채워진 적 없는 다음 방송 분기를 표준 본문에서 제거합니다.
-- 새 본문의 출력은 `NextStream`이 비어 있을 때의 기존 출력과 같아 이전 이미지와도 호환됩니다.
-- 표준 본문과 같은 행은 전역·채널 override 모두 바꿉니다. 그 밖에 `NextStream`을 참조하는 알람 추가·목록 본문이
-- 남으면 새 코드에서 렌더가 실패하므로 한 transaction 안에서 적용 전체를 거절합니다(아무 행도 바뀌지 않음).
-- 운영자가 그 본문을 고친 뒤 다시 실행합니다.
BEGIN;

UPDATE notification_templates AS target
SET body = seed.new_body, updated_at = now()
FROM (VALUES
    ('CMD_ALARM_ADDED', $old${{- if .Added -}}
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
{{- end -}}$old$, $new${{- if .Added -}}
✅ {{mdsafe (displayline .MemberName)}} 알람을 설정했습니다.
방송 시작 5분 전에 알립니다.
{{- else -}}
ℹ️ {{mdsafe (displayline .MemberName)}} 알람이 이미 설정되어 있습니다.
{{- end -}}$new$),
    ('CMD_ALARM_LIST', $old${{- if eq .Count 0 -}}
🔔 설정된 알람이 없습니다.
예) {{mdescape .Prefix}}알람 추가 페코라
{{- else -}}
🔔 설정된 알람 · {{.Count}}개
{{- $prevDetail := false}}
{{- range $i, $alarm := .Alarms}}
{{- $detail := and $alarm.NextStream (or (eq $alarm.NextStream.Status "live") (eq $alarm.NextStream.Status "upcoming"))}}
{{if eq $i 0}}
{{else if or $detail $prevDetail}}──────────
{{end}}{{add $i 1}} · {{mdsafe (displayline $alarm.MemberName)}}{{if $alarm.TypesLabel}} ({{mdsafe (displayline $alarm.TypesLabel)}}){{end}}
{{- if $detail}}
{{- if eq $alarm.NextStream.Status "live"}}
🔴 방송 중
{{- else}}
⏰ {{if $alarm.NextStream.StartingSoon}}곧 시작{{else}}{{mdsafe $alarm.NextStream.ScheduledKST}}{{if $alarm.NextStream.TimeDetail}} ({{mdsafe $alarm.NextStream.TimeDetail}}){{end}}{{end}}
{{- end}}
{{- if trim $alarm.NextStream.Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline $alarm.NextStream.Title))}}
{{- end}}
{{- if $alarm.NextStream.URL}}
{{$alarm.NextStream.URL}}
{{- end}}
{{- end}}
{{- $prevDetail = $detail}}
{{- end -}}
{{- end -}}$old$, $new${{- if eq .Count 0 -}}
🔔 설정된 알람이 없습니다.
예) {{mdescape .Prefix}}알람 추가 페코라
{{- else -}}
🔔 설정된 알람 · {{.Count}}개
{{- range $i, $alarm := .Alarms}}
{{if eq $i 0}}
{{end}}{{add $i 1}} · {{mdsafe (displayline $alarm.MemberName)}}{{if $alarm.TypesLabel}} ({{mdsafe (displayline $alarm.TypesLabel)}}){{end}}
{{- end -}}
{{- end -}}$new$)
) AS seed(template_key, old_body, new_body)
WHERE target.template_key = seed.template_key
  AND target.body = seed.old_body;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM notification_templates
        WHERE template_key IN ('CMD_ALARM_ADDED', 'CMD_ALARM_LIST')
          AND body LIKE '%NextStream%'
    ) THEN
        RAISE EXCEPTION 'notification_templates still reference NextStream; rewrite CMD_ALARM_ADDED/CMD_ALARM_LIST custom bodies before applying 233';
    END IF;
END $$;

COMMIT;
