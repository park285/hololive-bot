-- timefmt/karing 값은 Go 코드가 fmt.Sprintf 포맷 문자열로 사용한다 — %s/%d 자리수·순서가 코드 계약(파라미터 검증 테스트와 co-commit).

BEGIN;

INSERT INTO message_strings(namespace, key, value) VALUES
  ('timefmt','stream_time_days','%s (%d일 후)'),
  ('timefmt','stream_time_hours_minutes','%s (%d시간 %d분 후)'),
  ('timefmt','stream_time_minutes','%s (%d분 후)'),
  ('timefmt','relative_days','%d일 후'),
  ('timefmt','relative_hours_minutes','%d시간 %d분 후'),
  ('timefmt','relative_minutes','%d분 후')
ON CONFLICT (namespace, key) DO UPDATE SET value = EXCLUDED.value, updated_at = now();

INSERT INTO message_strings(namespace, key, value) VALUES
  ('karing','alarm_title_prelive','방송 %d분 전 알림'),
  ('karing','alarm_title_live','라이브 시작'),
  ('karing','time_left_prelive','%d분 후 시작'),
  ('karing','time_left_live','지금 시작'),
  ('karing','outbox_title_community','커뮤니티 알림'),
  ('karing','outbox_time_community','새 커뮤니티'),
  ('karing','outbox_title_shorts','쇼츠 알림'),
  ('karing','outbox_time_shorts','새 쇼츠'),
  ('karing','outbox_title_video','새 영상'),
  ('karing','outbox_time_video','새 영상'),
  ('karing','outbox_title_live','방송 알림'),
  ('karing','outbox_time_live','방송 알림'),
  ('karing','title_fallback','알림'),
  ('karing','time_fallback','새 알림'),
  ('karing','count_suffix','%s · %d건'),
  ('karing','item_title_community_fallback','커뮤니티 알림'),
  ('karing','status_community','커뮤니티'),
  ('karing','status_shorts','쇼츠'),
  ('karing','status_video','새 영상'),
  ('karing','status_fallback','알림')
ON CONFLICT (namespace, key) DO UPDATE SET value = EXCLUDED.value, updated_at = now();

COMMIT;
