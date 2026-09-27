-- YouTube delivery ledger backfill(youtube-delivery-ledger-backfill 명령과 alarm-worker backfill 패키지)을 지우고, alarm-worker
-- TransitionStore가 매 lifecycle 전이 전에 youtube_notification_delivery_ledger_state의 완료 표식을 읽던 ensureReady gate도
-- 지운다(DEC-20260926-hololive-retired-rollback-tooling, PLN-20260926-stack-audit-refactoring T19 holo-youtube-ledger-backfill-tool).
-- gate가 막던 전제(migration 190 이전 delivery·outbox 행의 ledger가 채워짐)는 이 migration이 적용 시점에 한 번 확인한다.
-- 운영 singleton은 2026-09-01 완료됐고 2026-09-26 T18에서 schema_version=1, completed_at 있음을 다시 확인했다.
--
-- 거절 조건: ① 완료되지 않았거나 schema_version이 1이 아닌 state 행이 있다. ② state 행이 없는데 delivery나 outbox 행이 있다
-- (backfill 없이 190을 지난 DB). 빈 DB의 fresh bootstrap은 두 조건 모두 해당하지 않는다. 거절되면 이전 revision의 backfill
-- 명령으로 완료한 뒤 다시 적용한다. 스키마는 바꾸지 않으므로 재실행해도 같은 검사만 반복한다.
-- 번호는 225 다음이다(live-evidence 218~220 뒤로 재번호).
DO $migration$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM youtube_notification_delivery_ledger_state
        WHERE completed_at IS NULL
           OR schema_version <> 1
    ) THEN
        RAISE EXCEPTION 'youtube delivery ledger backfill is not complete; finish it with the previous revision before applying 226';
    END IF;

    IF NOT EXISTS (SELECT 1 FROM youtube_notification_delivery_ledger_state)
       AND (
           EXISTS (SELECT 1 FROM youtube_notification_delivery)
           OR EXISTS (SELECT 1 FROM youtube_notification_outbox)
       ) THEN
        RAISE EXCEPTION 'youtube delivery ledger backfill never ran on a database with delivery rows; run it with the previous revision before applying 226';
    END IF;
END
$migration$;
