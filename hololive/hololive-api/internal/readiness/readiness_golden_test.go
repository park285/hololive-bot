package readiness

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGoldenReadyAllHealthy(t *testing.T) {
	t.Parallel()

	code, body := serveReadyGolden(t, GinHandler(t.Context(), NewProbe("bot", okCheck("postgres"), okCheck("valkey"))))

	if code != http.StatusOK {
		t.Fatalf("/ready status = %d, want %d", code, http.StatusOK)
	}
	want := `{"dependencies":{"postgres":true,"valkey":true},"goroutines":0,"plane":"bot","status":"ready","uptime":"UPTIME","version":"VERSION"}`
	if body != want {
		t.Fatalf("/ready body = %s, want %s", body, want)
	}
}

func TestGoldenReadyDependencyDown(t *testing.T) {
	t.Parallel()

	code, body := serveReadyGolden(t, GinHandler(t.Context(), NewProbe("admin",
		okCheck("postgres"),
		failCheck("valkey", errors.New("connection refused")),
	)))

	if code != http.StatusServiceUnavailable {
		t.Fatalf("/ready status = %d, want %d", code, http.StatusServiceUnavailable)
	}
	want := `{"dependencies":{"postgres":true,"valkey":false},"goroutines":0,"plane":"admin","status":"not_ready","uptime":"UPTIME","version":"VERSION"}`
	if body != want {
		t.Fatalf("/ready body = %s, want %s", body, want)
	}
}

func TestGoldenReadyNilProbe(t *testing.T) {
	t.Parallel()

	code, body := serveReadyGolden(t, GinHandler(t.Context(), nil))

	if code != http.StatusOK {
		t.Fatalf("/ready status = %d, want %d", code, http.StatusOK)
	}
	want := `{"health":{"goroutines":0,"status":"ok","uptime":"UPTIME","version":"VERSION"},"status":"ready"}`
	if body != want {
		t.Fatalf("/ready body = %s, want %s", body, want)
	}
}

func serveReadyGolden(t *testing.T, handler gin.HandlerFunc) (statusCode int, canonicalBody string) {
	t.Helper()

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.GET("/ready", handler)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ready", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	return rec.Code, canonicalizeGolden(t, rec.Body.Bytes())
}

func canonicalizeGolden(t *testing.T, raw []byte) string {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("golden unmarshal: %v, raw=%s", err, raw)
	}
	normalizeGoldenDynamicFields(t, payload)
	if nested, ok := payload["health"].(map[string]any); ok {
		normalizeGoldenDynamicFields(t, nested)
	}
	out, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("golden marshal: %v", err)
	}
	return string(out)
}

func normalizeGoldenDynamicFields(t *testing.T, payload map[string]any) {
	t.Helper()

	for key, placeholder := range map[string]string{"version": "VERSION", "uptime": "UPTIME"} {
		value, exists := payload[key]
		if !exists {
			continue
		}
		if _, ok := value.(string); !ok {
			t.Fatalf("%s = %T, want string", key, value)
		}
		payload[key] = placeholder
	}
	if value, exists := payload["goroutines"]; exists {
		if _, ok := value.(float64); !ok {
			t.Fatalf("goroutines = %T, want number", value)
		}
		payload["goroutines"] = float64(0)
	}
}
