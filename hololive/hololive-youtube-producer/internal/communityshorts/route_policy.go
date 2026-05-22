package communityshorts

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
)

type RouteRequest struct {
	AlarmType   domain.AlarmType
	ChannelID   string
	PublishedAt time.Time
}

type Policy struct {
	cutoverAt        time.Time
	targetChannelIDs map[string]struct{}
}

func BuildPolicy(ingestionConfig config.IngestionConfig, channels []OperationalChannel) (Policy, error) {
	if !ingestionConfig.CommunityShortsBigBangEnabled {
		return Policy{}, nil
	}

	if err := ValidateOperationalTargets(channels); err != nil {
		return Policy{}, fmt.Errorf("build community shorts big-bang policy: %w", err)
	}

	targetChannelIDs := make(map[string]struct{}, len(channels))
	for i := range channels {
		if !channels[i].Enabled {
			continue
		}
		targetChannelIDs[channels[i].ChannelID] = struct{}{}
	}

	return Policy{
		cutoverAt:        ingestionConfig.CommunityShortsBigBangCutoverAt.UTC(),
		targetChannelIDs: targetChannelIDs,
	}, nil
}

func BuildRouteDecider(policy Policy) poller.NotificationRouteDecider {
	if !policy.Enabled() {
		return nil
	}

	return func(req poller.NotificationRouteRequest) bool {
		return policy.ShouldUseNewPath(RouteRequest{
			AlarmType:   req.AlarmType,
			ChannelID:   req.ChannelID,
			PublishedAt: req.PublishedAt,
		})
	}
}

func (p Policy) Enabled() bool {
	return !p.cutoverAt.IsZero() && len(p.targetChannelIDs) > 0
}

func (p Policy) CutoverAt() time.Time {
	return p.cutoverAt
}

func (p Policy) TargetChannelCount() int {
	return len(p.targetChannelIDs)
}

func (p Policy) TargetChannelIDs() []string {
	channelIDs := make([]string, 0, len(p.targetChannelIDs))
	for channelID := range p.targetChannelIDs {
		channelIDs = append(channelIDs, channelID)
	}
	slices.Sort(channelIDs)
	return channelIDs
}

func (p Policy) ShouldUseNewPath(req RouteRequest) bool {
	if !p.Enabled() || req.PublishedAt.IsZero() {
		return false
	}

	switch req.AlarmType {
	case domain.AlarmTypeCommunity, domain.AlarmTypeShorts:
	default:
		return false
	}

	channelID := strings.TrimSpace(req.ChannelID)
	if _, ok := p.targetChannelIDs[channelID]; !ok {
		return false
	}

	return !req.PublishedAt.UTC().Before(p.cutoverAt)
}
