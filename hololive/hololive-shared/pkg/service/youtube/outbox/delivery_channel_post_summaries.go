package outbox

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type ChannelPostDeliverySummary struct {
	ChannelID                  string     `json:"channel_id"`
	EarliestObservedAt         *time.Time `json:"earliest_observed_at,omitempty"`
	LatestObservedAt           *time.Time `json:"latest_observed_at,omitempty"`
	DetectedPostCount          int64      `json:"detected_post_count"`
	AlarmSentPostCount         int64      `json:"alarm_sent_post_count"`
	SuccessPostCount           int64      `json:"success_post_count"`
	FailedPostCount            int64      `json:"failed_post_count"`
	DetectedUnsentPostCount    int64      `json:"detected_unsent_post_count"`
	CommunityDetectedPostCount int64      `json:"community_detected_post_count"`
	ShortsDetectedPostCount    int64      `json:"shorts_detected_post_count"`
}

func (r *DeliveryTelemetryRepository) ListChannelPostDeliverySummariesSince(
	ctx context.Context,
	since time.Time,
) ([]ChannelPostDeliverySummary, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("list channel post delivery summaries since: db is nil")
	}
	if since.IsZero() {
		return nil, fmt.Errorf("list channel post delivery summaries since: since is empty")
	}

	posts, err := r.ListPostSendCountsSince(ctx, since)
	if err != nil {
		return nil, fmt.Errorf("list channel post delivery summaries since: load post send counts: %w", err)
	}

	summaries, err := BuildChannelPostDeliverySummaries(posts)
	if err != nil {
		return nil, fmt.Errorf("list channel post delivery summaries since: %w", err)
	}

	return summaries, nil
}

func BuildChannelPostDeliverySummaries(posts []PostSendCount) ([]ChannelPostDeliverySummary, error) {
	if len(posts) == 0 {
		return []ChannelPostDeliverySummary{}, nil
	}

	accumulators := make(map[string]*channelPostDeliverySummaryAccumulator, len(posts))
	for i := range posts {
		channelID := strings.TrimSpace(posts[i].ChannelID)
		if channelID == "" {
			return nil, fmt.Errorf("post[%d] channel id is empty", i)
		}

		accumulator, ok := accumulators[channelID]
		if !ok {
			accumulator = &channelPostDeliverySummaryAccumulator{
				summary: ChannelPostDeliverySummary{ChannelID: channelID},
			}
			accumulators[channelID] = accumulator
		}

		if err := accumulator.add(posts[i]); err != nil {
			return nil, fmt.Errorf("post[%d] %s: %w", i, strings.TrimSpace(posts[i].ContentID), err)
		}
	}

	summaries := make([]ChannelPostDeliverySummary, 0, len(accumulators))
	for _, accumulator := range accumulators {
		summaries = append(summaries, accumulator.summary)
	}

	sort.SliceStable(summaries, func(i, j int) bool {
		left := channelPostDeliverySummarySortTime(summaries[i])
		right := channelPostDeliverySummarySortTime(summaries[j])
		if !left.Equal(right) {
			return left.After(right)
		}
		return summaries[i].ChannelID < summaries[j].ChannelID
	})

	return summaries, nil
}

type channelPostDeliverySummaryAccumulator struct {
	summary ChannelPostDeliverySummary
}

func (a *channelPostDeliverySummaryAccumulator) add(post PostSendCount) error {
	observedAt, err := postLatencyObservedAt(post)
	if err != nil {
		return err
	}
	observedAt = observedAt.UTC()

	if a.summary.EarliestObservedAt == nil || observedAt.Before(a.summary.EarliestObservedAt.UTC()) {
		a.summary.EarliestObservedAt = cloneUTCTimePtr(&observedAt)
	}
	if a.summary.LatestObservedAt == nil || observedAt.After(a.summary.LatestObservedAt.UTC()) {
		a.summary.LatestObservedAt = cloneUTCTimePtr(&observedAt)
	}

	a.summary.DetectedPostCount++

	switch post.AlarmType {
	case domain.AlarmTypeCommunity:
		a.summary.CommunityDetectedPostCount++
	case domain.AlarmTypeShorts:
		a.summary.ShortsDetectedPostCount++
	default:
		return fmt.Errorf("unsupported alarm type: %s", post.AlarmType)
	}

	if hasChannelPostDeliverySendActivity(post) {
		a.summary.AlarmSentPostCount++
	}
	if hasChannelPostDeliverySuccess(post) {
		a.summary.SuccessPostCount++
	} else {
		a.summary.DetectedUnsentPostCount++
	}
	if hasChannelPostDeliveryFailure(post) {
		a.summary.FailedPostCount++
	}

	return nil
}

func hasChannelPostDeliverySendActivity(post PostSendCount) bool {
	return post.OutboxCount > 0 ||
		post.SuccessSendCount > 0 ||
		post.FailedAttemptCount > 0 ||
		post.AlarmSentAt != nil ||
		post.FirstEventAt != nil ||
		post.LastEventAt != nil
}

func hasChannelPostDeliverySuccess(post PostSendCount) bool {
	return post.AlarmSentAt != nil ||
		post.SuccessSendCount > 0 ||
		post.FirstSuccessAt != nil ||
		post.LastSuccessAt != nil
}

func hasChannelPostDeliveryFailure(post PostSendCount) bool {
	return post.FailedAttemptCount > 0
}

func channelPostDeliverySummarySortTime(summary ChannelPostDeliverySummary) time.Time {
	if summary.LatestObservedAt != nil {
		return summary.LatestObservedAt.UTC()
	}
	if summary.EarliestObservedAt != nil {
		return summary.EarliestObservedAt.UTC()
	}
	return time.Time{}
}
