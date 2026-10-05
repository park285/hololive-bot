package alarmdispatch

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	mekparkUnitBChannel   = "UC3OH5FKQ3qtl4uRme_vZTgA"
	mekparkAcroraChannel  = "UChpRPsAeSZn5DistGacR3iA"
	mekparkUnitBShortName = "유닛 B"
)

// mekparkTestMembers는 호스트 채널의 members 정본 표시명을 돌려준다. 알림은 원천 채널 제목 대신 이 이름에 호스트 표기를 붙인다.
type mekparkTestMembers struct {
	collabTestMembers
}

func (mekparkTestMembers) FindMemberByChannelID(_ context.Context, channelID string) (*domain.Member, error) {
	switch channelID {
	case mekparkUnitBChannel:
		return &domain.Member{ChannelID: channelID, Name: "UNIT B", ShortKoreanName: mekparkUnitBShortName}, nil
	case mekparkAcroraChannel:
		return &domain.Member{ChannelID: channelID, Name: "Acrora", NameKo: "아크로라"}, nil
	default:
		return nil, domain.ErrMemberNotFound
	}
}

func TestMekParkLiveAndUpcomingRendering(t *testing.T) {
	renderer, store := newAlarmDispatchTestRendering(t)

	for _, minutes := range []int{0, 5} {
		notification := &domain.AlarmNotification{
			Channel: &domain.Channel{ID: mekparkUnitBChannel, Name: "UNIT B Ch."},
			Stream: &domain.Stream{
				ID: "video123", ChannelID: mekparkUnitBChannel,
				ChannelName: "UNIT B", Title: "【ピアノ】#玲銘ミラ", Status: domain.StreamStatusUpcoming,
			},
			MinutesUntil: minutes,
		}
		message, err := renderAlarmDispatchNotification(t.Context(), renderer, store, mekparkTestMembers{}, notification)
		require.NoError(t, err)
		require.Contains(t, message, "유닛 B · 미라")

		require.Equal(t, "UNIT B Ch.", notification.Channel.Name)
		require.Equal(t, "UNIT B", notification.Stream.ChannelName)

		notification.Stream.ChannelID = ""
		name, err := resolveAlarmDispatchMemberName(t.Context(), store, mekparkTestMembers{}, notification)
		require.NoError(t, err)
		require.Equal(t, "유닛 B · 미라", name)
	}
}

func TestMekParkSeriesHostRendering(t *testing.T) {
	renderer, store := newAlarmDispatchTestRendering(t)
	notification := &domain.AlarmNotification{
		Channel: &domain.Channel{ID: mekparkAcroraChannel, Name: "Acrora Ch."},
		Stream: &domain.Stream{
			ID: "video123", ChannelID: mekparkAcroraChannel,
			Title: "【#由比河ひなみ】#あさやなストレッチ夏 #墨汐さやな", Status: domain.StreamStatusLive,
		},
	}
	message, err := renderAlarmDispatchNotification(t.Context(), renderer, store, mekparkTestMembers{}, notification)
	require.NoError(t, err)
	require.Contains(t, message, "아크로라 · 사야나 (게스트: 히나미)")
}

type failingDispatchMembers struct {
	collabTestMembers

	err error
}

func (m failingDispatchMembers) FindMemberByChannelID(context.Context, string) (*domain.Member, error) {
	return nil, m.err
}

// members 조회 실패는 원천 채널 제목이나 종단 문구로 덮지 않고 렌더 실패(발송 전 재시도)로 돌려준다.
func TestAlarmDispatchMemberNameLookupFailureFailsRender(t *testing.T) {
	renderer, store := newAlarmDispatchTestRendering(t)
	lookupErr := errors.New("member cache unavailable")
	notification := &domain.AlarmNotification{
		Channel: &domain.Channel{ID: mekparkAcroraChannel, Name: "Acrora Ch."},
		Stream:  &domain.Stream{ID: "video123", ChannelID: mekparkAcroraChannel, Title: "방송", Status: domain.StreamStatusLive},
	}

	_, err := renderAlarmDispatchNotification(t.Context(), renderer, store, failingDispatchMembers{err: lookupErr}, notification)
	require.ErrorIs(t, err, lookupErr)

	_, err = renderAlarmDispatchNotification(t.Context(), renderer, store, nil, notification)
	require.Error(t, err)
}
