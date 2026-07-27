package alarmcache

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/alarm/dedup"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/stretchr/testify/require"
)

func TestStateNotifiedUsesCanonicalMinuteMarkerOnly(t *testing.T) {
	stored := make(map[string]any)
	client := cachemocks.NewStrictClient()
	client.SetFunc = func(_ context.Context, key string, value any, _ time.Duration) error {
		stored[key] = value
		return nil
	}
	client.GetFunc = func(_ context.Context, key string, destination any) error {
		value, ok := stored[key]
		if !ok {
			return errors.New("missing")
		}
		marker, ok := destination.(*string)
		if !ok {
			return errors.New("unexpected destination")
		}
		*marker, ok = value.(string)
		if !ok {
			return errors.New("unexpected value")
		}
		return nil
	}

	state := NewState(client, nil, nil)
	start := time.Date(2026, 7, 27, 10, 15, 42, 0, time.UTC)
	require.NoError(t, state.MarkAsNotified(t.Context(), "stream-1", start, 5))
	require.Equal(t, map[string]any{NotifiedMinuteKey("stream-1", start, 5): "1"}, stored)
	require.True(t, state.WasNotified(t.Context(), "stream-1", start, 5))
}

func TestStateNotifiedIgnoresOldAggregateOnly(t *testing.T) {
	start := time.Date(2026, 7, 27, 10, 15, 0, 0, time.UTC)
	oldPayload, err := json.Marshal(dedup.NotifiedData{
		StartScheduled: start.Format(time.RFC3339),
		SentAt:         map[int]bool{5: true},
	})
	require.NoError(t, err)
	require.NotEmpty(t, oldPayload)

	client := cachemocks.NewStrictClient()
	client.GetFunc = func(_ context.Context, key string, _ any) error {
		if key == sharedalarmkeys.NotifiedKeyPrefix+"stream-1" {
			t.Fatal("canonical reader accessed old aggregate payload")
		}
		return errors.New("missing")
	}

	state := NewState(client, nil, nil)
	require.False(t, state.WasNotified(t.Context(), "stream-1", start, 5))
}
