-- 기존 방송 시작 알림과 동일한 표시 및 Markdown 무력화 규칙을 사용합니다.
-- 재적용 시 운영자가 수정한 템플릿을 덮어쓰지 않습니다.
INSERT INTO notification_templates (template_key, channel_id, body)
SELECT 'X_SPACE_STARTED', NULL, $template$## 🔴 **{{mdsafe .MemberName}}** 스페이스 시작
{{- if .Title}}
[{{mdsafe .Title}}]({{.URL}})
{{- else}}
{{.URL}}
{{- end}}$template$
WHERE NOT EXISTS (
    SELECT 1 FROM notification_templates
    WHERE template_key = 'X_SPACE_STARTED' AND channel_id IS NULL
);
