BEGIN;
SET LOCAL lock_timeout = '3s';

DROP TABLE public.bot_reply_outbox_replay_audit;
DROP TABLE public.bot_reply_outbox_resolution_audit;
DROP TABLE public.bot_webhook_heads;
DROP TABLE public.bot_reply_outbox;
DROP TABLE public.bot_webhook_inbox;
DROP TABLE public.bot_command_executions;
DROP TABLE public.bot_webhook_nonces;

DROP FUNCTION public.append_bot_reply_outbox_replay_claim_audit();
DROP FUNCTION public.discard_bot_reply_outbox_manual_review(bigint, text, text, text);
DROP FUNCTION public.enforce_bot_reply_outbox_discard_audit();
DROP FUNCTION public.grant_bot_reply_outbox_manual_replay(bigint, text, text);
DROP FUNCTION public.reject_bot_reply_outbox_replay_audit_mutation();
DROP FUNCTION public.reject_bot_reply_outbox_resolution_audit_mutation();
DROP FUNCTION public.reserve_bot_webhook_nonce(bytea, bigint);

COMMIT;
