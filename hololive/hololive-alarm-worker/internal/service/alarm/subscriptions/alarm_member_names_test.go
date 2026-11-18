package subscriptions

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const testEnglishMemberName = "Usada Pekora"

type failedMemberLookupProvider struct {
	domain.MemberDataProvider

	err error
}

func (p failedMemberLookupProvider) FindMemberByChannelID(context.Context, string) (*domain.Member, error) {
	return nil, p.err
}

type countingMemberLookupProvider struct {
	*mockMemberDataProvider

	lookups int
}

func (p *countingMemberLookupProvider) FindMemberByChannelID(ctx context.Context, channelID string) (*domain.Member, error) {
	p.lookups++

	return p.mockMemberDataProvider.FindMemberByChannelID(ctx, channelID)
}

func TestListRoomAlarmsViewResolvesMemberNamesFromMembers(t *testing.T) {
	lookupErr := errors.New("member database unavailable")

	for _, tc := range []struct {
		name     string
		provider domain.MemberDataProvider
		wantName string
		wantErr  error
	}{
		{
			name:     "member display name",
			provider: &mockMemberDataProvider{members: []*domain.Member{{ChannelID: testChannelID, Name: testEnglishMemberName, NameKo: "우사다 페코라", ShortKoreanName: "페코라"}}},
			wantName: "페코라",
		},
		{
			name:     "english name only",
			provider: &mockMemberDataProvider{members: []*domain.Member{{ChannelID: testChannelID, Name: testEnglishMemberName}}},
			wantName: testEnglishMemberName,
		},
		{
			name:     "blank short korean name",
			provider: &mockMemberDataProvider{members: []*domain.Member{{ChannelID: testChannelID, Name: testEnglishMemberName, NameKo: "우사다 페코라", ShortKoreanName: " "}}},
			wantName: testChannelID,
		},
		{name: "unknown channel keeps channel id", provider: &mockMemberDataProvider{}, wantName: testChannelID},
		{name: "lookup failure", provider: failedMemberLookupProvider{err: lookupErr}, wantErr: lookupErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			as := newTestAlarmService(t)

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

func TestGetAllAlarmKeysResolvesMemberNamesFromMembers(t *testing.T) {
	ctx := t.Context()
	as := newTestAlarmService(t)

	for _, req := range []*domain.AddAlarmRequest{
		{RoomID: testRoomID, ChannelID: "UC_short"},
		{RoomID: "room-2", ChannelID: "UC_short"},
		{RoomID: testRoomID, ChannelID: "UC_english"},
		{RoomID: testRoomID, ChannelID: "UC_unknown"},
	} {
		added, err := as.AddAlarm(ctx, req)
		require.NoError(t, err)
		require.True(t, added)
	}

	provider := &countingMemberLookupProvider{mockMemberDataProvider: &mockMemberDataProvider{members: []*domain.Member{
		{ChannelID: "UC_short", Name: "Juufuutei Raden", NameKo: "주후테이 라덴", ShortKoreanName: "라덴"},
		{ChannelID: "UC_english", Name: testEnglishMemberName},
	}}}

	as.memberData = provider

	entries, err := as.GetAllAlarmKeys(ctx)
	require.NoError(t, err)
	require.Len(t, entries, 4)
	require.Equal(t, 3, provider.lookups)

	for _, entry := range entries {
		want := map[string]string{"UC_short": "라덴", "UC_english": testEnglishMemberName, "UC_unknown": ""}[entry.ChannelID]
		require.Equal(t, want, entry.MemberName, entry.ChannelID)
	}

	lookupErr := errors.New("member database unavailable")

	as.memberData = failedMemberLookupProvider{err: lookupErr}

	entries, err = as.GetAllAlarmKeys(ctx)
	require.ErrorIs(t, err, lookupErr)
	require.Nil(t, entries)
}
