package canonicalwrite

import (
	"cmp"
	"slices"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

func buildCommunityShortsAlarmStates(trackingRows []*domain.YouTubeContentAlarmTracking) []*domain.YouTubeCommunityShortsAlarmState {
	states := make([]*domain.YouTubeCommunityShortsAlarmState, 0, len(trackingRows))

	for _, row := range trackingRows {
		if state, ok := buildCommunityShortsAlarmState(row); ok {
			states = append(states, state)
		}
	}

	slices.SortStableFunc(states, func(a, b *domain.YouTubeCommunityShortsAlarmState) int {
		return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.PostID, b.PostID))
	})

	return states
}

func buildCommunityShortsAlarmState(row *domain.YouTubeContentAlarmTracking) (*domain.YouTubeCommunityShortsAlarmState, bool) {
	if row == nil || !isCommunityShortsOutboxKind(row.Kind) {
		return nil, false
	}

	postID := normalizeContentID(row.Kind, row.ContentID)
	if postID == "" {
		return nil, false
	}

	return &domain.YouTubeCommunityShortsAlarmState{
		Kind:              row.Kind,
		PostID:            postID,
		ContentID:         strings.TrimSpace(row.ContentID),
		ChannelID:         strings.TrimSpace(row.ChannelID),
		ActualPublishedAt: yttimestamp.NormalizePtr(row.ActualPublishedAt),
		DetectedAt:        yttimestamp.Normalize(row.DetectedAt),
		AlarmSentAt:       yttimestamp.NormalizePtr(row.AlarmSentAt),
	}, true
}
