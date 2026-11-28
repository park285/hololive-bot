INSERT INTO bot_webhook_nonces AS nonce (digest, expires_at)
VALUES (?1, ?2 + ?3)
ON CONFLICT (digest) DO UPDATE SET expires_at = excluded.expires_at
WHERE nonce.expires_at <= ?2
