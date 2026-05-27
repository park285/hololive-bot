package analytics

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func BuildChannelPostDeliverySummaries(posts []PostSendCount) ([]ChannelPostDeliverySummary, error) {
	if len(posts) == 0 {
		return []ChannelPostDeliverySummary{}, nil
	}

	accumulators, err := buildChannelPostDeliverySummaryAccumulators(posts)
	if err != nil {
		return nil, err
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

func buildChannelPostDeliverySummaryAccumulators(
	posts []PostSendCount,
) (map[string]*channelPostDeliverySummaryAccumulator, error) {
	accumulators := make(map[string]*channelPostDeliverySummaryAccumulator, len(posts))
	for i := range posts {
		channelID := strings.TrimSpace(posts[i].ChannelID)
		if channelID == "" {
			return nil, fmt.Errorf("post[%d] channel id is empty", i)
		}

		accumulator := channelPostDeliverySummaryAccumulatorFor(accumulators, channelID)
		if err := accumulator.add(posts[i]); err != nil {
			return nil, fmt.Errorf("post[%d] %s: %w", i, strings.TrimSpace(posts[i].ContentID), err)
		}
	}
	return accumulators, nil
}

func channelPostDeliverySummaryAccumulatorFor(
	accumulators map[string]*channelPostDeliverySummaryAccumulator,
	channelID string,
) *channelPostDeliverySummaryAccumulator {
	accumulator, ok := accumulators[channelID]
	if ok {
		return accumulator
	}

	accumulator = &channelPostDeliverySummaryAccumulator{
		summary: ChannelPostDeliverySummary{ChannelID: channelID},
	}
	accumulators[channelID] = accumulator
	return accumulator
}

type channelPostDeliverySummaryAccumulator struct {
	summary ChannelPostDeliverySummary
}

func (a *channelPostDeliverySummaryAccumulator) add(post PostSendCount) error {
	observedAt, err := PostLatencyObservedAt(post)
	if err != nil {
		return err
	}
	observedAt = observedAt.UTC()

	a.addObservedAt(observedAt)
	a.summary.DetectedPostCount++

	if err := a.addDetectedPostType(post.AlarmType); err != nil {
		return err
	}
	a.addDeliveryResult(post)

	return nil
}

func (a *channelPostDeliverySummaryAccumulator) addObservedAt(observedAt time.Time) {
	if a.summary.EarliestObservedAt == nil || observedAt.Before(a.summary.EarliestObservedAt.UTC()) {
		a.summary.EarliestObservedAt = CloneUTCTimePtr(&observedAt)
	}
	if a.summary.LatestObservedAt == nil || observedAt.After(a.summary.LatestObservedAt.UTC()) {
		a.summary.LatestObservedAt = CloneUTCTimePtr(&observedAt)
	}
}

func (a *channelPostDeliverySummaryAccumulator) addDetectedPostType(alarmType domain.AlarmType) error {
	switch alarmType {
	case domain.AlarmTypeCommunity:
		a.summary.CommunityDetectedPostCount++
	case domain.AlarmTypeShorts:
		a.summary.ShortsDetectedPostCount++
	default:
		return fmt.Errorf("unsupported alarm type: %s", alarmType)
	}
	return nil
}

func (a *channelPostDeliverySummaryAccumulator) addDeliveryResult(post PostSendCount) {
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
