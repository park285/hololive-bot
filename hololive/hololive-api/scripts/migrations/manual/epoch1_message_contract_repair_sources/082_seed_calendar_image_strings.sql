BEGIN;

INSERT INTO message_strings(namespace, key, value) VALUES
  ('calendar','header_month','%d년 %d월 기념일'),
  ('calendar','summary','총 %d건 · 생일 %d · 데뷔주년 %d'),
  ('calendar','empty','등록된 기념일이 없습니다.'),
  ('calendar','day','%d월 %d일'),
  ('calendar','badge_birthday','생일'),
  ('calendar','badge_anniversary','데뷔 %d주년'),
  ('calendar','unknown','알 수 없음')
ON CONFLICT (namespace, key) DO NOTHING;

COMMIT;
