package celebration

import (
	"testing"
	"time"

	"github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPgxStoreFindSentRoomsByEventKeysUsesBirthdayDeliveryLedger(t *testing.T) {
	t.Parallel()

	pool := dbtest.NewPool(t)
	repository := dispatchoutbox.NewPgxRepositoryFromPool(pool, nil)
	dateStr := "2026-07-10"
	eventKey := birthdayGreetingEventKey("UC_a", dateStr)
	envelopes := []domain.AlarmQueueEnvelope{
		birthdayGreetingTestEnvelope("UC_a", "room-sent", dateStr),
		birthdayGreetingTestEnvelope("UC_a", "room-retry", dateStr),
		birthdayGreetingTestEnvelope("UC_b", "room-other-member", dateStr),
	}
	_, err := repository.InsertBatch(t.Context(), dispatchoutbox.PublishBatchInput{
		Envelopes: envelopes,
		Status:    dispatchoutbox.StatusPending,
	})
	require.NoError(t, err)

	sentAt := time.Date(2026, 7, 10, 0, 5, 0, 0, time.UTC)
	_, err = pool.Exec(t.Context(), `
		UPDATE alarm_dispatch_deliveries d
		SET status = 'sent', sent_at = $1
		FROM alarm_dispatch_events e
		WHERE d.event_id = e.id
		  AND e.event_key = $2
		  AND d.room_id = 'room-sent'
	`, sentAt, eventKey)
	require.NoError(t, err)

	store := NewPgxStore(pool)
	roomsByEventKey, err := store.FindSentRoomsByEventKeys(t.Context(), []string{
		eventKey,
		birthdayGreetingEventKey("UC_b", dateStr),
		birthdayGreetingEventKey("UC_missing", dateStr),
	})
	require.NoError(t, err)

	assert.Equal(t, []string{"room-sent"}, roomsByEventKey[eventKey])
	assert.NotContains(t, roomsByEventKey[eventKey], "room-retry")
	assert.NotContains(t, roomsByEventKey, birthdayGreetingEventKey("UC_b", dateStr))
	assert.NotContains(t, roomsByEventKey, birthdayGreetingEventKey("UC_missing", dateStr))
}

func birthdayGreetingTestEnvelope(channelID, roomID, dateStr string) domain.AlarmQueueEnvelope {
	return domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeBirthday,
			RoomID:    roomID,
			Channel:   &domain.Channel{ID: channelID, Name: channelID},
		},
		SourceKind: domain.AlarmDispatchSourceKindCelebration,
		Celebration: &domain.CelebrationDispatchPayload{
			Kind:       domain.CelebrationKindBirthday,
			MemberName: channelID,
			ChannelID:  channelID,
			Date:       dateStr,
		},
	}
}
