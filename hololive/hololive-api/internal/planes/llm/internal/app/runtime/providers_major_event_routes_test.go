// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package runtime

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"log/slog"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/majorevent"
	commoncontracts "github.com/kapu/hololive-shared/pkg/contracts/common"
	majoreventcontracts "github.com/kapu/hololive-shared/pkg/contracts/majorevent"
	"github.com/kapu/hololive-shared/pkg/server/middleware"
	json "github.com/park285/shared-go/pkg/json"
)

func TestRegisterMajorEventInternalRoutes_NoOp(t *testing.T) {
	t.Parallel()

	registerMajorEventInternalRoutes(nil, middleware.AuthConfig{Disabled: true}, nil)

	engine, err := buildHealthOnlyRouter(context.Background(), slog.New(slog.DiscardHandler), middleware.AuthConfig{Disabled: true})
	require.NoError(t, err)

	registerMajorEventInternalRoutes(engine, middleware.AuthConfig{Disabled: true}, nil)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, majoreventcontracts.SubscriptionsPath+"/room-1", http.NoBody)
	rr := httptest.NewRecorder()
	engine.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestRegisterMajorEventInternalRoutes_AuthMiddleware(t *testing.T) {
	t.Parallel()

	router := newMajorEventRouter(t, middleware.AuthConfig{APIKey: "secret"}, &majorevent.Repository{})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, majoreventcontracts.SubscriptionsPath+"/room-1", http.NoBody)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)

	req = httptest.NewRequestWithContext(context.Background(), http.MethodGet, majoreventcontracts.SubscriptionsPath+"/room-1", http.NoBody)
	req.Header.Set(commoncontracts.APIKeyHeader, "wrong")
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusForbidden, rr.Code)
}

func TestRegisterMajorEventInternalRoutes_Handlers(t *testing.T) {
	t.Parallel()

	router := newMajorEventRouter(t, middleware.AuthConfig{Disabled: true}, &majorevent.Repository{})

	t.Run("get subscription room_id required", func(t *testing.T) {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, majoreventcontracts.SubscriptionsPath+"/%20", http.NoBody)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
		assertErrorResponse(t, rr, "room_id_required")
	})

	t.Run("get subscription repository error", func(t *testing.T) {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, majoreventcontracts.SubscriptionsPath+"/room-1", http.NoBody)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusInternalServerError, rr.Code)
		assertErrorResponse(t, rr, "subscription_check_failed")
	})

	t.Run("post subscribe invalid body", func(t *testing.T) {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, majoreventcontracts.SubscriptionsPath, bytes.NewBufferString("not-json"))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
		assertErrorResponse(t, rr, "invalid_request")
	})

	t.Run("post subscribe room_id required", func(t *testing.T) {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, majoreventcontracts.SubscriptionsPath, bytes.NewBufferString(`{"room_id":"  ","room_name":"room"}`))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
		assertErrorResponse(t, rr, "room_id_required")
	})

	t.Run("post subscribe repository error", func(t *testing.T) {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, majoreventcontracts.SubscriptionsPath, bytes.NewBufferString(`{"room_id":"room-1","room_name":"room"}`))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusInternalServerError, rr.Code)
		assertErrorResponse(t, rr, "subscribe_failed")
	})

	t.Run("delete unsubscribe room_id required", func(t *testing.T) {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, majoreventcontracts.SubscriptionsPath+"/%20", http.NoBody)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
		assertErrorResponse(t, rr, "room_id_required")
	})

	t.Run("delete unsubscribe repository error", func(t *testing.T) {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, majoreventcontracts.SubscriptionsPath+"/room-1", http.NoBody)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusInternalServerError, rr.Code)
		assertErrorResponse(t, rr, "unsubscribe_failed")
	})
}

func assertErrorResponse(t *testing.T, rr *httptest.ResponseRecorder, want string) {
	t.Helper()

	var payload map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &payload))
	assert.Equal(t, want, payload["error"])
	assert.Len(t, payload, 1)
}

func newMajorEventRouter(t *testing.T, authConfig middleware.AuthConfig, repository *majorevent.Repository) *http.ServeMux {
	t.Helper()

	engine, err := buildHealthOnlyRouter(context.Background(), slog.New(slog.DiscardHandler), middleware.AuthConfig{Disabled: true})
	require.NoError(t, err)

	registerMajorEventInternalRoutes(engine, authConfig, repository)

	mux := http.NewServeMux()
	mux.Handle("/", engine)
	return mux
}
