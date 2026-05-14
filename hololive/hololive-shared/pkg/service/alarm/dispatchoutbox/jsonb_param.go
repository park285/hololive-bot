package dispatchoutbox

// jsonbRecordsetParam returns text JSON for PostgreSQL jsonb_to_recordset($1::jsonb).
//
// pgx can encode []byte as bytea in some query contexts, which makes
// jsonb_to_recordset($1::jsonb) fail or behave unexpectedly. Keep this helper
// at every jsonb_to_recordset batch boundary so callers pass text JSON.
func jsonbRecordsetParam(raw []byte) string {
	return string(raw)
}
