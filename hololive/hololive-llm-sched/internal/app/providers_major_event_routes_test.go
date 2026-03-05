package app

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-llm-sched/internal/service/majorevent"
	commoncontracts "github.com/kapu/hololive-shared/pkg/contracts/common"
	majoreventcontracts "github.com/kapu/hololive-shared/pkg/contracts/majorevent"
)

func TestRegisterMajorEventInternalRoutes_NoOp(t *testing.T) {
	t.Parallel()

	registerMajorEventInternalRoutes(nil, "", nil)

	engine, err := ProvideHealthOnlyRouter(context.Background(), newTestLogger())
	require.NoError(t, err)

	registerMajorEventInternalRoutes(engine, "", nil)

	req := httptest.NewRequest(http.MethodGet, majoreventcontracts.SubscriptionsPath+"/room-1", nil)
	rr := httptest.NewRecorder()
	engine.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestRegisterMajorEventInternalRoutes_AuthMiddleware(t *testing.T) {
	t.Parallel()

	router := newMajorEventRouter(t, "secret", &majorevent.Repository{})

	req := httptest.NewRequest(http.MethodGet, majoreventcontracts.SubscriptionsPath+"/room-1", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)

	req = httptest.NewRequest(http.MethodGet, majoreventcontracts.SubscriptionsPath+"/room-1", nil)
	req.Header.Set(commoncontracts.APIKeyHeader, "wrong")
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusForbidden, rr.Code)
}

func TestRegisterMajorEventInternalRoutes_Handlers(t *testing.T) {
	t.Parallel()

	router := newMajorEventRouter(t, "", &majorevent.Repository{})

	t.Run("get subscription room_id required", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, majoreventcontracts.SubscriptionsPath+"/%20", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})

	t.Run("get subscription repository error", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, majoreventcontracts.SubscriptionsPath+"/room-1", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusInternalServerError, rr.Code)
	})

	t.Run("post subscribe invalid body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, majoreventcontracts.SubscriptionsPath, bytes.NewBufferString("not-json"))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})

	t.Run("post subscribe room_id required", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, majoreventcontracts.SubscriptionsPath, bytes.NewBufferString(`{"room_id":"  ","room_name":"room"}`))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})

	t.Run("post subscribe repository error", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, majoreventcontracts.SubscriptionsPath, bytes.NewBufferString(`{"room_id":"room-1","room_name":"room"}`))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusInternalServerError, rr.Code)
	})

	t.Run("delete unsubscribe room_id required", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, majoreventcontracts.SubscriptionsPath+"/%20", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})

	t.Run("delete unsubscribe repository error", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, majoreventcontracts.SubscriptionsPath+"/room-1", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusInternalServerError, rr.Code)
	})
}

func newMajorEventRouter(t *testing.T, apiKey string, repo *majorevent.Repository) *http.ServeMux {
	t.Helper()

	engine, err := ProvideHealthOnlyRouter(context.Background(), newTestLogger())
	require.NoError(t, err)

	registerMajorEventInternalRoutes(engine, apiKey, repo)

	mux := http.NewServeMux()
	mux.Handle("/", engine)
	return mux
}

