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
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/httputil"
	sharedlogging "github.com/park285/shared-go/v2/pkg/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	membernewssvc "github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews"
	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/model"
	"github.com/kapu/hololive-shared/pkg/contracts/common"
	membernewscontracts "github.com/kapu/hololive-shared/pkg/contracts/membernews"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
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

	module, err := BuildDeliveryModule(nil, postgres, logger)
	require.NoError(t, err)
	require.NotNil(t, module)
	require.NotNil(t, module.Repository)

	locker := module.Locker
	require.NotNil(t, locker)

	token, acquired, err := locker.TryAcquire(t.Context(), "test-lock", time.Second)
	require.NoError(t, err)
	assert.True(t, acquired)
	assert.Empty(t, token)

	triggerHandler := sharedserver.NewTriggerHandler(nil, nil, nil, logger)
	require.NotNil(t, triggerHandler)
}

func TestBuildDeliveryModuleRejectsInvalidHandoffMode(t *testing.T) {
	t.Setenv(deliveryOutboxV3HandoffModeEnv, "dual-write")

	module, err := BuildDeliveryModule(nil, &fakePostgresClient{}, sharedlogging.NewTestLogger())

	require.Error(t, err)
	assert.Nil(t, module)
	assert.Contains(t, err.Error(), "unsupported mode")
}

func TestConvertMemberNewsDigest(t *testing.T) {
	t.Parallel()

	assert.Nil(t, convertMemberNewsDigest(nil))

	digest := &model.Digest{
		Period:      model.PeriodMonthly,
		Headline:    "이번달 뉴스",
		MoreSummary: "외 2건",
		TopItems: []model.SummaryItem{
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
	registerMemberNewsInternalRoutes(nil, httputil.AdminAuthConfig{Disabled: true}, nil)

	service := membernewssvc.NewService(nil, nil, nil, nil, sharedlogging.NewTestLogger())

	t.Run("auth middleware", func(t *testing.T) {
		router := newMemberNewsRouter(t, httputil.AdminAuthConfig{APIKey: "secret"}, service)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, membernewscontracts.DigestPath, bytes.NewBufferString(`{"room_id":"r1"}`))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusUnauthorized, rr.Code)

		req = httptest.NewRequestWithContext(t.Context(), http.MethodPost, membernewscontracts.DigestPath, bytes.NewBufferString(`{"room_id":"r1"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(common.APIKeyHeader, "wrong")

		rr = httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusForbidden, rr.Code)
	})

	t.Run("subscription and digest handlers", func(t *testing.T) {
		router := newMemberNewsRouter(t, httputil.AdminAuthConfig{Disabled: true}, service)

		assertMemberNewsRoute(t, router, http.MethodGet, membernewscontracts.SubscriptionsPath+"/%20", "", http.StatusBadRequest, "room_id_required")
		assertMemberNewsRoute(t, router, http.MethodGet, membernewscontracts.SubscriptionsPath+"/room-1", "", http.StatusInternalServerError, "subscription_check_failed")
		assertMemberNewsRoute(t, router, http.MethodPost, membernewscontracts.SubscriptionsPath, "not-json", http.StatusBadRequest, "invalid_request")
		assertMemberNewsRoute(t, router, http.MethodPost, membernewscontracts.SubscriptionsPath, `{"room_id":"  ","room_name":"room"}`, http.StatusBadRequest, "room_id_required")
		assertMemberNewsRoute(t, router, http.MethodPost, membernewscontracts.SubscriptionsPath, `{"room_id":"room-1","room_name":"room"}`, http.StatusInternalServerError, "subscribe_failed")
		assertMemberNewsRoute(t, router, http.MethodDelete, membernewscontracts.SubscriptionsPath+"/%20", "", http.StatusBadRequest, "room_id_required")
		assertMemberNewsRoute(t, router, http.MethodDelete, membernewscontracts.SubscriptionsPath+"/room-1", "", http.StatusInternalServerError, "unsubscribe_failed")
		assertMemberNewsRoute(t, router, http.MethodPost, membernewscontracts.DigestPath, "not-json", http.StatusBadRequest, "invalid_request")
		assertMemberNewsRoute(t, router, http.MethodPost, membernewscontracts.DigestPath, `{"room_id":" ","period":"weekly"}`, http.StatusBadRequest, "room_id_required")
		assertMemberNewsRoute(t, router, http.MethodPost, membernewscontracts.DigestPath, `{"room_id":"room-1","period":"weekly"}`, http.StatusInternalServerError, "digest_generation_failed")
	})
}

func assertMemberNewsRoute(
	t *testing.T,
	router http.Handler,
	method, path, body string,
	wantStatus int,
	wantError string,
) {
	t.Helper()

	var reqBody io.Reader = http.NoBody

	if body != "" {
		reqBody = bytes.NewBufferString(body)
	}

	req := httptest.NewRequestWithContext(t.Context(), method, path, reqBody)

	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	assert.Equal(t, wantStatus, rr.Code)
	assertErrorResponse(t, rr, wantError)
}

func newMemberNewsRouter(t *testing.T, authConfig httputil.AdminAuthConfig, service *membernewssvc.Service) *http.ServeMux {
	t.Helper()

	// gin.Engine는 http.Handler를 구현하므로 테스트 편의를 위해 mux에 연결합니다.
	engine, err := buildHealthOnlyRouter(t.Context(), sharedlogging.NewTestLogger(), httputil.AdminAuthConfig{Disabled: true})
	require.NoError(t, err)
	registerMemberNewsInternalRoutes(engine, authConfig, service)

	mux := http.NewServeMux()
	mux.Handle("/", engine)

	return mux
}

var (
	_ database.Client        = (*fakePostgresClient)(nil)
	_ delivery.MessageSender = (*fakeSender)(nil)
)
