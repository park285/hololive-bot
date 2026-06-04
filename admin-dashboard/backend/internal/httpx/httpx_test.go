package httpx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/park285/shared-go/pkg/json"
	"github.com/stretchr/testify/require"
)

func TestErrorMapsAppError(t *testing.T) {
	rec := httptest.NewRecorder()
	Error(rec, BadRequest("nope"))

	require.Equal(t, http.StatusBadRequest, rec.Code)
	var body ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "nope", body.Error)
	require.Equal(t, "bad_request", body.Code)
}

func TestErrorMapsUnknownErrorTo500(t *testing.T) {
	rec := httptest.NewRecorder()
	Error(rec, errors.New("boom"))

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Contains(t, rec.Body.String(), "An internal error occurred")
}

func TestAbortMapsAppErrorAndStopsChain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	Abort(c, Forbidden())

	require.True(t, c.IsAborted())
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "Forbidden")
}

func TestAppErrorUnwrap(t *testing.T) {
	cause := errors.New("root cause")
	err := Internal(cause)

	require.ErrorIs(t, err, cause)
	require.Equal(t, "root cause", err.Error())
}

func TestDecodeJSONRejectsUnknownFields(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"known":1,"unknown":2}`))
	var dst struct {
		Known int `json:"known"`
	}
	require.Error(t, DecodeJSON(req, &dst, 1024))
}

func TestDecodeJSONHonorsLimit(t *testing.T) {
	payload := `{"value":"` + strings.Repeat("x", 100) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(payload))
	var dst struct {
		Value string `json:"value"`
	}
	require.Error(t, DecodeJSON(req, &dst, 10))

	req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"value":"ok"}`))
	require.NoError(t, DecodeJSON(req, &dst, 1024))
	require.Equal(t, "ok", dst.Value)
}
