-- ledger엔 074-082가 applied인데 SQL은 실제로 안 돌던 2026-06-28 baseline-마킹 사고(apply-all.sh
-- 7a66082a에서 fix)로 오염된 DB를 찾는다. 읽기 전용이라 prod에 그대로 psql -f 해도 안전하다.

DROP TABLE IF EXISTS _msg_contract_audit;
CREATE TEMP TABLE _msg_contract_audit(
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
  a074 boolean;          a076 boolean := false; a077 boolean := false; a078 boolean := false;
  a079 boolean := false; a080 boolean := false; a081 boolean := false; a082 boolean := false;
BEGIN
  a074 := has_ms;

  IF has_nt THEN
    SELECT EXISTS (SELECT 1 FROM notification_templates WHERE template_key = 'CMD_PROFILE'                 AND channel_id IS NULL) INTO a076;
    SELECT EXISTS (SELECT 1 FROM notification_templates WHERE template_key = 'CELEBRATION_BIRTHDAY'        AND channel_id IS NULL) INTO a077;
    SELECT NOT EXISTS (SELECT 1 FROM notification_templates WHERE template_key = 'OUTBOX_VIDEO' AND channel_id IS NULL AND body NOT LIKE '{{if eq .Kind%') INTO a078;
    SELECT EXISTS (SELECT 1 FROM notification_templates WHERE template_key = 'CMD_AMBIGUOUS_MEMBER'        AND channel_id IS NULL) INTO a080;
    SELECT EXISTS (SELECT 1 FROM notification_templates WHERE template_key = 'ALARM_DISPATCH_NOTIFICATION' AND channel_id IS NULL) INTO a081;
  END IF;

  IF has_ms THEN
    SELECT EXISTS (SELECT 1 FROM message_strings WHERE namespace = 'error'    AND key = 'unknown_command') INTO a079;
    SELECT EXISTS (SELECT 1 FROM message_strings WHERE namespace = 'calendar' AND key = 'header_month')   INTO a082;
  END IF;

  INSERT INTO _msg_contract_audit(ord, migration, artifact_present) VALUES
    (1, '074_create_message_strings.sql',                  a074),
    (2, '076_seed_new_command_templates.sql',              a076),
    (3, '077_seed_notification_celebration_templates.sql', a077),
    (4, '078_unify_outbox_header_body_templates.sql',      a078),
    (5, '079_seed_error_strings.sql',                      a079),
    (6, '080_refresh_help_and_ambiguous.sql',              a080),
    (7, '081_seed_canonical_alarm_templates.sql',          a081),
    (8, '082_seed_calendar_image_strings.sql',             a082);

  IF has_led THEN
    UPDATE _msg_contract_audit t SET ledger_applied = true
      FROM schema_migrations s WHERE s.filename = t.migration;
  END IF;

  UPDATE _msg_contract_audit SET verdict = CASE
    WHEN ledger_applied AND artifact_present     THEN 'OK'
    WHEN ledger_applied AND NOT artifact_present  THEN 'DAMAGED'
    WHEN NOT ledger_applied AND artifact_present  THEN 'APPLIED_UNMARKED'
    ELSE 'PENDING'
  END;
END $$;

SELECT migration, ledger_applied, artifact_present, verdict
FROM _msg_contract_audit ORDER BY ord;

SELECT verdict, count(*) AS n
FROM _msg_contract_audit GROUP BY verdict ORDER BY verdict;
