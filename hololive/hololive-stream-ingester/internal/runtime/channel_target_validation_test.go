package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type fakeOperationalMemberRepository struct {
	getAllMembers func(context.Context) ([]*domain.Member, error)
}

func (f fakeOperationalMemberRepository) GetAllMembers(ctx context.Context) ([]*domain.Member, error) {
	return f.getAllMembers(ctx)
}

func TestResolveCommunityShortsOperationalChannelsFromRepository_ReturnsErrorOnRepositoryFailure(t *testing.T) {
	t.Parallel()

	_, err := resolveCommunityShortsOperationalChannelsFromRepository(context.Background(), fakeOperationalMemberRepository{
		getAllMembers: func(context.Context) ([]*domain.Member, error) {
			return nil, assert.AnError
		},
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "load members from repository")
}

func TestResolveCommunityShortsOperationalChannelsFromRepository_SkipsGraduatedAndDedupesChannelIDs(t *testing.T) {
	t.Parallel()

	channels, err := resolveCommunityShortsOperationalChannelsFromRepository(context.Background(), fakeOperationalMemberRepository{
		getAllMembers: func(context.Context) ([]*domain.Member, error) {
			return []*domain.Member{
				{Name: "Pekora", Org: "Hololive", ChannelID: " UCdup "},
				{Name: "Miko", Org: "Hololive", ChannelID: "UCdup"},
				{Name: "Noel", Org: "Hololive", ChannelID: "  "},
				{Name: "Graduated", Org: "Hololive", ChannelID: "UCgraduated", IsGraduated: true},
				nil,
			}, nil
		},
	})
	require.NoError(t, err)
	require.Len(t, channels, 2)
	assert.Equal(t, communityShortsOperationalChannel{
		ownerLabel: "Pekora (Hololive)",
		channelID:  "UCdup",
		enabled:    true,
	}, channels[0])
	assert.Equal(t, communityShortsOperationalChannel{
		ownerLabel: "Noel (Hololive)",
		channelID:  "",
		enabled:    false,
	}, channels[1])
}
