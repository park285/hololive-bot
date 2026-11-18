SELECT COALESCE(rolpassword, '') FROM pg_catalog.pg_authid WHERE rolname = $1
