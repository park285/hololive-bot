package api

import (
	"net/http/httptest"
	"testing"

	json "github.com/park285/shared-go/pkg/json"
)

func assertErrorResponse(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantMessage string) {
	t.Helper()

	if rec.Code != wantStatus {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, wantStatus, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v body=%s", err, rec.Body.String())
	}

	if payload["error"] != wantMessage {
		t.Fatalf("error=%v want=%q body=%s", payload["error"], wantMessage, rec.Body.String())
	}
}
