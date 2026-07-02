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
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAlarmHandler_HandlerSignatures(t *testing.T) {
	t.Parallel()

	var _ gin.HandlerFunc = (&AlarmHandler{}).GetAlarms
	var _ gin.HandlerFunc = (&AlarmHandler{}).DeleteAlarm
}

func TestAlarmHandler_GetAlarmsRequiresAlarmDependency(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler *AlarmHandler
	}{
		{
			name:    "nil receiver",
			handler: nil,
		},
		{
			name:    "nil embedded handler",
			handler: &AlarmHandler{},
		},
		{
			name: "nil alarm service",
			handler: &AlarmHandler{Handler: &Handler{
				logger: newDiscardLogger(),
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, rec := newAPITestContext(http.MethodGet, "/api/holo/alarms", nil)
			tt.handler.GetAlarms(ctx)

			assertErrorResponse(t, rec, http.StatusServiceUnavailable, "alarm service not available")
		})
	}
}

func TestAlarmHandler_DeleteAlarmValidationRunsBeforeDependencyCheck(t *testing.T) {
	t.Parallel()

	handler := &AlarmHandler{Handler: &Handler{
		logger: newDiscardLogger(),
	}}

	ctx, rec := newAPITestContext(http.MethodDelete, "/api/holo/alarm", []byte(`{"roomId":"room-1"}`))
	handler.DeleteAlarm(ctx)

	assertErrorResponse(t, rec, http.StatusBadRequest, "invalid request body")
}

func TestAlarmHandler_DeleteAlarmRequiresAlarmDependency(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler *AlarmHandler
	}{
		{
			name:    "nil receiver",
			handler: nil,
		},
		{
			name:    "nil embedded handler",
			handler: &AlarmHandler{},
		},
		{
			name: "nil alarm service",
			handler: &AlarmHandler{Handler: &Handler{
				logger: newDiscardLogger(),
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, rec := newAPITestContext(
				http.MethodDelete,
				"/api/holo/alarm",
				[]byte(`{"roomId":"room-1","channelId":"channel-1"}`),
			)
			tt.handler.DeleteAlarm(ctx)

			assertErrorResponse(t, rec, http.StatusServiceUnavailable, "alarm service not available")
		})
	}
}

func TestAlarmHandler_DeleteAlarmReturnsRemovedFalse(t *testing.T) {
	t.Parallel()

	var gotRoomID string
	var gotChannelID string
	var gotAlarmTypes domain.AlarmTypes

	handler := &AlarmHandler{Handler: &Handler{
		alarm: &stubAlarmCRUDForServer{
			removeAlarm: func(
				_ context.Context,
				roomID string,
				channelID string,
				alarmTypes domain.AlarmTypes,
			) (bool, error) {
				gotRoomID = roomID
				gotChannelID = channelID
				gotAlarmTypes = alarmTypes
				return false, nil
			},
		},
		logger: newDiscardLogger(),
	}}

	ctx, rec := newAPITestContext(
		http.MethodDelete,
		"/api/holo/alarm",
		[]byte(`{"roomId":"room-1","channelId":"channel-1"}`),
	)
	handler.DeleteAlarm(ctx)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "room-1", gotRoomID)
	assert.Equal(t, "channel-1", gotChannelID)
	assert.Nil(t, gotAlarmTypes)
	assert.JSONEq(t, `{"status":"ok","removed":false}`, rec.Body.String())
}
