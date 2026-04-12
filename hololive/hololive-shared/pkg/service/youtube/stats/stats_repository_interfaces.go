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

package stats

import (
	"context"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type StatsWriteRepository interface {
	SaveStats(ctx context.Context, stats *domain.TimestampedStats) error
	SaveStatsBatch(ctx context.Context, stats []*domain.TimestampedStats) error
	RecordChange(ctx context.Context, change *domain.StatsChange) error
	RecordChangeBatch(ctx context.Context, changes []*domain.StatsChange) error
}

type StatsReadRepository interface {
	GetLatestStats(ctx context.Context, channelID string) (*domain.TimestampedStats, error)
	GetLatestStatsForChannels(ctx context.Context, channelIDs []string) (map[string]*domain.TimestampedStats, error)
	GetTopGainers(ctx context.Context, since time.Time, limit int) ([]domain.RankEntry, error)
}

type MilestoneRepository interface {
	GetAchievedMilestones(ctx context.Context, channelIDs []string, milestoneType domain.MilestoneType) (map[string][]uint64, error)
	SaveMilestone(ctx context.Context, milestone *domain.Milestone) error
	HasAchievedMilestone(ctx context.Context, channelID string, milestoneType domain.MilestoneType, value uint64) (bool, error)
	GetAllMilestones(ctx context.Context, filter MilestoneFilter) (*MilestoneResult, error)
	GetNearMilestoneMembers(ctx context.Context, thresholdPct float64, milestones []uint64, limit int) ([]NearMilestoneEntry, error)
	CountNearMilestoneMembers(ctx context.Context, thresholdPct float64, milestones []uint64) (int, error)
	GetClosestMilestoneMembers(ctx context.Context, limit int, milestones []uint64) ([]NearMilestoneEntry, error)
	GetMilestoneStats(ctx context.Context) (*MilestoneStats, error)
}

type SubscriberGraphRepository interface {
	GetSubscriberGraph(ctx context.Context, channelID string, days int) (*SubscriberGraphData, error)
}

type NotificationRepository interface {
	GetUnnotifiedChanges(ctx context.Context, limit int) ([]*domain.StatsChange, error)
	MarkChangeNotified(ctx context.Context, channelID string, detectedAt time.Time) error
	GetUnnotifiedMilestones(ctx context.Context, limit int) ([]MilestoneNotification, error)
	MarkMilestoneNotified(ctx context.Context, channelID string, milestoneType string, value uint64) error
	MarkMilestonesNotifiedBatch(ctx context.Context, milestones []MilestoneNotification) error
	HasApproachingNotified(ctx context.Context, channelID string, milestoneValue uint64) (bool, error)
	SaveApproachingNotification(ctx context.Context, channelID string, milestoneValue, currentSubs uint64, notifiedAt time.Time) error
	GetUnnotifiedApproaching(ctx context.Context, limit int) ([]ApproachingNotification, error)
	MarkApproachingChatNotified(ctx context.Context, channelID string, milestoneValue uint64) error
	MarkApproachingChatNotifiedBatch(ctx context.Context, notifications []ApproachingNotification) error
}

type StatsServiceRepository interface {
	GetLatestStats(ctx context.Context, channelID string) (*domain.TimestampedStats, error)
	SaveStats(ctx context.Context, stats *domain.TimestampedStats) error
}

type StatsSchedulerRepository interface {
	GetLatestStats(ctx context.Context, channelID string) (*domain.TimestampedStats, error)
	GetLatestStatsForChannels(ctx context.Context, channelIDs []string) (map[string]*domain.TimestampedStats, error)
	SaveStatsBatch(ctx context.Context, stats []*domain.TimestampedStats) error
	SaveStats(ctx context.Context, stats *domain.TimestampedStats) error
	RecordChange(ctx context.Context, change *domain.StatsChange) error
	RecordChangeBatch(ctx context.Context, changes []*domain.StatsChange) error
	GetAchievedMilestones(ctx context.Context, channelIDs []string, milestoneType domain.MilestoneType) (map[string][]uint64, error)
	HasAchievedMilestone(ctx context.Context, channelID string, milestoneType domain.MilestoneType, value uint64) (bool, error)
	SaveMilestone(ctx context.Context, milestone *domain.Milestone) error
	GetNearMilestoneMembers(ctx context.Context, thresholdPct float64, milestones []uint64, limit int) ([]NearMilestoneEntry, error)
	HasApproachingNotified(ctx context.Context, channelID string, milestoneValue uint64) (bool, error)
	SaveApproachingNotification(ctx context.Context, channelID string, milestoneValue, currentSubs uint64, notifiedAt time.Time) error
	GetUnnotifiedMilestones(ctx context.Context, limit int) ([]MilestoneNotification, error)
	MarkMilestoneNotified(ctx context.Context, channelID string, milestoneType string, value uint64) error
	MarkMilestonesNotifiedBatch(ctx context.Context, milestones []MilestoneNotification) error
	GetUnnotifiedApproaching(ctx context.Context, limit int) ([]ApproachingNotification, error)
	MarkApproachingChatNotified(ctx context.Context, channelID string, milestoneValue uint64) error
	MarkApproachingChatNotifiedBatch(ctx context.Context, notifications []ApproachingNotification) error
}

type StatsCommandRepository interface {
	GetTopGainers(ctx context.Context, since time.Time, limit int) ([]domain.RankEntry, error)
	GetSubscriberGraph(ctx context.Context, channelID string, days int) (*SubscriberGraphData, error)
}

type StatsDashboardRepository interface {
	GetLatestStatsForChannels(ctx context.Context, channelIDs []string) (map[string]*domain.TimestampedStats, error)
	GetAllMilestones(ctx context.Context, filter MilestoneFilter) (*MilestoneResult, error)
	GetNearMilestoneMembers(ctx context.Context, thresholdPct float64, milestones []uint64, limit int) ([]NearMilestoneEntry, error)
	GetMilestoneStats(ctx context.Context) (*MilestoneStats, error)
	CountNearMilestoneMembers(ctx context.Context, thresholdPct float64, milestones []uint64) (int, error)
}
