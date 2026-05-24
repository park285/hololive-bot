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

package api

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	triggercontracts "github.com/kapu/hololive-shared/pkg/contracts/trigger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type majorEventContextKey struct{}

type weeklyMajorEventSchedulerFunc func(context.Context) error

func (f weeklyMajorEventSchedulerFunc) SendWeeklyNotification(ctx context.Context) error {
	return f(ctx)
}

type monthlyMajorEventSchedulerFunc func(context.Context) error

func (f monthlyMajorEventSchedulerFunc) SendMonthlyNotification(ctx context.Context) error {
	return f(ctx)
}

func TestMajorEventHandler_HandlerSignatures(t *testing.T) {
	t.Parallel()

	var _ gin.HandlerFunc = (&MajorEventHandler{}).TriggerMajorEventNotification
	var _ gin.HandlerFunc = (&MajorEventHandler{}).TriggerMajorEventMonthlyNotification
}

func TestMajorEventHandler_WeeklyNotificationDependencyErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler *MajorEventHandler
	}{
		{
			name:    "nil receiver",
			handler: nil,
		},
		{
			name:    "nil embedded handler",
			handler: &MajorEventHandler{},
		},
		{
			name: "nil scheduler",
			handler: &MajorEventHandler{Handler: &Handler{
				logger: newDiscardLogger(),
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, rec := newAPITestContext(http.MethodPost, "/api/holo/trigger/major-event", nil)
			tt.handler.TriggerMajorEventNotification(ctx)

			assertErrorResponse(t, rec, http.StatusServiceUnavailable, "major event scheduler not initialized")
		})
	}
}

func TestMajorEventHandler_WeeklyNotificationErrorResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		err         error
		wantStatus  int
		wantMessage string
	}{
		{
			name:        "conflict",
			err:         triggercontracts.ErrNotificationInProgress,
			wantStatus:  http.StatusConflict,
			wantMessage: "notification already in progress",
		},
		{
			name:        "internal error",
			err:         errors.New("weekly failed"),
			wantStatus:  http.StatusInternalServerError,
			wantMessage: "failed to send notification",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := &MajorEventHandler{Handler: &Handler{
				logger: newDiscardLogger(),
				majorEventScheduler: weeklyMajorEventSchedulerFunc(func(context.Context) error {
					return tt.err
				}),
			}}
			ctx, rec := newAPITestContext(http.MethodPost, "/api/holo/trigger/major-event", nil)
			handler.TriggerMajorEventNotification(ctx)

			assertErrorResponse(t, rec, tt.wantStatus, tt.wantMessage)
		})
	}
}

func TestMajorEventHandler_WeeklyNotificationUsesRequestContext(t *testing.T) {
	t.Parallel()

	gotValue := make(chan any, 1)
	handler := &MajorEventHandler{Handler: &Handler{
		logger: newDiscardLogger(),
		majorEventScheduler: weeklyMajorEventSchedulerFunc(func(ctx context.Context) error {
			gotValue <- ctx.Value(majorEventContextKey{})
			return nil
		}),
	}}
	ctx, rec := newAPITestContext(http.MethodPost, "/api/holo/trigger/major-event", nil)
	ctx.Request = ctx.Request.WithContext(context.WithValue(ctx.Request.Context(), majorEventContextKey{}, "weekly"))

	handler.TriggerMajorEventNotification(ctx)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "weekly", <-gotValue)
	assert.JSONEq(t, `{"status":"weekly notification sent"}`, rec.Body.String())
}

func TestMajorEventHandler_MonthlyNotificationDependencyErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler *MajorEventHandler
	}{
		{
			name:    "nil receiver",
			handler: nil,
		},
		{
			name:    "nil embedded handler",
			handler: &MajorEventHandler{},
		},
		{
			name: "nil scheduler",
			handler: &MajorEventHandler{Handler: &Handler{
				logger: newDiscardLogger(),
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, rec := newAPITestContext(http.MethodPost, "/api/holo/trigger/major-event-monthly", nil)
			tt.handler.TriggerMajorEventMonthlyNotification(ctx)

			assertErrorResponse(t, rec, http.StatusServiceUnavailable, "major event monthly scheduler not initialized")
		})
	}
}

func TestMajorEventHandler_MonthlyNotificationErrorResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		err         error
		wantStatus  int
		wantMessage string
	}{
		{
			name:        "conflict",
			err:         triggercontracts.ErrNotificationInProgress,
			wantStatus:  http.StatusConflict,
			wantMessage: "notification already in progress",
		},
		{
			name:        "internal error",
			err:         errors.New("monthly failed"),
			wantStatus:  http.StatusInternalServerError,
			wantMessage: "failed to send notification",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := &MajorEventHandler{Handler: &Handler{
				logger: newDiscardLogger(),
				majorEventMonthlyScheduler: monthlyMajorEventSchedulerFunc(func(context.Context) error {
					return tt.err
				}),
			}}
			ctx, rec := newAPITestContext(http.MethodPost, "/api/holo/trigger/major-event-monthly", nil)
			handler.TriggerMajorEventMonthlyNotification(ctx)

			assertErrorResponse(t, rec, tt.wantStatus, tt.wantMessage)
		})
	}
}

func TestMajorEventHandler_MonthlyNotificationUsesRequestContext(t *testing.T) {
	t.Parallel()

	gotValue := make(chan any, 1)
	handler := &MajorEventHandler{Handler: &Handler{
		logger: newDiscardLogger(),
		majorEventMonthlyScheduler: monthlyMajorEventSchedulerFunc(func(ctx context.Context) error {
			gotValue <- ctx.Value(majorEventContextKey{})
			return nil
		}),
	}}
	ctx, rec := newAPITestContext(http.MethodPost, "/api/holo/trigger/major-event-monthly", nil)
	ctx.Request = ctx.Request.WithContext(context.WithValue(ctx.Request.Context(), majorEventContextKey{}, "monthly"))

	handler.TriggerMajorEventMonthlyNotification(ctx)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "monthly", <-gotValue)
	assert.JSONEq(t, `{"status":"monthly notification sent"}`, rec.Body.String())
}
