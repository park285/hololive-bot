// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package youtube

import (
	"context"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	ytstats "github.com/kapu/hololive-shared/pkg/service/youtube/stats"
)

func (ys *schedulerImpl) watchNearMilestoneMembers(ctx context.Context) {
	nearMembers, err := ys.statsRepo.GetNearMilestoneMembers(ctx, MilestoneThresholdRatio, SubscriberMilestones, 50)
	if err != nil {
		ys.logger.Error("Failed to get near milestone members", slog.Any("error", err))
		return
	}

	if len(nearMembers) == 0 {
		return
	}

	_, channelToMember := ys.buildChannelMaps()
	channelMap := ys.getNearMilestoneChannelMap(ctx, nearMembers, channelToMember)

	ys.logger.Info("Checking near-milestone members via Holodex",
		slog.Int("count", len(nearMembers)))

	now := time.Now()
	for _, nm := range nearMembers {
		member := channelToMember[nm.ChannelID]
		if member == nil {
			continue
		}

		channel := channelMap[nm.ChannelID]
		if channel == nil {
			channel, err = ys.holodex.GetChannel(ctx, nm.ChannelID)
			if err != nil {
				ys.logger.Warn("Failed to get channel from Holodex",
					slog.String("channel", nm.ChannelID),
					slog.Any("error", err))
				continue
			}
		}
		if channel == nil || channel.SubscriberCount == nil {
			continue
		}

		currentSubs := uint64(*channel.SubscriberCount)
		prevSubs := nm.CurrentSubs

		milestones := ys.checkMilestones(prevSubs, currentSubs)
		if len(milestones) > 0 {
			achieved, _, _ := ys.processMilestones(ctx, nm.ChannelID, member, milestones, nil, false, now)
			if achieved > 0 {
				ys.logger.Info("Milestone detected via Holodex watcher",
					slog.String("member", member.Name),
					slog.Any("milestones", milestones),
					slog.Any("current_subs", currentSubs))

				stats := &domain.TimestampedStats{
					ChannelID:       nm.ChannelID,
					MemberName:      member.Name,
					SubscriberCount: currentSubs,
					Timestamp:       now,
				}
				if err := ys.statsRepo.SaveStats(ctx, stats); err != nil {
					ys.logger.Warn("Failed to save Holodex stats",
						slog.String("channel", nm.ChannelID),
						slog.Any("error", err))
				}
			}
			continue
		}

		ys.checkApproachingAlert(ctx, nm, member, currentSubs, now)
	}
}

func (ys *schedulerImpl) getNearMilestoneChannelMap(
	ctx context.Context,
	nearMembers []ytstats.NearMilestoneEntry,
	channelToMember map[string]*domain.Member,
) map[string]*domain.Channel {
	channelIDs := make([]string, 0, len(nearMembers))
	for _, nm := range nearMembers {
		if channelToMember[nm.ChannelID] == nil {
			continue
		}
		channelIDs = append(channelIDs, nm.ChannelID)
	}

	if len(channelIDs) == 0 {
		return make(map[string]*domain.Channel)
	}

	channelMap, err := ys.holodex.GetChannels(ctx, channelIDs)
	return finalizeNearMilestoneChannelMap(ys.logger, len(channelIDs), channelMap, err)
}

func finalizeNearMilestoneChannelMap(
	logger *slog.Logger,
	requested int,
	channelMap map[string]*domain.Channel,
	err error,
) map[string]*domain.Channel {
	if channelMap == nil {
		channelMap = make(map[string]*domain.Channel)
	}

	if err != nil {
		logger.Warn("Failed to batch fetch near-milestone channels; keeping partial results",
			slog.Int("requested", requested),
			slog.Int("available", len(channelMap)),
			slog.Any("error", err),
		)
		return channelMap
	}

	logger.Debug("Near-milestone channel batch fetched",
		slog.Int("requested", requested),
		slog.Int("fetched", len(channelMap)),
	)
	return channelMap
}

func (ys *schedulerImpl) checkApproachingAlert(ctx context.Context, nm ytstats.NearMilestoneEntry, member *domain.Member, currentSubs uint64, now time.Time) {
	progressPct := float64(currentSubs) / float64(nm.NextMilestone)
	if progressPct < ApproachingThresholdRatio {
		return
	}

	alreadyNotified, err := ys.statsRepo.HasApproachingNotified(ctx, nm.ChannelID, nm.NextMilestone)
	if err != nil {
		ys.logger.Warn("Failed to check approaching notification status",
			slog.String("channel", nm.ChannelID),
			slog.Any("error", err))
		return
	}
	if alreadyNotified {
		return
	}

	if err := ys.statsRepo.SaveApproachingNotification(ctx, nm.ChannelID, nm.NextMilestone, currentSubs, now); err != nil {
		ys.logger.Warn("Failed to save approaching notification",
			slog.String("channel", nm.ChannelID),
			slog.Any("error", err))
		return
	}

	remaining := nm.NextMilestone - currentSubs
	ys.logger.Info("Approaching milestone alert triggered",
		slog.String("member", member.Name),
		slog.Any("milestone", nm.NextMilestone),
		slog.Any("current_subs", currentSubs),
		slog.Any("remaining", remaining))
}
