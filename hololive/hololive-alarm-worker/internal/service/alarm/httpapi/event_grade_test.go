package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/eventgrade"
	contract "github.com/kapu/hololive-shared/pkg/contracts/alarm"
)

func TestEventGradeRequiresInternalKeyAndStrictBody(t *testing.T) {
	router := gin.New()
	require.NoError(t, NewInternalRouteRegistrar("test-key", fakeAlarmCRUD{}, eventgrade.NewRepository(nil), slog.New(slog.DiscardHandler))(router))

	for _, tt := range []struct {
		key, body string
		want      int
	}{
		{"", `{}`, http.StatusUnauthorized},
		{"test-key", `{"region":"global","score":1,"room_id":"1","user_id":"2"}`, http.StatusBadRequest},
		{"test-key", `{"region":"jp","score":1,"room_id":"1","user_id":"2","chapter":"injected"}`, http.StatusBadRequest},
		{"test-key", strings.Repeat(" ", 1025) + `{}`, http.StatusBadRequest},
	} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, contract.EventGradePath, strings.NewReader(tt.body))
		req.Header.Set("X-API-Key", tt.key)
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, tt.want, rec.Code)
	}
}

type fakeEventGradeRegistrar struct {
	resp contract.EventGradeResponse
	err  error
}

func (f fakeEventGradeRegistrar) RegisterEventGrade(context.Context, contract.EventGradeRequest) (contract.EventGradeResponse, error) {
	return f.resp, f.err
}

func TestEventGradeRouteSeparatesUnavailableFromDatabaseFailure(t *testing.T) {
	for _, tt := range []struct {
		registrar fakeEventGradeRegistrar
		want      int
		body      string
	}{
		{fakeEventGradeRegistrar{resp: contract.EventGradeResponse{Region: "jp", Status: "unavailable"}}, http.StatusOK, `{"success":true,"data":{"region":"jp","status":"unavailable"}}`},
		{fakeEventGradeRegistrar{err: errors.New("database unavailable")}, http.StatusServiceUnavailable, ""},
	} {
		router := gin.New()
		require.NoError(t, NewInternalRouteRegistrar("test-key", fakeAlarmCRUD{}, tt.registrar, slog.New(slog.DiscardHandler))(router))

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, contract.EventGradePath, strings.NewReader(`{"region":"jp","score":1,"room_id":"1","user_id":"2"}`))
		req.Header.Set("X-API-Key", "test-key")
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, tt.want, rec.Code)

		if tt.body != "" {
			require.JSONEq(t, tt.body, rec.Body.String())
		}
	}
}
