package communityshorts

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type fakeMemberSnapshotLoader struct {
	allMembers func(context.Context) ([]*domain.Member, error)
}

func (f fakeMemberSnapshotLoader) AllMembers(ctx context.Context) ([]*domain.Member, error) {
	return f.allMembers(ctx)
}

func TestResolveOperationalChannels_ReturnsErrorOnSnapshotFailure(t *testing.T) {
	t.Parallel()

	_, err := ResolveOperationalChannels(context.Background(), fakeMemberSnapshotLoader{
		allMembers: func(context.Context) ([]*domain.Member, error) {
			return nil, assert.AnError
		},
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "load members from snapshot")
}

func TestResolveOperationalChannels_SkipsGraduatedAndDedupesChannelIDs(t *testing.T) {
	t.Parallel()

	channels, err := ResolveOperationalChannels(context.Background(), fakeMemberSnapshotLoader{
		allMembers: func(context.Context) ([]*domain.Member, error) {
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
	assert.Equal(t, OperationalChannel{OwnerLabel: "Pekora (Hololive)", ChannelID: "UCdup", Enabled: true}, channels[0])
	assert.Equal(t, OperationalChannel{OwnerLabel: "Noel (Hololive)", ChannelID: "", Enabled: false}, channels[1])
}

func TestValidateOperationalTargets(t *testing.T) {
	t.Parallel()

	t.Run("accepts distinct active channel targets", func(t *testing.T) {
		t.Parallel()
		err := ValidateOperationalTargets(BuildOperationalChannelsFromMembers([]*domain.Member{
			{Name: "Pekora", Org: "Hololive", ChannelID: "UCpekora"},
			{Name: "Miko", Org: "Hololive", ChannelID: "UCmiko"},
		}))
		require.NoError(t, err)
	})

	t.Run("rejects active member without channel id", func(t *testing.T) {
		t.Parallel()
		err := ValidateOperationalTargets(BuildOperationalChannelsFromMembers([]*domain.Member{
			{Name: "Pekora", Org: "Hololive", ChannelID: ""},
		}))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing operating channel targets")
		assert.Contains(t, err.Error(), "Pekora (Hololive)")
	})

	t.Run("enabled channel ids follow resolver enablement", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"UCenabled"}, EnabledChannelIDs([]OperationalChannel{
			{OwnerLabel: "Enabled", ChannelID: "UCenabled", Enabled: true},
			{OwnerLabel: "Disabled", ChannelID: "UCshadow", Enabled: false},
		}))
	})
}
