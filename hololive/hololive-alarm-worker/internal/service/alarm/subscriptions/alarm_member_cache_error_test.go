package subscriptions

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/subscriptions/internal/alarmcache"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
)

type failedCacheNameProvider struct {
	domain.MemberDataProvider

	err error
}

func (p failedCacheNameProvider) FindMemberByChannelID(context.Context, string) (*domain.Member, error) {
	return nil, p.err
}

func TestCacheAlarmDistinguishesMissingMemberFromLookupFailure(t *testing.T) {
	lookupErr := errors.New("member database unavailable")
	for _, tc := range []struct {
		name        string
		err         error
		wantName    string
		wantFailure bool
	}{
		{name: "lookup failure preserves cached name", err: lookupErr, wantName: "기존 이름", wantFailure: true},
		{name: "missing member clears stale name", err: domain.ErrMemberNotFound, wantName: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			client := sharedtestutil.NewTestCacheService(ctx, t)
			require.NoError(t, client.HSet(ctx, sharedalarmkeys.MemberNameKey, testChannelID, "기존 이름"))

			provider := failedCacheNameProvider{err: tc.err}
			service := &AlarmService{
				cacheState: alarmcache.NewState(client, func() domain.MemberDataProvider { return provider }, newDiscardAlarmLogger()),
			}

			err := service.cacheAlarm(ctx, &domain.Alarm{ChannelID: testChannelID})

			if tc.wantFailure {
				require.ErrorIs(t, err, lookupErr)
			} else {
				require.NoError(t, err)
			}

			name, err := client.HGet(ctx, sharedalarmkeys.MemberNameKey, testChannelID)
			require.NoError(t, err)
			require.Equal(t, tc.wantName, name)
		})
	}
}
