SELECT id, value
FROM dbx_tx_test
WHERE jsonb_build_object('k', 1) ? 'k'
  AND value LIKE '%?'
  AND value = ANY($1::text[])
ORDER BY id
