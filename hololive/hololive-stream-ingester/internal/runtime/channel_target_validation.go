package runtime

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
)

type communityShortsOperationalChannel struct {
	ownerLabel string
	channelID  string
	enabled    bool
}

type communityShortsMemberRepository interface {
	GetAllMembers(context.Context) ([]*domain.Member, error)
}

func resolveCommunityShortsOperationalChannels(membersData domain.MemberDataProvider) ([]communityShortsOperationalChannel, error) {
	if membersData == nil {
		return nil, fmt.Errorf("members data provider is nil")
	}

	return resolveCommunityShortsOperationalChannelsFromMembers(membersData.GetAllMembers()), nil
}

func resolveCommunityShortsOperationalChannelsFromRepository(
	ctx context.Context,
	repo communityShortsMemberRepository,
) ([]communityShortsOperationalChannel, error) {
	if repo == nil {
		return nil, fmt.Errorf("member repository is nil")
	}
	value := reflect.ValueOf(repo)
	if value.Kind() == reflect.Ptr && value.IsNil() {
		return nil, fmt.Errorf("member repository is nil")
	}

	members, err := repo.GetAllMembers(ctx)
	if err != nil {
		return nil, fmt.Errorf("load members from repository: %w", err)
	}
	return resolveCommunityShortsOperationalChannelsFromMembers(members), nil
}

func resolveCommunityShortsOperationalChannelsFromMembers(members []*domain.Member) []communityShortsOperationalChannel {
	channels := make([]communityShortsOperationalChannel, 0, len(members))
	seenChannelIDs := make(map[string]struct{}, len(members))
	for i := range members {
		member := members[i]
		if member == nil || member.IsGraduated {
			continue
		}
		channelID := strings.TrimSpace(member.ChannelID)
		if channelID != "" {
			if _, exists := seenChannelIDs[channelID]; exists {
				continue
			}
			seenChannelIDs[channelID] = struct{}{}
		}
		channels = append(channels, communityShortsOperationalChannel{
			ownerLabel: communityShortsTargetOwnerLabel(member),
			channelID:  channelID,
			enabled:    channelID != "",
		})
	}

	return channels
}

func buildCommunityShortsOperationalTargetDefinitions(channels []communityShortsOperationalChannel) []sharedalarmkeys.ChannelContentAlarmTargetDefinition {
	definitions := make([]sharedalarmkeys.ChannelContentAlarmTargetDefinition, 0, len(channels))
	for i := range channels {
		definitions = append(definitions, sharedalarmkeys.ChannelContentAlarmTargetDefinition{
			OwnerLabel: channels[i].ownerLabel,
			ChannelID:  channels[i].channelID,
		})
	}
	return definitions
}

func communityShortsEnabledChannelIDs(channels []communityShortsOperationalChannel) []string {
	channelIDs := make([]string, 0, len(channels))
	for i := range channels {
		if !channels[i].enabled {
			continue
		}
		channelIDs = append(channelIDs, channels[i].channelID)
	}
	return channelIDs
}

func validateCommunityShortsOperationalTargets(channels []communityShortsOperationalChannel) error {
	definitions := buildCommunityShortsOperationalTargetDefinitions(channels)
	if err := sharedalarmkeys.ValidateChannelContentAlarmTargetDefinitions(definitions); err != nil {
		return fmt.Errorf("validate community shorts operational targets: %w", err)
	}

	return nil
}

func communityShortsTargetOwnerLabel(member *domain.Member) string {
	if member == nil {
		return ""
	}
	if strings.TrimSpace(member.Name) != "" {
		return member.GetDisplayName()
	}
	return strings.TrimSpace(member.ChannelID)
}
