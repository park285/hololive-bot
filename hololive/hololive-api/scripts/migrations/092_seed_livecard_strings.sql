BEGIN;

INSERT INTO message_strings(namespace, key, value) VALUES
  ('livecard','header','현재 라이브'),
  ('livecard','summary','총 %d건'),
  ('livecard','badge_chzzk','치지직'),
  ('livecard','overflow_footer','외 %d건 생략')
ON CONFLICT (namespace, key) DO UPDATE SET value = EXCLUDED.value;

COMMIT;
