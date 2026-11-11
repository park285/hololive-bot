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

// 알람 목록의 멤버 이름은 members 정본의 DisplayName(short_korean_name→korean_name→english_name)이다.
// 멤버 정본에 없는 채널은 채널 ID로 보이고, 조회 실패는 채널 ID 등 다른 이름으로 덮지 않는다.
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
			// DisplayName은 공백을 다듬지 않으므로 공백뿐인 short_korean_name이 korean_name을 가리고 목록은 채널 ID를 보여 준다.
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

// 관리 목록의 멤버 이름도 members 정본의 DisplayName이며 members에 없는 채널은 빈 이름이다. 여러 방이 구독한 채널도
// 한 번만 조회하고, 조회 실패는 이름 없는 목록으로 바꾸지 않고 목록 오류로 돌려준다.
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
