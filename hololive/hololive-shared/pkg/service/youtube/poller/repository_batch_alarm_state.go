package poller

import (
	"sort"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

func buildCommunityShortsAlarmStates(trackingRows []*domain.YouTubeContentAlarmTracking) []*domain.YouTubeCommunityShortsAlarmState {
	if len(trackingRows) == 0 {
		return nil
	}

	rowsByKey := make(map[string]*domain.YouTubeCommunityShortsAlarmState, len(trackingRows))
	for i := range trackingRows {
		row := trackingRows[i]
		if row == nil || !isCommunityShortsOutboxKind(row.Kind) {
			continue
		}
		postID := normalizeContentID(row.Kind, row.ContentID)
		if postID == "" {
			continue
		}

		state := &domain.YouTubeCommunityShortsAlarmState{
			Kind:              row.Kind,
			PostID:            postID,
			ContentID:         strings.TrimSpace(row.ContentID),
			ChannelID:         strings.TrimSpace(row.ChannelID),
			ActualPublishedAt: yttimestamp.NormalizePtr(row.ActualPublishedAt),
			DetectedAt:        yttimestamp.Normalize(row.DetectedAt),
			AlarmSentAt:       yttimestamp.NormalizePtr(row.AlarmSentAt),
		}
		key := string(state.Kind) + "\x00" + state.PostID
		if existing, ok := rowsByKey[key]; ok {
			if strings.TrimSpace(state.ContentID) != "" {
				existing.ContentID = state.ContentID
			}
			if strings.TrimSpace(state.ChannelID) != "" {
				existing.ChannelID = state.ChannelID
			}
			if state.ActualPublishedAt != nil {
				existing.ActualPublishedAt = state.ActualPublishedAt
			}
			if state.DetectedAt.Before(existing.DetectedAt) {
				existing.DetectedAt = state.DetectedAt
			}
			switch {
			case existing.AlarmSentAt == nil:
				existing.AlarmSentAt = state.AlarmSentAt
			case state.AlarmSentAt != nil && state.AlarmSentAt.Before(*existing.AlarmSentAt):
				existing.AlarmSentAt = state.AlarmSentAt
			}
			continue
		}
		rowsByKey[key] = state
	}

	keys := make([]string, 0, len(rowsByKey))
	for key := range rowsByKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	rows := make([]*domain.YouTubeCommunityShortsAlarmState, 0, len(keys))
	for _, key := range keys {
		row := rowsByKey[key]
		if row == nil {
			continue
		}
		row.DeliveryStatus = domain.ResolveYouTubeCommunityShortsAlarmStateStatus(row.AuthorizedAt, row.AlarmSentAt)
		rows = append(rows, row)
	}
	return rows
}
