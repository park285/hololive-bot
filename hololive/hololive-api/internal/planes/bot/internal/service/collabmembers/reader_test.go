package collabmembers

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	dbmocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
)

const testHostChannel = "UC_miko"

type membersFake []*domain.Member

func (m membersFake) LoadAllMembers(context.Context) ([]*domain.Member, error) { return m, nil }

func (m membersFake) FindMemberByChannelID(_ context.Context, channelID string) (*domain.Member, error) {
	for _, member := range m {
		if member.ChannelID == channelID {
			return member, nil
		}
	}

	return nil, domain.ErrMemberNotFound
}

type countingMembers struct {
	membersFake

	loads int
}

func (m *countingMembers) LoadAllMembers(ctx context.Context) ([]*domain.Member, error) {
	m.loads++

	return m.membersFake.LoadAllMembers(ctx)
}

func (membersFake) GetChannelIDs(context.Context) ([]string, error) { return nil, nil }

func (membersFake) FindMemberByName(context.Context, string) (*domain.Member, error) {
	return nil, domain.ErrMemberNotFound
}

func (membersFake) FindMemberByAlias(context.Context, string) (*domain.Member, error) {
	return nil, domain.ErrMemberNotFound
}

// 공식 스케줄의 최신 행만 읽고, 다른 출처와 출연자 없는 행은 무시하며, 호스트는 빼고 표시명으로 바꾼다.
func TestReaderDisplayNamesReadsLatestOfficialScheduleRow(t *testing.T) {
	t.Parallel()

	pool := dbtest.NewPool(t)

	_, err := pool.Exec(t.Context(), `
INSERT INTO youtube_schedule_items(group_key, provider, external_id, video_id, channel_id, title, scheduled_at, collabo_talent_names, updated_at) VALUES
 ('g', 'hololive_official', 'old', 'collab00001', 'UC_miko', '방송', now(), ARRAY['Gawr Gura'], now() - interval '1 hour'),
 ('g', 'hololive_official', 'new', 'collab00001', 'UC_miko', '방송', now(), ARRAY['さくらみこ', '星街すいせい', 'Gawr Gura'], now()),
 ('g', 'holodex', 'other', 'collab00002', 'UC_miko', '방송', now(), ARRAY['星街すいせい'], now()),
 ('g', 'hololive_official', 'second', 'collab00003', 'UC_miko', '방송', now(), ARRAY['星街すいせい'], now()),
 ('g', 'hololive_official', 'solo', 'solo0000001', 'UC_miko', '방송', now(), '{}', now())`)
	require.NoError(t, err)

	members := &countingMembers{membersFake: membersFake{
		{ChannelID: testHostChannel, NameJa: "さくらみこ", ShortKoreanName: "미코"},
		{ChannelID: "UC_sui", NameJa: "星街すいせい", ShortKoreanName: "스이세이"},
	}}
	reader := New(&dbmocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}, members)

	got, err := reader.DisplayNames(t.Context(), []Video{
		{ID: "collab00001", HostChannelID: testHostChannel},
		{ID: "collab00002", HostChannelID: testHostChannel},
		{ID: "collab00003", HostChannelID: testHostChannel},
		{ID: "solo0000001", HostChannelID: testHostChannel},
		{ID: " ", HostChannelID: testHostChannel},
	})
	require.NoError(t, err)
	require.Equal(t, map[string][]string{"collab00001": {"스이세이", "Gawr Gura"}, "collab00003": {"스이세이"}}, got)
	require.Equal(t, 1, members.loads, "member index must be built once per request")

	empty, err := New(nil, nil).DisplayNames(t.Context(), nil)
	require.NoError(t, err)
	require.Empty(t, empty)

	pool.Close()

	_, err = reader.DisplayNames(t.Context(), []Video{{ID: "collab00001"}})
	require.Error(t, err)
}
