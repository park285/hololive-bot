SELECT
    nonce.digest,
    floor(extract(epoch FROM nonce.expires_at) * 1000)::bigint
FROM public.bot_webhook_nonces AS nonce
ORDER BY nonce.digest
