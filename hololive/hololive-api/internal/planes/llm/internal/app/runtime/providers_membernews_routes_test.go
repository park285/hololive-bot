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
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	membernewssvc "github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews"
	membernewscontracts "github.com/kapu/hololive-shared/pkg/contracts/membernews"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/server/middleware"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	sharedlogging "github.com/park285/shared-go/pkg/logging"
)

type fakePostgresClient struct{}

func (f *fakePostgresClient) GetPool() *pgxpool.Pool { return nil }
func (f *fakePostgresClient) Ping(context.Context) error {
	return nil
}
func (f *fakePostgresClient) Close() error { return nil }

type fakeSender struct{}

func (fakeSender) SendMessage(context.Context, string, string) error { return nil }

func TestBuildDeliveryModuleAndTriggerProviders(t *testing.T) {
	t.Parallel()

	var postgres database.Client = &fakePostgresClient{}
	logger := sharedlogging.NewTestLogger()

	module := BuildDeliveryModule(nil, postgres, logger)
	require.NotNil(t, module)
	require.NotNil(t, module.Repository)
	locker := module.Locker
	require.NotNil(t, locker)
	token, acquired, err := locker.TryAcquire(context.Background(), "test-lock", time.Second)
	require.NoError(t, err)
	assert.True(t, acquired)
	assert.Empty(t, token)

	triggerHandler := sharedserver.NewTriggerHandler(nil, nil, nil, logger)
	require.NotNil(t, triggerHandler)
}

func TestConvertMemberNewsDigest(t *testing.T) {
	t.Parallel()

	assert.Nil(t, convertMemberNewsDigest(nil))

	digest := &membernewssvc.Digest{
		Period:      membernewssvc.PeriodMonthly,
		Headline:    "이번달 뉴스",
		MoreSummary: "외 2건",
		TopItems: []membernewssvc.SummaryItem{
			{
				Member:    "사쿠라 미코",
				Category:  "event",
				Title:     "행사",
				DateText:  "2026-03-10",
				Summary:   "요약",
				SourceURL: "https://example.com/news/1",
			},
		},
		OmittedCount: 2,
		TotalCount:   3,
	}

	converted := convertMemberNewsDigest(digest)
	require.NotNil(t, converted)
	assert.Equal(t, membernewscontracts.PeriodMonthly, converted.Period)
	assert.Equal(t, digest.Headline, converted.Headline)
	require.Len(t, converted.TopItems, 1)
	assert.Equal(t, digest.TopItems[0].Member, converted.TopItems[0].Member)
	assert.Equal(t, digest.TopItems[0].Category, converted.TopItems[0].Category)
	assert.Equal(t, digest.TopItems[0].SourceURL, converted.TopItems[0].SourceURL)
	assert.Equal(t, digest.MoreSummary, converted.MoreSummary)
	assert.Equal(t, digest.OmittedCount, converted.OmittedCount)
	assert.Equal(t, digest.TotalCount, converted.TotalCount)
}

