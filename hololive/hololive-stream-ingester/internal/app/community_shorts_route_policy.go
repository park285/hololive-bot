package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
)

type communityShortsRouteRequest struct {
	AlarmType   domain.AlarmType
	ChannelID   string
	PublishedAt time.Time
}

type communityShortsBigBangPolicy struct {
	cutoverAt        time.Time
	targetChannelIDs map[string]struct{}
}

func buildCommunityShortsBigBangPolicy(ingestionCfg config.IngestionConfig, channels []communityShortsOperationalChannel) (communityShortsBigBangPolicy, error) {
	if !ingestionCfg.CommunityShortsBigBangEnabled {
		return communityShortsBigBangPolicy{}, nil
	}

	if err := validateCommunityShortsOperationalTargets(channels); err != nil {
		return communityShortsBigBangPolicy{}, fmt.Errorf("build community shorts big-bang policy: %w", err)
	}

	targetChannelIDs := make(map[string]struct{}, len(channels))
	for i := range channels {
		if !channels[i].enabled {
			continue
		}
		targetChannelIDs[channels[i].channelID] = struct{}{}
	}

	return communityShortsBigBangPolicy{
		cutoverAt:        ingestionCfg.CommunityShortsBigBangCutoverAt.UTC(),
		targetChannelIDs: targetChannelIDs,
	}, nil
}

func buildCommunityShortsRouteDecider(policy communityShortsBigBangPolicy) poller.NotificationRouteDecider {
	if !policy.Enabled() {
		return nil
	}

	return func(req poller.NotificationRouteRequest) bool {
		return policy.ShouldUseNewPath(communityShortsRouteRequest{
			AlarmType:   req.AlarmType,
			ChannelID:   req.ChannelID,
			PublishedAt: req.PublishedAt,
		})
	}
}

func (p communityShortsBigBangPolicy) Enabled() bool {
	return !p.cutoverAt.IsZero() && len(p.targetChannelIDs) > 0
}

func (p communityShortsBigBangPolicy) CutoverAt() time.Time {
	return p.cutoverAt
}

func (p communityShortsBigBangPolicy) TargetChannelCount() int {
	return len(p.targetChannelIDs)
}

func (p communityShortsBigBangPolicy) ShouldUseNewPath(req communityShortsRouteRequest) bool {
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
