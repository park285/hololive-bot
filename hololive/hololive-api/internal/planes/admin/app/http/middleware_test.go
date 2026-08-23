package apphttp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	jsonv2 "encoding/json/v2"
	"github.com/gin-gonic/gin"
)

func TestCorsOriginGuard_ForbiddenResponseContract(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(corsOriginGuard([]string{"https://allowed.example"}, true, nil))
	router.GET("/api/holo/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/holo/test", http.NoBody)
	req.Header.Set("Origin", "https://blocked.example")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d want=%d", rec.Code, http.StatusForbidden)
	}

	var payload map[string]any
	if err := jsonv2.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if payload["error"] != "forbidden" {
		t.Fatalf("error=%v want=%q", payload["error"], "forbidden")
	}
}
