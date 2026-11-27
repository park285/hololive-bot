DELETE FROM bot_webhook_nonces
WHERE digest IN (
    SELECT digest
    FROM bot_webhook_nonces
    WHERE expires_at <= ?1
    ORDER BY expires_at
    LIMIT 64
)
