DROP TABLE IF EXISTS _msg_tone_audit;
CREATE TEMP TABLE _msg_tone_audit(
  ord              int  PRIMARY KEY,
  migration        text NOT NULL,
  ledger_applied   boolean NOT NULL DEFAULT false,
  artifact_present boolean NOT NULL DEFAULT false,
  verdict          text
);

DO $$
DECLARE
  has_ms  boolean := to_regclass('public.message_strings')        IS NOT NULL;
  has_nt  boolean := to_regclass('public.notification_templates') IS NOT NULL;
  has_led boolean := to_regclass('public.schema_migrations')      IS NOT NULL;
  a087 boolean := false; a088 boolean := false; a089 boolean := false; a090 boolean := false;
BEGIN
  IF has_nt THEN
    SELECT EXISTS (SELECT 1 FROM notification_templates WHERE template_key = 'CMD_HELP' AND channel_id IS NULL AND body LIKE '홀로라이브 봇 명령어%') INTO a087;
    SELECT EXISTS (SELECT 1 FROM notification_templates WHERE template_key = 'ALARM_DISPATCH_NOTIFICATION' AND channel_id IS NULL AND body LIKE '{{if .IsStarting}}🔴%') INTO a088;
  END IF;

  IF has_ms THEN
    SELECT EXISTS (SELECT 1 FROM message_strings WHERE namespace = 'error' AND key = 'unknown_command' AND value LIKE '❌ 알 수 없는 명령입니다.%') INTO a089;
    SELECT (SELECT count(key) FROM message_strings WHERE namespace = 'timefmt') = 6
       AND (SELECT count(key) FROM message_strings WHERE namespace = 'karing')  = 20 INTO a090;
  END IF;

  INSERT INTO _msg_tone_audit(ord, migration, artifact_present) VALUES
    (1, '087_reseed_command_templates_minimal_tone.sql',      a087),
    (2, '088_reseed_notification_templates_minimal_tone.sql', a088),
    (3, '089_reseed_error_notify_strings_minimal_tone.sql',   a089),
    (4, '090_seed_timefmt_karing_strings.sql',                a090);

  IF has_led THEN
    UPDATE _msg_tone_audit t SET ledger_applied = true
      FROM schema_migrations s WHERE s.filename = t.migration;
  END IF;

  UPDATE _msg_tone_audit SET verdict = CASE
    WHEN ledger_applied AND artifact_present      THEN 'OK'
    WHEN ledger_applied AND NOT artifact_present  THEN 'DAMAGED'
    WHEN NOT ledger_applied AND artifact_present  THEN 'APPLIED_UNMARKED'
    ELSE 'PENDING'
  END;
END $$;

SELECT migration, ledger_applied, artifact_present, verdict
FROM _msg_tone_audit ORDER BY ord;

SELECT verdict, count(ord) AS n
FROM _msg_tone_audit GROUP BY verdict ORDER BY verdict;

-- channel override 행은 재시드가 덮지 않아 새 톤이 적용되지 않는다(보존 정책) — 대상 방 가시화용.
SELECT template_key, channel_id, updated_at
FROM notification_templates
WHERE channel_id IS NOT NULL
ORDER BY template_key, channel_id;
