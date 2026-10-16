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
	"fmt"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	triggercontracts "github.com/kapu/hololive-shared/pkg/contracts/trigger"
)

type majorEventContextKey struct{}

type weeklyMajorEventSchedulerFunc func(context.Context) error

func (f weeklyMajorEventSchedulerFunc) SendWeeklyNotification(ctx context.Context) (triggercontracts.Result, error) {
	if err := f(ctx); err != nil {
		return triggercontracts.Result{}, fmt.Errorf("f: %w", err)
	}

	return triggercontracts.NewResult(1, 0, 0), nil
}

type monthlyMajorEventSchedulerFunc func(context.Context) error

func (f monthlyMajorEventSchedulerFunc) SendMonthlyNotification(ctx context.Context) (triggercontracts.Result, error) {
	if err := f(ctx); err != nil {
		return triggercontracts.Result{}, fmt.Errorf("f: %w", err)
	}

	return triggercontracts.NewResult(1, 0, 0), nil
}

func TestMajorEventHandler_HandlerSignatures(t *testing.T) {
	t.Parallel()

	var (
		_ gin.HandlerFunc = (&MajorEventHandler{}).TriggerMajorEventNotification
		_ gin.HandlerFunc = (&MajorEventHandler{}).TriggerMajorEventMonthlyNotification
	)
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
			handler: (&Handler{
				logger: newDiscardLogger(),
			}).DomainHandlers().MajorEvent,
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

			handler := (&Handler{
				logger: newDiscardLogger(),
				majorEventScheduler: weeklyMajorEventSchedulerFunc(func(context.Context) error {
					return tt.err
				}),
			}).DomainHandlers().MajorEvent
			ctx, rec := newAPITestContext(http.MethodPost, "/api/holo/trigger/major-event", nil)
			handler.TriggerMajorEventNotification(ctx)

			assertErrorResponse(t, rec, tt.wantStatus, tt.wantMessage)
		})
	}
}

func TestMajorEventHandler_WeeklyNotificationUsesRequestContext(t *testing.T) {
	t.Parallel()

	gotValue := make(chan any, 1)
	handler := (&Handler{
		logger: newDiscardLogger(),
		majorEventScheduler: weeklyMajorEventSchedulerFunc(func(ctx context.Context) error {
			gotValue <- ctx.Value(majorEventContextKey{})
			return nil
		}),
	}).DomainHandlers().MajorEvent
	ctx, rec := newAPITestContext(http.MethodPost, "/api/holo/trigger/major-event", nil)

	ctx.Request = ctx.Request.WithContext(context.WithValue(ctx.Request.Context(), majorEventContextKey{}, "weekly"))

	handler.TriggerMajorEventNotification(ctx)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "weekly", <-gotValue)
	assert.JSONEq(t, `{"status":"queued","queued":1,"skipped":0,"failed":0}`, rec.Body.String())
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
			handler: (&Handler{
				logger: newDiscardLogger(),
			}).DomainHandlers().MajorEvent,
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

			handler := (&Handler{
				logger: newDiscardLogger(),
				majorEventMonthlyScheduler: monthlyMajorEventSchedulerFunc(func(context.Context) error {
					return tt.err
				}),
			}).DomainHandlers().MajorEvent
			ctx, rec := newAPITestContext(http.MethodPost, "/api/holo/trigger/major-event-monthly", nil)
			handler.TriggerMajorEventMonthlyNotification(ctx)

			assertErrorResponse(t, rec, tt.wantStatus, tt.wantMessage)
		})
	}
}

func TestMajorEventHandler_MonthlyNotificationUsesRequestContext(t *testing.T) {
	t.Parallel()

	gotValue := make(chan any, 1)
	handler := (&Handler{
		logger: newDiscardLogger(),
		majorEventMonthlyScheduler: monthlyMajorEventSchedulerFunc(func(ctx context.Context) error {
			gotValue <- ctx.Value(majorEventContextKey{})
			return nil
		}),
	}).DomainHandlers().MajorEvent
	ctx, rec := newAPITestContext(http.MethodPost, "/api/holo/trigger/major-event-monthly", nil)

	ctx.Request = ctx.Request.WithContext(context.WithValue(ctx.Request.Context(), majorEventContextKey{}, "monthly"))

	handler.TriggerMajorEventMonthlyNotification(ctx)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "monthly", <-gotValue)
	assert.JSONEq(t, `{"status":"queued","queued":1,"skipped":0,"failed":0}`, rec.Body.String())
}

type enqueueResultScheduler struct {
	result triggercontracts.Result
}

func (s enqueueResultScheduler) SendWeeklyNotification(context.Context) (triggercontracts.Result, error) {
	return s.result, nil
}

func (s enqueueResultScheduler) SendMonthlyNotification(context.Context) (triggercontracts.Result, error) {
	return s.result, nil
}

func TestMajorEventHandlerPreservesPartialAndSkippedOutcomes(t *testing.T) {
	for _, tc := range []struct {
		result triggercontracts.Result
		wire   string
	}{
		{triggercontracts.NewResult(2, 0, 1), `{"status":"partial","queued":2,"skipped":0,"failed":1}`},
		{triggercontracts.NewResult(0, 0, 0), `{"status":"skipped","queued":0,"skipped":0,"failed":0}`},
	} {
		scheduler := enqueueResultScheduler{result: tc.result}
		handler := (&Handler{
			logger:                     newDiscardLogger(),
			majorEventScheduler:        scheduler,
			majorEventMonthlyScheduler: scheduler,
		}).DomainHandlers().MajorEvent

		for _, trigger := range []gin.HandlerFunc{
			handler.TriggerMajorEventNotification,
			handler.TriggerMajorEventMonthlyNotification,
		} {
			ctx, response := newAPITestContext(http.MethodPost, "/api/holo/trigger/major-event", nil)
			trigger(ctx)
			require.Equal(t, http.StatusOK, response.Code)
			assert.JSONEq(t, tc.wire, response.Body.String())
		}
	}
}
