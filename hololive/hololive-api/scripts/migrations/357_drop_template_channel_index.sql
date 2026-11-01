-- 355가 대체한 키·채널당 override 하나 제약을 지워 358이 같은 채널의 richtext override를 넣을 수 있게 한다.
DROP INDEX CONCURRENTLY IF EXISTS ux_notification_templates_channel;
