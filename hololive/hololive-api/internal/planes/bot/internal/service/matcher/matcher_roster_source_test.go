package matcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const rosterTestMemberName = "Hoshimachi Suisei"

// DEC-20260926-hololive-source-fallbacks-retirement: 부분 일치 결과의 채널명은 roster 정본에서 나온다. Holodex 조회나
// Valkey 알림 멤버명 캐시(HGet)로 채우는 폴백 체인을 거치지 않는다. HGetFunc를 두지 않아 호출되면 mock이 실패한다.
func TestFindBestMatchUsesRosterNameWithoutCachedNameFallback(t *testing.T) {
	t.Parallel()

	provider := newStubMemberProvider([]*domain.Member{{ChannelID: "ch-sui", Name: rosterTestMemberName, Org: orgHololive}})
	matcher := NewMatcher(provider, nil, newMatcherTestLogger())

	channel, found, err := matcher.FindBestMatch(t.Context(), "hoshimachi")
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, channel)
	assert.Equal(t, "ch-sui", channel.ID)
	assert.Equal(t, rosterTestMemberName, channel.Name)
	require.NotNil(t, channel.Org)
	assert.Equal(t, orgHololive, *channel.Org)
}
