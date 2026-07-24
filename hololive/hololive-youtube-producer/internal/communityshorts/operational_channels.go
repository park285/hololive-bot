package communityshorts

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
)

type OperationalChannel struct {
	OwnerLabel string
	ChannelID  string
	Enabled    bool
}

type MemberSnapshotLoader interface {
	AllMembers(context.Context) ([]*domain.Member, error)
}

func ResolveOperationalChannels(
	ctx context.Context,
	loader MemberSnapshotLoader,
) ([]OperationalChannel, error) {
	if loader == nil {
		return nil, fmt.Errorf("member snapshot loader is nil")
	}
	value := reflect.ValueOf(loader)
	if value.Kind() == reflect.Pointer && value.IsNil() {
		return nil, fmt.Errorf("member snapshot loader is nil")
	}

	members, err := loader.AllMembers(ctx)
	if err != nil {
		return nil, fmt.Errorf("load members from snapshot: %w", err)
	}
	return BuildOperationalChannelsFromMembers(members), nil
}

func BuildOperationalChannelsFromMembers(members []*domain.Member) []OperationalChannel {
	channels := make([]OperationalChannel, 0, len(members))
	seenChannelIDs := make(map[string]struct{}, len(members))
	for i := range members {
		member := members[i]
		if skipOperationalMember(member) {
			continue
		}
		channelID := strings.TrimSpace(member.ChannelID)
		if !registerOperationalChannelID(channelID, seenChannelIDs) {
			continue
		}
		channels = append(channels, OperationalChannel{
			OwnerLabel: targetOwnerLabel(member),
			ChannelID:  channelID,
			Enabled:    channelID != "",
		})
	}

	return channels
}

func skipOperationalMember(member *domain.Member) bool {
	return member == nil || member.IsGraduated
}

func registerOperationalChannelID(channelID string, seenChannelIDs map[string]struct{}) bool {
	if channelID == "" {
		return true
	}
	if _, exists := seenChannelIDs[channelID]; exists {
		return false
	}
	seenChannelIDs[channelID] = struct{}{}
	return true
}

func EnabledChannelIDs(channels []OperationalChannel) []string {
	channelIDs := make([]string, 0, len(channels))
	for i := range channels {
		if !channels[i].Enabled {
			continue
		}
		channelIDs = append(channelIDs, channels[i].ChannelID)
	}
	return channelIDs
}

func ValidateOperationalTargets(channels []OperationalChannel) error {
	definitions := make([]sharedalarmkeys.ChannelContentAlarmTargetDefinition, 0, len(channels))
	for i := range channels {
		definitions = append(definitions, sharedalarmkeys.ChannelContentAlarmTargetDefinition{
			OwnerLabel: channels[i].OwnerLabel,
			ChannelID:  channels[i].ChannelID,
		})
	}
	if err := sharedalarmkeys.ValidateChannelContentAlarmTargetDefinitions(definitions); err != nil {
		return fmt.Errorf("validate community shorts operational targets: %w", err)
	}
	return nil
}

func targetOwnerLabel(member *domain.Member) string {
	if member == nil {
		return ""
	}
	if strings.TrimSpace(member.Name) != "" {
		return member.GetDisplayName()
	}
	return strings.TrimSpace(member.ChannelID)
}
