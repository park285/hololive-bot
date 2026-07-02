-- 키셋과 ❌/⚠️ 글리프 접두는 messages_seed_parity_test.go 계약 — 값만 갱신한다.
-- notify/member_news_* 값은 087의 CMD_MEMBER_NEWS_* 템플릿 폴백 쌍이므로 문구를 동일하게 유지한다.

BEGIN;

INSERT INTO message_strings(namespace, key, value) VALUES
  ('error','no_member_info_found','❌ 등록된 멤버 정보를 찾을 수 없습니다.'),
  ('error','cannot_display_member_info','❌ 멤버 정보를 표시할 수 없습니다.'),
  ('error','member_profile_load_failed','❌ 프로필을 불러오는 중 오류가 발생했습니다.'),
  ('error','member_profile_build_failed','❌ 프로필을 구성하지 못했습니다.'),
  ('error','graduated_member_blocked','⚠️ 졸업한 멤버입니다.'),
  ('error','alarm_service_not_initialized','❌ 알람 서비스가 초기화되지 않았습니다.'),
  ('error','alarm_add_failed','❌ 알람 설정 중 오류가 발생했습니다.'),
  ('error','alarm_remove_failed','❌ 알람 제거 중 오류가 발생했습니다.'),
  ('error','alarm_list_failed','❌ 알람 목록 조회 중 오류가 발생했습니다.'),
  ('error','alarm_clear_failed','❌ 알람 초기화 중 오류가 발생했습니다.'),
  ('error','alarm_need_member_name_add','❌ 멤버 이름을 입력해주세요.
예) !알람 추가 페코라'),
  ('error','alarm_need_member_name_remove','❌ 멤버 이름을 입력해주세요.
예) !알람 제거 페코라'),
  ('error','invalid_alarm_usage','❌ 지원하지 않는 알람 명령입니다.
예) !알람 추가 페코라'),
  ('error','live_stream_query_failed','❌ 라이브 조회 중 오류가 발생했습니다.'),
  ('error','upcoming_stream_query_failed','❌ 예정 방송 조회 중 오류가 발생했습니다.'),
  ('error','schedule_query_failed','❌ 일정 조회 중 오류가 발생했습니다.'),
  ('error','schedule_need_member_name','❌ 멤버 이름을 입력해주세요.
예) !일정 페코라'),
  ('error','unknown_stats_period','❌ 알 수 없는 통계 유형입니다. !도움말을 참고해주세요.'),
  ('error','stats_query_failed','❌ 구독자 순위 조회 중 오류가 발생했습니다.'),
  ('error','no_stats_data','❌ 해당 기간의 통계 데이터가 없습니다.'),
  ('error','subscriber_need_member_name','❌ 멤버 이름을 입력해주세요.
예) !구독자 페코라'),
  ('error','subscriber_query_failed','❌ 구독자 정보 조회 중 오류가 발생했습니다.'),
  ('error','no_subscriber_data','❌ 해당 멤버의 구독자 정보가 없습니다.'),
  ('error','calendar_query_failed','❌ 기념일 조회 중 오류가 발생했습니다.'),
  ('error','major_event_service_not_initialized','❌ 행사 알림 서비스가 초기화되지 않았습니다.'),
  ('error','major_event_status_check_failed','❌ 행사 알림 상태 확인 중 오류가 발생했습니다.'),
  ('error','major_event_subscribe_failed','❌ 행사 알림 설정 중 오류가 발생했습니다.'),
  ('error','major_event_unsubscribe_failed','❌ 행사 알림 해제 중 오류가 발생했습니다.'),
  ('error','member_news_service_not_initialized','❌ 뉴스 서비스가 초기화되지 않았습니다.'),
  ('error','member_news_query_failed','❌ 뉴스 조회 중 오류가 발생했습니다.'),
  ('error','member_news_subscription_failed','❌ 뉴스 알림 설정 중 오류가 발생했습니다.'),
  ('error','unknown_command','❌ 알 수 없는 명령입니다.
!도움말에서 사용 가능한 명령을 확인할 수 있습니다.'),
  ('error','external_api_call_failed','❌ 외부 데이터 조회 중 오류가 발생했습니다. 잠시 후 다시 시도해주세요.'),
  ('error','cache_connection_failed','❌ 일시적인 문제로 요청을 처리하지 못했습니다. 잠시 후 다시 시도해주세요.'),
  ('error','iris_connection_failed','❌ 서버 연결에 실패했습니다. 잠시 후 다시 시도해주세요.'),
  ('error','command_processing_failed','❌ 명령 처리 중 오류가 발생했습니다.'),
  ('error','async_command_backpressure','❌ 요청이 많아 잠시 후 다시 시도해주세요.')
ON CONFLICT (namespace, key) DO UPDATE SET value = EXCLUDED.value, updated_at = now();

INSERT INTO message_strings(namespace, key, value) VALUES
  ('notify','member_news_no_members','📰 뉴스 대상 멤버가 없습니다.
예) !알람 추가 페코라'),
  ('notify','member_news_subscribed','✅ 뉴스 알림을 켰습니다.
매주 월요일 09:00 KST에 발송됩니다.'),
  ('notify','member_news_already_subscribed','ℹ️ 뉴스 알림이 이미 켜져 있습니다.'),
  ('notify','member_news_unsubscribed','✅ 뉴스 알림을 껐습니다.'),
  ('notify','member_news_not_subscribed','ℹ️ 뉴스 알림이 이미 꺼져 있습니다.'),
  ('notify','member_news_status_on','🔔 뉴스 알림: 켜짐
- 발송: 매주 월요일 09:00 KST
- 해제: !뉴스알림 끄기'),
  ('notify','member_news_status_off','🔕 뉴스 알림: 꺼짐
- 설정: !뉴스알림 켜기'),
  ('notify','graduated_member_warning','⚠️ 졸업한 멤버입니다.

')
ON CONFLICT (namespace, key) DO UPDATE SET value = EXCLUDED.value, updated_at = now();

COMMIT;
