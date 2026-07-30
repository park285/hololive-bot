package latencycause

import (
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"

	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/analytics"
	deliverytimeline "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/timeline"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/reports/shared"
)

type timelineKey struct {
	channelID string
	alarmType domain.AlarmType
	contentID string
}

func buildTimelineKey(channelID string, alarmType domain.AlarmType, contentID string) timelineKey {
	return timelineKey{
		channelID: strings.TrimSpace(channelID),
		alarmType: alarmType,
		contentID: strings.TrimSpace(contentID),
	}
}

func buildTimelineIndex(timelineRows []deliverytimeline.PostDeliveryTimeline) map[timelineKey]deliverytimeline.PostDeliveryTimeline {
	index := make(map[timelineKey]deliverytimeline.PostDeliveryTimeline, len(timelineRows))
	for i := range timelineRows {
		timeline := normalizeDeliveryTimeline(&timelineRows[i])
		key := buildTimelineKey(timeline.ChannelID, timeline.AlarmType, timeline.ContentID)
		if key.contentID == "" {
			continue
		}
		index[key] = timeline
	}
	return index
}

func normalizePostSendCount(row *analytics.PostSendCount) analytics.PostSendCount {
	if row == nil {
		return analytics.PostSendCount{}
	}
	normalized := *row
	normalized.ChannelID = strings.TrimSpace(normalized.ChannelID)
	normalized.PostID = strings.TrimSpace(normalized.PostID)
	normalized.ContentID = strings.TrimSpace(normalized.ContentID)
	normalized.ActualPublishedAt = shared.CloneSendCountTime(normalized.ActualPublishedAt)
	normalized.DetectedAt = shared.CloneSendCountTime(normalized.DetectedAt)
	normalized.AlarmSentAt = shared.CloneSendCountTime(normalized.AlarmSentAt)
	normalized.FirstEventAt = shared.CloneSendCountTime(normalized.FirstEventAt)
	normalized.LastEventAt = shared.CloneSendCountTime(normalized.LastEventAt)
	normalized.FirstSuccessAt = shared.CloneSendCountTime(normalized.FirstSuccessAt)
	normalized.LastSuccessAt = shared.CloneSendCountTime(normalized.LastSuccessAt)
	return normalized
}

func normalizeDeliveryTimeline(row *deliverytimeline.PostDeliveryTimeline) deliverytimeline.PostDeliveryTimeline {
	if row == nil {
		return deliverytimeline.PostDeliveryTimeline{}
	}
	normalized := *row
	normalized.ChannelID = strings.TrimSpace(normalized.ChannelID)
	normalized.PostID = strings.TrimSpace(normalized.PostID)
	normalized.ContentID = strings.TrimSpace(normalized.ContentID)
	normalized.PublishToDetectMillis = shared.CloneSendCountInt64(normalized.PublishToDetectMillis)
	if normalized.DelaySource == "" {
		normalized.DelaySource = deliverytimeline.PostDelaySourceNone
	}
	normalized.QueueWaitMillis = shared.CloneSendCountInt64(normalized.QueueWaitMillis)
	normalized.RetryAccumulationMillis = shared.CloneSendCountInt64(normalized.RetryAccumulationMillis)
	if normalized.InternalDelayCause == "" {
		normalized.InternalDelayCause = deliverytimeline.PostInternalDelayCauseNone
	}
	normalized.LatencyClassification = shared.CloneLatencyClassification(&normalized.LatencyClassification)
	return normalized
}

func resolvePostID(sendCount *analytics.PostSendCount) string {
	if sendCount == nil {
		return ""
	}
	postID := strings.TrimSpace(sendCount.PostID)
	if postID != "" {
		return postID
	}
	return strings.TrimSpace(sendCount.ContentID)
}
