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
