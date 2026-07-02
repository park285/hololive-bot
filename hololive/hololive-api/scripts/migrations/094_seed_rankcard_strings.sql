BEGIN;

INSERT INTO message_strings(namespace, key, value) VALUES
  ('rankcard','header','구독자 증가 순위'),
  ('rankcard','summary','%s · 상위 %d'),
  ('rankcard','total','구독자 %s')
ON CONFLICT (namespace, key) DO UPDATE SET value = EXCLUDED.value;

COMMIT;
