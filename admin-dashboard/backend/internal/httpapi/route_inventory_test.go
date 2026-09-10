package httpapi

import (
	jsonv2 "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/kapu/admin-dashboard/internal/contract"
)

type baselineOperation struct {
	Method      string `json:"method"`
	Path        string `json:"path"`
	OperationID string `json:"operationId"`
	Access      string `json:"access"`
}

type baselineInventory struct {
	Operations           []baselineOperation `json:"operations"`
	RegisteredExceptions []baselineOperation `json:"registered_exceptions"`
}

// T01의 동결 목록을 실제 등록과 대조한다. SDK·validator 검증은 T02의 별도 조건이다.
func TestContractRouteInventory(t *testing.T) {
	baseline := readBaselineInventory(t)
	inventory := baselineInventory{}

	for _, operation := range contract.Operations() {
		inventory.Operations = append(inventory.Operations, baselineOperation{Method: operation.Method, Path: operation.Path, OperationID: operation.ID, Access: operation.Access})
	}

	for _, exception := range baseline.RegisteredExceptions {
		if exception.Path != "/admin/api/openapi.json" {
			inventory.RegisteredExceptions = append(inventory.RegisteredExceptions, exception)
		}
	}

	rt := newTestRuntime(t, storeWith(liveSession("inventory-session")), nil)
	t.Cleanup(rt.Close)

	engine, ok := rt.Handler().(*gin.Engine)
	require.True(t, ok)

	actual := baselineRouteKeys(engine)
	specOperations := baselineSpecOperations(t, rt.openapiJSON)

	for _, operation := range baseline.Operations {
		require.NotEmpty(t, specOperations[operation.Method+" "+operation.Path], "baseline endpoint must remain: %s", operation.Path)
	}

	for _, operation := range inventory.Operations {
		key := operation.Method + " " + operation.Path
		require.True(t, actual[key], "missing or duplicate inventory route: %s", key)
		require.NotEmpty(t, operation.OperationID)
		require.Equal(t, operation.OperationID, specOperations[key], "spec drift: %s", key)
		delete(actual, key)
		delete(specOperations, key)

		t.Run(key, func(t *testing.T) {
			assertBaselineAccess(t, engine, operation)
		})
	}

	for _, route := range inventory.RegisteredExceptions {
		key := route.Method + " " + route.Path
		require.True(t, actual[key], "missing or duplicate exception: %s", key)
		delete(actual, key)

		if strings.HasPrefix(route.Access, "session_") {
			req := httptest.NewRequestWithContext(t.Context(), route.Method, route.Path, http.NoBody)
			req.Header.Set("Origin", rt.cfg.Security.AllowedOrigins[0])
			require.Equal(t, http.StatusUnauthorized, doRequest(engine, req).Code, key)
		}
	}

	require.Empty(t, specOperations, "spec operation missing from inventory")
	require.Empty(t, actual, "registered route missing from inventory")
	t.Logf("route inventory: %d operations + %d registered exceptions; access denial checked for every operation", len(inventory.Operations), len(inventory.RegisteredExceptions))
}

func readBaselineInventory(t *testing.T) baselineInventory {
	t.Helper()

	data, err := os.ReadFile("../../../docs/bigbang/endpoint-feature-parity.json")
	require.NoError(t, err)

	var inventory baselineInventory

	require.NoError(t, jsonv2.Unmarshal(data, &inventory))
	require.NotEmpty(t, inventory.Operations)
	require.NotEmpty(t, inventory.RegisteredExceptions)

	return inventory
}

func baselineRouteKeys(engine *gin.Engine) map[string]bool {
	actual := make(map[string]bool)

	for _, route := range engine.Routes() {
		parts := strings.Split(route.Path, "/")
		for i, part := range parts {
			if name, found := strings.CutPrefix(part, ":"); found {
				parts[i] = "{" + name + "}"
			}
		}

		actual[route.Method+" "+strings.Join(parts, "/")] = true
	}

	return actual
}

func baselineSpecOperations(t *testing.T, data []byte) map[string]string {
	t.Helper()

	var spec struct {
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
		} `json:"paths"`
	}

	require.NoError(t, jsonv2.Unmarshal(data, &spec))

	specOps := make(map[string]string)

	for path, methods := range spec.Paths {
		for method, operation := range methods {
			if operation.OperationID != "" {
				specOps[strings.ToUpper(method)+" "+path] = operation.OperationID
			}
		}
	}

	return specOps
}

// ASVS 8.1.1: 목록의 접근 분류를 실제 거부 응답과 연결하되 정상 업무 실행을 주장하지 않는다.
func assertBaselineAccess(t *testing.T, handler http.Handler, operation baselineOperation) {
	t.Helper()

	path := strings.NewReplacer("{id}", "1", "{name}", "hololive-api").Replace(operation.Path)
	req := httptest.NewRequestWithContext(t.Context(), operation.Method, path, http.NoBody)

	switch operation.Access {
	case "public_metadata":
		require.Equal(t, http.StatusOK, doRequest(handler, req).Code)
	case "public_login":
		require.Equal(t, http.StatusBadRequest, doRequest(handler, req).Code)
	case "session", "session_csrf", "session_csrf_audit":
		require.Equal(t, http.StatusUnauthorized, doRequest(handler, req).Code)

		if operation.Access != "session" {
			req = httptest.NewRequestWithContext(t.Context(), operation.Method, path, http.NoBody)
			req.AddCookie(signedSessionCookie("inventory-session"))
			require.Equal(t, http.StatusForbidden, doRequest(handler, req).Code)
		}
	default:
		t.Fatalf("unclassified access: %s", operation.Access)
	}
}

func TestContractRoutingResponses(t *testing.T) {
	rt := newTestRuntime(t, &fakeSessions{}, nil)
	t.Cleanup(rt.Close)

	engine := rt.Handler()

	for _, tc := range []struct {
		method string
		path   string
		status int
	}{
		{http.MethodGet, "/health", http.StatusOK},
		{http.MethodGet, "/admin/api/inventory-missing", http.StatusNotFound},
		{http.MethodPut, "/admin/api/auth/logout", http.StatusMethodNotAllowed},
	} {
		req := httptest.NewRequestWithContext(t.Context(), tc.method, tc.path, http.NoBody)
		rec := doRequest(engine, req)
		require.Equal(t, tc.status, rec.Code)
		require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
		require.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	}
}
