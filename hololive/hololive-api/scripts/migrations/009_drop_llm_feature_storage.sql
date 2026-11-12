-- 의존 객체가 남아 DROP이 실패하면 트랜잭션 전체가 되돌아가고 다음 실행이 처음부터 다시 하므로 CASCADE를 쓰지 않습니다.
-- 도움말은 본문 전체 CAS 대신 폐기 명령 구획이 표준 문구와 정확히 일치할 때 그 구획만 지워, 기본·채널 행의 나머지 수정을 보존합니다.
BEGIN;

DROP TABLE IF EXISTS public.notification_delivery_outbox;
DROP TABLE IF EXISTS public.major_event_subscriptions;
DROP TABLE IF EXISTS public.major_events;
DROP TABLE IF EXISTS public.member_news_subscriptions;

DELETE FROM public.notification_templates
WHERE template_key IN (
    'CMD_MAJOR_EVENT_SUBSCRIBED', 'CMD_MAJOR_EVENT_UNSUBSCRIBED', 'CMD_MAJOR_EVENT_ALREADY_SUB', 'CMD_MAJOR_EVENT_NOT_SUB',
    'CMD_MAJOR_EVENT_STATUS', 'CMD_MAJOR_EVENT_USAGE', 'CMD_MAJOR_EVENT_WEEKLY_SUMMARY', 'CMD_MAJOR_EVENT_MONTHLY_SUMMARY',
    'CMD_MEMBER_NEWS_DIGEST', 'CMD_MEMBER_NEWS_SUBSCRIBED', 'CMD_MEMBER_NEWS_UNSUBSCRIBED', 'CMD_MEMBER_NEWS_ALREADY_SUB',
    'CMD_MEMBER_NEWS_NOT_SUB', 'CMD_MEMBER_NEWS_STATUS', 'CMD_MEMBER_NEWS_NO_MEMBERS'
);

UPDATE public.notification_templates
SET body = replace(body, section, '')
FROM (VALUES
    ('plain', E'\n\n[뉴스]\n{{mdescape .Prefix}}뉴스 - 주간 뉴스 요약\n{{mdescape .Prefix}}뉴스알림 켜기 / 끄기 / 상태\n\n[행사]\n{{mdescape .Prefix}}행사 - 행사 알림 상태\n{{mdescape .Prefix}}행사 켜기 / 끄기'),
    ('richtext', E'\n\n**뉴스**\n{{mdescape .Prefix}}뉴스 - 주간 뉴스 요약\n{{mdescape .Prefix}}뉴스알림 켜기 / 끄기 / 상태\n\n**행사**\n{{mdescape .Prefix}}행사 - 행사 알림 상태\n{{mdescape .Prefix}}행사 켜기 / 끄기')
) AS retired(format_variant, section)
WHERE notification_templates.template_key = 'CMD_HELP'
  AND notification_templates.format_variant = retired.format_variant
  AND strpos(notification_templates.body, retired.section) > 0;

DELETE FROM public.message_strings
WHERE namespace = 'newscat'
   OR (namespace = 'error' AND key IN (
        'major_event_service_not_initialized', 'major_event_status_check_failed', 'major_event_subscribe_failed',
        'major_event_unsubscribe_failed', 'member_news_service_not_initialized', 'member_news_query_failed',
        'member_news_subscription_failed'))
   OR (namespace = 'notify' AND key IN (
        'member_news_no_members', 'member_news_subscribed', 'member_news_already_subscribed', 'member_news_unsubscribed',
        'member_news_not_subscribed', 'member_news_status_on', 'member_news_status_off'));

COMMIT;
