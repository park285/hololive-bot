BEGIN;
SET LOCAL lock_timeout = '3s';

UPDATE public.source_observation_queue AS queue
SET status = 'DEAD_LETTER',
    lease_owner = NULL,
    lease_token = NULL,
    lease_expires_at = NULL,
    processed_at = NULL,
    dead_lettered_at = pg_catalog.clock_timestamp(),
    last_error_code = 'unsupported_contract',
    last_error_detail = 'holodex channel_profile contract is retired',
    updated_at = pg_catalog.clock_timestamp()
FROM public.source_observations AS observation
WHERE observation.id = queue.observation_id
  AND observation.provider = 'holodex'
  AND observation.observation_kind = 'channel_profile'
  AND queue.status IN ('PENDING', 'PROCESSING');

DROP TRIGGER reject_retired_holodex_channel_profile ON public.source_observations;
DROP TRIGGER terminalize_retired_holodex_channel_profile ON public.source_observation_queue;
DROP FUNCTION public.reject_retired_holodex_channel_profile();
DROP FUNCTION public.terminalize_retired_holodex_channel_profile();

DROP TRIGGER bot_command_execution_terminal_summary_scrub ON public.bot_command_executions;
DROP FUNCTION public.scrub_bot_command_execution_terminal_summary();

DROP TRIGGER bot_reply_outbox_replay_claim_audit ON public.bot_reply_outbox;
CREATE TRIGGER bot_reply_outbox_replay_claim_audit BEFORE UPDATE ON public.bot_reply_outbox
    FOR EACH ROW
    WHEN (new.status = 'submitting' AND old.status <> 'submitting' AND new.operator_replay_grants > 0)
    EXECUTE FUNCTION public.append_bot_reply_outbox_replay_claim_audit();

ALTER TABLE public.source_collection_checkpoints SET (fillfactor = 80);

COMMIT;
