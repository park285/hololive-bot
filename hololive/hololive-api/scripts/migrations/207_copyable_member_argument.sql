-- 복사 예제의 멤버명에도 ZWSP가 남지 않도록 명령 전체를 한 번 이스케이프합니다.
-- 205 표준 본문만 갱신하며 사용자 지정 본문은 보존합니다.
UPDATE notification_templates AS target
SET body = seed.new_body, updated_at = now()
FROM (VALUES
    ('CMD_AMBIGUOUS_MEMBER', $old$동일한 이름의 멤버가 여러 명 있습니다.
{{range .Candidates}}{{.Index}} · {{mdsafe (trim (replace (replace (replace (replace (replace .Name "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}
{{end}}
예) {{mdescape .Prefix}}{{mdsafe .CommandExample}} {{mdsafe (trim (replace (replace (replace (replace (replace .FirstName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}$old$, $new$동일한 이름의 멤버가 여러 명 있습니다.
{{range .Candidates}}{{.Index}} · {{mdsafe (trim (replace (replace (replace (replace (replace .Name "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " "))}}
{{end}}
예) {{mdescape (printf "%s%s %s" .Prefix .CommandExample (trim (replace (replace (replace (replace (replace .FirstName "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " ")))}}$new$)
) AS seed(template_key, old_body, new_body)
WHERE target.template_key = seed.template_key
  AND target.channel_id IS NULL
  AND target.body = seed.old_body
  AND target.body IS DISTINCT FROM seed.new_body;