func TestRegisterMemberNewsInternalRoutes(t *testing.T) {
	t.Parallel()

	registerMemberNewsInternalRoutes(nil, middleware.AuthConfig{Disabled: true}, nil)

	service := membernewssvc.NewService(nil, nil, nil, nil, sharedlogging.NewTestLogger())

	t.Run("auth middleware", func(t *testing.T) {
		router := newMemberNewsRouter(t, middleware.AuthConfig{APIKey: "secret"}, service)

		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, membernewscontracts.DigestPath, bytes.NewBufferString(`{"room_id":"r1"}`))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusUnauthorized, rr.Code)

		req = httptest.NewRequestWithContext(context.Background(), http.MethodPost, membernewscontracts.DigestPath, bytes.NewBufferString(`{"room_id":"r1"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(middleware.APIKeyHeader, "wrong")
		rr = httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusForbidden, rr.Code)
	})

	t.Run("subscription and digest handlers", func(t *testing.T) {
		router := newMemberNewsRouter(t, middleware.AuthConfig{Disabled: true}, service)

		// GET subscription - room_id required
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, membernewscontracts.SubscriptionsPath+"/%20", http.NoBody)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
		assertErrorResponse(t, rr, "room_id_required")

		// GET subscription - service error
		req = httptest.NewRequestWithContext(context.Background(), http.MethodGet, membernewscontracts.SubscriptionsPath+"/room-1", http.NoBody)
		rr = httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusInternalServerError, rr.Code)
		assertErrorResponse(t, rr, "subscription_check_failed")

		// POST subscribe - invalid body
		req = httptest.NewRequestWithContext(context.Background(), http.MethodPost, membernewscontracts.SubscriptionsPath, bytes.NewBufferString("not-json"))
		req.Header.Set("Content-Type", "application/json")
		rr = httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
		assertErrorResponse(t, rr, "invalid_request")

		// POST subscribe - room_id required
		req = httptest.NewRequestWithContext(context.Background(), http.MethodPost, membernewscontracts.SubscriptionsPath, bytes.NewBufferString(`{"room_id":"  ","room_name":"room"}`))
		req.Header.Set("Content-Type", "application/json")
		rr = httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
		assertErrorResponse(t, rr, "room_id_required")

		// POST subscribe - service error
		req = httptest.NewRequestWithContext(context.Background(), http.MethodPost, membernewscontracts.SubscriptionsPath, bytes.NewBufferString(`{"room_id":"room-1","room_name":"room"}`))
		req.Header.Set("Content-Type", "application/json")
		rr = httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusInternalServerError, rr.Code)
		assertErrorResponse(t, rr, "subscribe_failed")

		// DELETE subscribe - room_id required
		req = httptest.NewRequestWithContext(context.Background(), http.MethodDelete, membernewscontracts.SubscriptionsPath+"/%20", http.NoBody)
		rr = httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
		assertErrorResponse(t, rr, "room_id_required")

		// DELETE subscribe - service error
		req = httptest.NewRequestWithContext(context.Background(), http.MethodDelete, membernewscontracts.SubscriptionsPath+"/room-1", http.NoBody)
		rr = httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusInternalServerError, rr.Code)
		assertErrorResponse(t, rr, "unsubscribe_failed")

		// POST digest - invalid body
		req = httptest.NewRequestWithContext(context.Background(), http.MethodPost, membernewscontracts.DigestPath, bytes.NewBufferString("not-json"))
		req.Header.Set("Content-Type", "application/json")
		rr = httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
		assertErrorResponse(t, rr, "invalid_request")

		// POST digest - room_id required
		req = httptest.NewRequestWithContext(context.Background(), http.MethodPost, membernewscontracts.DigestPath, bytes.NewBufferString(`{"room_id":" ","period":"weekly"}`))
		req.Header.Set("Content-Type", "application/json")
		rr = httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
		assertErrorResponse(t, rr, "room_id_required")

		// POST digest - service error
		req = httptest.NewRequestWithContext(context.Background(), http.MethodPost, membernewscontracts.DigestPath, bytes.NewBufferString(`{"room_id":"room-1","period":"weekly"}`))
		req.Header.Set("Content-Type", "application/json")
		rr = httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusInternalServerError, rr.Code)
		assertErrorResponse(t, rr, "digest_generation_failed")
	})
}

func newMemberNewsRouter(t *testing.T, authConfig middleware.AuthConfig, service *membernewssvc.Service) *http.ServeMux {
	t.Helper()

	// gin.Engine는 http.Handler를 구현하므로 테스트 편의를 위해 mux에 연결합니다.
	engine, err := buildHealthOnlyRouter(context.Background(), sharedlogging.NewTestLogger(), middleware.AuthConfig{Disabled: true})
	require.NoError(t, err)
	registerMemberNewsInternalRoutes(engine, authConfig, service)

	mux := http.NewServeMux()
	mux.Handle("/", engine)
	return mux
}

var _ database.Client = (*fakePostgresClient)(nil)
var _ delivery.MessageSender = (*fakeSender)(nil)
