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

// 알람 목록은 이름 캐시에 없는 채널을 members 정본으로 채운다. 조회 실패는 채널 ID 등 다른 이름으로 덮지 않는다.
func TestListRoomAlarmsViewResolvesCacheMissFromMembers(t *testing.T) {
	lookupErr := errors.New("member database unavailable")

	for _, tc := range []struct {
		name     string
		provider domain.MemberDataProvider
		wantName string
		wantErr  error
	}{
		{
			name:     "member display name",
			provider: &mockMemberDataProvider{members: []*domain.Member{{ChannelID: testChannelID, Name: "Usada Pekora", NameKo: "우사다 페코라", ShortKoreanName: "페코라"}}},
			wantName: "페코라",
		},
		{name: "unknown channel keeps channel id", provider: &mockMemberDataProvider{}, wantName: testChannelID},
		{name: "lookup failure", provider: failedCacheNameProvider{err: lookupErr}, wantErr: lookupErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			as := newTestAlarmService(t)
			// members에 없는 채널로 구독해 이름 캐시를 빈 값으로 둔 뒤, 목록 조회 때의 members 상태를 바꾼다.
			as.memberData = &mockMemberDataProvider{}

			added, err := as.AddAlarm(t.Context(), &domain.AddAlarmRequest{RoomID: testRoomID, ChannelID: testChannelID})
			require.NoError(t, err)
			require.True(t, added)

			as.memberData = tc.provider

			views, listErr := as.ListRoomAlarmsView(t.Context(), testRoomID)
			require.ErrorIs(t, listErr, tc.wantErr)

			if tc.wantErr != nil {
				return
			}

			require.Len(t, views, 1)
			require.Equal(t, tc.wantName, views[0].MemberName)
		})
	}
}
