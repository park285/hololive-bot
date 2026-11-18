package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestAlarmHandler_DeleteAlarmRejectsMissingChannelID(t *testing.T) {
	t.Parallel()

	handler := &AlarmHandler{
		logger: newDiscardLogger(),
	}

	ctx, rec := newAPITestContext(http.MethodDelete, "/api/holo/alarm", []byte(`{"roomId":"room-1"}`))
	handler.DeleteAlarm(ctx)

	assertErrorResponse(t, rec, http.StatusBadRequest, "invalid request body")
}

func TestAlarmHandler_DeleteAlarmReturnsRemovedFalse(t *testing.T) {
	t.Parallel()

	var (
		gotRoomID     string
		gotChannelID  string
		gotAlarmTypes domain.AlarmTypes
	)

	handler := &AlarmHandler{
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
	}

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
