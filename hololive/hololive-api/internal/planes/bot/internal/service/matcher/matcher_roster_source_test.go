package matcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const rosterTestMemberName = "Hoshimachi Suisei"

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
