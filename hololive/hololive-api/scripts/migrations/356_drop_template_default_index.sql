-- 354가 대체한 키당 기본 본문 하나 제약을 지워 358이 같은 키의 richtext 기본 행을 넣을 수 있게 한다.
DROP INDEX CONCURRENTLY IF EXISTS ux_notification_templates_default;
