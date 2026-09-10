package httpx

import (
	jsonv2 "encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/kapu/admin-dashboard/internal/contract"
)

func TestErrorMapsAppError(t *testing.T) {
	rec := httptest.NewRecorder()
	Error(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody), contract.BadRequest("nope"))

	require.Equal(t, http.StatusBadRequest, rec.Code)

	var body contract.ErrorResponse

	require.NoError(t, jsonv2.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "nope", body.Error)
	require.Equal(t, "BAD_REQUEST", body.Code)
}

func TestMutationRejectionEvidenceComesOnlyFromRequestContext(t *testing.T) {
	const id = "5ae58f70-51c4-4df2-bf70-683773e40638"

	state := &contract.Dispatch{}
	ctx := contract.WithDispatch(t.Context(), state)
	input := contract.ErrorResponse{Code: "BAD_REQUEST", Error: "Refused", NotDispatchedMutationID: id}
	require.Empty(t, errorBody(ctx, 400, input, "fixture").NotDispatchedMutationID)

	contract.MarkMutationClaimed(ctx, id)
	require.Equal(t, id, errorBody(ctx, 400, input, "fixture").NotDispatchedMutationID)
	contract.MarkDispatched(ctx)
	require.Empty(t, errorBody(ctx, 400, input, "fixture").NotDispatchedMutationID)
}

func TestErrorMapsUnknownErrorTo500(t *testing.T) {
	rec := httptest.NewRecorder()
	Error(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody), errors.New("boom"))

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Contains(t, rec.Body.String(), "An internal error occurred")
}

func TestAbortMapsAppErrorAndStopsChain(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	c.Request = httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)

	Abort(c, contract.Forbidden())

	require.True(t, c.IsAborted())
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "Forbidden")
}

func TestAppErrorUnwrap(t *testing.T) {
	cause := errors.New("root cause")
	err := contract.Internal(cause)

	require.ErrorIs(t, err, cause)
	require.Equal(t, "root cause", err.Error())
}

func TestDecodeJSONRejectsUnknownFields(t *testing.T) {
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(`{"known":1,"unknown":2}`))

	var dst struct {
		Known int `json:"known"`
	}

	require.Error(t, DecodeJSON(req, &dst, 1024))
}

func TestDecodeJSONHonorsLimit(t *testing.T) {
	payload := `{"value":"` + strings.Repeat("x", 100) + `"}`
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(payload))

	var dst struct {
		Value string `json:"value"`
	}

	require.Error(t, DecodeJSON(req, &dst, 10))

	req = httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(`{"value":"ok"}`))
	require.NoError(t, DecodeJSON(req, &dst, 1024))
	require.Equal(t, "ok", dst.Value)
}
