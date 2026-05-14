package dispatchoutbox

import "testing"

func TestJSONBRecordsetParamUsesTextJSON(t *testing.T) {
	raw := []byte(`[{"event_key":"event-1","payload":{"title":"test"}}]`)

	got := jsonbRecordsetParam(raw)

	if got != string(raw) {
		t.Fatalf("jsonbRecordsetParam() = %q, want %q", got, string(raw))
	}
}
