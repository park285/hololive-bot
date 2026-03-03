package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	triggercontracts "github.com/kapu/hololive-shared/pkg/contracts/trigger"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
)

type stubMajorEventScheduler struct {
	err error
}

func (s *stubMajorEventScheduler) SendWeeklyNotification(_ context.Context) error {
	return s.err
}

type stubMajorEventMonthlyScheduler struct {
	err error
}

func (s *stubMajorEventMonthlyScheduler) SendMonthlyNotification(_ context.Context) error {
	return s.err
}

type stubMemberNewsWeeklyScheduler struct {
	err error
}

func (s *stubMemberNewsWeeklyScheduler) SendWeeklyDigest(_ context.Context) error {
	return s.err
}

func TestTriggerHandler_MemberNewsWeekly_NotInitialized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	handler := sharedserver.NewTriggerHandler(
		&stubMajorEventScheduler{},
		&stubMajorEventMonthlyScheduler{},
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	handler.RegisterInternalRoutes(router.Group(""))

	req := httptest.NewRequest(http.MethodPost, triggercontracts.MemberNewsWeeklyPath, http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestTriggerHandler_MemberNewsWeekly_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	handler := sharedserver.NewTriggerHandler(
		&stubMajorEventScheduler{},
		&stubMajorEventMonthlyScheduler{},
		&stubMemberNewsWeeklyScheduler{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	handler.RegisterInternalRoutes(router.Group(""))

	req := httptest.NewRequest(http.MethodPost, triggercontracts.MemberNewsWeeklyPath, http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
