BEGIN;

INSERT INTO message_strings(namespace, key, value) VALUES
  ('profilecard','badge_graduated','졸업')
ON CONFLICT (namespace, key) DO UPDATE SET value = EXCLUDED.value;

COMMIT;
