package apphttp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	json "github.com/park285/shared-go/pkg/json"
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
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if payload["error"] != "forbidden" {
		t.Fatalf("error=%v want=%q", payload["error"], "forbidden")
	}
}
