INSERT INTO dbx_tx_test (value) SELECT unnest($1::text[])
