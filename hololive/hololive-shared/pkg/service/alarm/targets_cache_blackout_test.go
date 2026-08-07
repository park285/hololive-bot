package alarm

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

func cacheErrorCount(operation string) float64 {
	return testutil.ToFloat64(alarmSubscriberCacheErrorTotal.WithLabelValues(operation))
}

func TestSubscriberLookupCacheErrorIsCounted(t *testing.T) {
	db := newAlarmTargetLookupTestDB(t)
	requireAlarmRecord(t, db, &domain.Alarm{
		RoomID:     "room-db",
		ChannelID:  "UC_blackout_lookup",
		AlarmTypes: domain.AlarmTypes{domain.AlarmTypeShorts},
	})

	cacheClient := cachemocks.NewStrictClient()
	cacheClient.SMembersFunc = func(context.Context, string) ([]string, error) {
		return nil, errors.New("valkey unavailable")
	}
	cacheClient.SAddFunc = func(_ context.Context, _ string, members []string) (int64, error) {
		return int64(len(members)), nil
	}
	cacheClient.ExpireFunc = func(context.Context, string, time.Duration) error { return nil }
	cacheClient.DelFunc = func(context.Context, string) error { return nil }

	before := cacheErrorCount("lookup")

	got, err := ResolveChannelSubscribersByType(t.Context(), cacheClient, db, "UC_blackout_lookup", domain.AlarmTypeShorts)

	require.NoError(t, err, "a cache blackout must still resolve through the database")
	require.Equal(t, []string{"room-db"}, got)
	require.Greater(t, cacheErrorCount("lookup"), before,
		"a swallowed subscriber-cache read failure must still raise the cache error counter, or a full Valkey blackout is invisible")
}

func TestSubscriberEmptyMarkerCheckErrorIsCounted(t *testing.T) {
	db := newAlarmTargetLookupTestDB(t)
	requireAlarmRecord(t, db, &domain.Alarm{
		RoomID:     "room-db",
		ChannelID:  "UC_blackout_empty",
		AlarmTypes: domain.AlarmTypes{domain.AlarmTypeShorts},
	})

	cacheClient := cachemocks.NewStrictClient()
	cacheClient.SMembersFunc = func(context.Context, string) ([]string, error) { return nil, nil }
	cacheClient.ExistsFunc = func(context.Context, string) (bool, error) {
		return false, errors.New("valkey unavailable")
	}
	cacheClient.SAddFunc = func(_ context.Context, _ string, members []string) (int64, error) {
		return int64(len(members)), nil
	}
	cacheClient.ExpireFunc = func(context.Context, string, time.Duration) error { return nil }
	cacheClient.DelFunc = func(context.Context, string) error { return nil }

	before := cacheErrorCount("check_empty")

	got, err := ResolveChannelSubscribersByType(t.Context(), cacheClient, db, "UC_blackout_empty", domain.AlarmTypeShorts)

	require.NoError(t, err)
	require.Equal(t, []string{"room-db"}, got)
	require.Greater(t, cacheErrorCount("check_empty"), before,
		"a swallowed empty-marker check failure must still raise the cache error counter")
}
