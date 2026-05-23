package batchrepo

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
		state, ok := buildCommunityShortsAlarmState(trackingRows[i])
		if !ok {
			continue
		}
		upsertCommunityShortsAlarmState(rowsByKey, state)
	}

	return sortedCommunityShortsAlarmStates(rowsByKey)
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

func upsertCommunityShortsAlarmState(rowsByKey map[string]*domain.YouTubeCommunityShortsAlarmState, state *domain.YouTubeCommunityShortsAlarmState) {
	key := communityShortsAlarmStateKey(state)
	if existing, ok := rowsByKey[key]; ok {
		mergeCommunityShortsAlarmState(existing, state)
		return
	}
	rowsByKey[key] = state
}

func communityShortsAlarmStateKey(state *domain.YouTubeCommunityShortsAlarmState) string {
	return string(state.Kind) + "\x00" + state.PostID
}

func mergeCommunityShortsAlarmState(existing *domain.YouTubeCommunityShortsAlarmState, state *domain.YouTubeCommunityShortsAlarmState) {
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
	mergeCommunityShortsAlarmSentAt(existing, state)
}

func mergeCommunityShortsAlarmSentAt(existing *domain.YouTubeCommunityShortsAlarmState, state *domain.YouTubeCommunityShortsAlarmState) {
	switch {
	case existing.AlarmSentAt == nil:
		existing.AlarmSentAt = state.AlarmSentAt
	case state.AlarmSentAt != nil && state.AlarmSentAt.Before(*existing.AlarmSentAt):
		existing.AlarmSentAt = state.AlarmSentAt
	}
}

func sortedCommunityShortsAlarmStates(rowsByKey map[string]*domain.YouTubeCommunityShortsAlarmState) []*domain.YouTubeCommunityShortsAlarmState {
	keys := sortedCommunityShortsAlarmStateKeys(rowsByKey)
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

func sortedCommunityShortsAlarmStateKeys(rowsByKey map[string]*domain.YouTubeCommunityShortsAlarmState) []string {
	keys := make([]string, 0, len(rowsByKey))
	for key := range rowsByKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
