package latencycause

import (
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"

	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/shared"
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

func buildTimelineIndex(timelineRows []outbox.PostDeliveryTimeline) map[timelineKey]outbox.PostDeliveryTimeline {
	index := make(map[timelineKey]outbox.PostDeliveryTimeline, len(timelineRows))
	for i := range timelineRows {
		timeline := normalizeDeliveryTimeline(timelineRows[i])
		key := buildTimelineKey(timeline.ChannelID, timeline.AlarmType, timeline.ContentID)
		if key.contentID == "" {
			continue
		}
		index[key] = timeline
	}
	return index
}

func normalizePostSendCount(row outbox.PostSendCount) outbox.PostSendCount {
	row.ChannelID = strings.TrimSpace(row.ChannelID)
	row.PostID = strings.TrimSpace(row.PostID)
	row.ContentID = strings.TrimSpace(row.ContentID)
	row.ActualPublishedAt = shared.CloneSendCountTime(row.ActualPublishedAt)
	row.DetectedAt = shared.CloneSendCountTime(row.DetectedAt)
	row.AlarmSentAt = shared.CloneSendCountTime(row.AlarmSentAt)
	row.FirstEventAt = shared.CloneSendCountTime(row.FirstEventAt)
	row.LastEventAt = shared.CloneSendCountTime(row.LastEventAt)
	row.FirstSuccessAt = shared.CloneSendCountTime(row.FirstSuccessAt)
	row.LastSuccessAt = shared.CloneSendCountTime(row.LastSuccessAt)
	return row
}

func normalizeDeliveryTimeline(row outbox.PostDeliveryTimeline) outbox.PostDeliveryTimeline {
	row.ChannelID = strings.TrimSpace(row.ChannelID)
	row.PostID = strings.TrimSpace(row.PostID)
	row.ContentID = strings.TrimSpace(row.ContentID)
	row.PublishToDetectMillis = shared.CloneSendCountInt64(row.PublishToDetectMillis)
	if row.DelaySource == "" {
		row.DelaySource = outbox.PostDelaySourceNone
	}
	row.QueueWaitMillis = shared.CloneSendCountInt64(row.QueueWaitMillis)
	row.RetryAccumulationMillis = shared.CloneSendCountInt64(row.RetryAccumulationMillis)
	if row.InternalDelayCause == "" {
		row.InternalDelayCause = outbox.PostInternalDelayCauseNone
	}
	row.LatencyClassification = shared.CloneLatencyClassification(row.LatencyClassification)
	return row
}

func resolvePostID(sendCount outbox.PostSendCount) string {
	postID := strings.TrimSpace(sendCount.PostID)
	if postID != "" {
		return postID
	}
	return strings.TrimSpace(sendCount.ContentID)
}
