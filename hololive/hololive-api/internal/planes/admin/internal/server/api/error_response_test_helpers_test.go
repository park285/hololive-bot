package api

import (
	jsonv2 "encoding/json/v2"
	"net/http/httptest"
	"testing"
)

func assertErrorResponse(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantMessage string) {
	t.Helper()

	if rec.Code != wantStatus {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, wantStatus, rec.Body.String())
	}

	var payload map[string]any

	if err := jsonv2.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v body=%s", err, rec.Body.String())
	}

	if payload["error"] != wantMessage {
		t.Fatalf("error=%v want=%q body=%s", payload["error"], wantMessage, rec.Body.String())
	}
}
