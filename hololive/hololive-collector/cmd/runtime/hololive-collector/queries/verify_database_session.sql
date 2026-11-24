SELECT current_database() = 'hololive' AND current_user = $1::name AND session_user = $1::name
	AND current_setting('default_transaction_read_only') = 'on' AND current_setting('transaction_read_only') = 'on'
	AND current_setting('statement_timeout') = '30s' AND current_setting('lock_timeout') = '3s'
	AND EXISTS (SELECT 1 FROM pg_catalog.pg_stat_ssl WHERE pid = pg_backend_pid() AND ssl AND version = 'TLSv1.3')
