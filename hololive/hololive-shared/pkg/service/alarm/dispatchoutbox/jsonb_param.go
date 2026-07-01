package dispatchoutbox

// jsonbRecordsetParam은 PostgreSQL jsonb_to_recordset($1::jsonb)용 text JSON을 반환한다.
//
// pgx는 일부 query context에서 []byte를 bytea로 인코딩할 수 있고, 그러면
// jsonb_to_recordset($1::jsonb)가 실패하거나 예기치 않게 동작한다. 모든
// jsonb_to_recordset batch 경계에서 이 helper를 거쳐 caller가 text JSON을 넘기게 한다.
func jsonbRecordsetParam(raw []byte) string {
	return string(raw)
}
