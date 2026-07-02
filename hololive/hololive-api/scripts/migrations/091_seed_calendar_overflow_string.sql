BEGIN;

INSERT INTO message_strings(namespace, key, value) VALUES
  ('calendar','overflow_footer','외 %d건 생략')
ON CONFLICT (namespace, key) DO UPDATE SET value = EXCLUDED.value;

COMMIT;
