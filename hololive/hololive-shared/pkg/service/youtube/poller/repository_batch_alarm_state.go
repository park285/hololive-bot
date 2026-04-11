package poller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
	"gorm.io/gorm"
)

type persistedOutboxAuthorizationRow struct {
	Kind      domain.OutboxKind `gorm:"column:kind"`
	ContentID string            `gorm:"column:content_id"`
	CreatedAt time.Time         `gorm:"column:created_at"`
}

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

	rows := make([]*domain.YouTubeCommunityShortsAlarmState, 0, len(rowsByKey))
	for _, row := range rowsByKey {
		if row == nil {
			continue
		}
		row.DeliveryStatus = domain.ResolveYouTubeCommunityShortsAlarmStateStatus(row.AuthorizedAt, row.AlarmSentAt)
		rows = append(rows, row)
	}
	return rows
}

func reconcileAlarmStatesWithPersistedAuthorization(ctx context.Context, tx *gorm.DB, alarmStates []*domain.YouTubeCommunityShortsAlarmState) error {
	if len(alarmStates) == 0 || tx == nil {
		return nil
	}

	clauses := make([]string, 0, len(alarmStates))
	args := make([]any, 0, len(alarmStates)*2)
	identitySeen := make(map[string]struct{}, len(alarmStates))
	for i := range alarmStates {
		row := alarmStates[i]
		if row == nil || !isCommunityShortsOutboxKind(row.Kind) {
			continue
		}
		contentID := strings.TrimSpace(row.ContentID)
		if contentID == "" {
			continue
		}
		identityKey := fmt.Sprintf("%s::%s", row.Kind, contentID)
		if _, ok := identitySeen[identityKey]; ok {
			continue
		}
		identitySeen[identityKey] = struct{}{}
		clauses = append(clauses, "(kind = ? AND content_id = ?)")
		args = append(args, row.Kind, contentID)
	}
	if len(clauses) == 0 {
		return nil
	}

	var outboxRows []persistedOutboxAuthorizationRow
	if err := tx.WithContext(ctx).
		Model(&domain.YouTubeNotificationOutbox{}).
		Select("kind, content_id, created_at").
		Where(strings.Join(clauses, " OR "), args...).
		Find(&outboxRows).Error; err != nil {
		return fmt.Errorf("query outbox authorization rows: %w", err)
	}

	authorizedAtByIdentity := make(map[string]time.Time, len(outboxRows))
	for i := range outboxRows {
		contentID := strings.TrimSpace(outboxRows[i].ContentID)
		if contentID == "" {
			continue
		}
		identityKey := fmt.Sprintf("%s::%s", outboxRows[i].Kind, contentID)
		candidate := yttimestamp.Normalize(outboxRows[i].CreatedAt)
		if candidate.IsZero() {
			continue
		}
		if existing, ok := authorizedAtByIdentity[identityKey]; !ok || candidate.Before(existing) {
			authorizedAtByIdentity[identityKey] = candidate
		}
	}

	for i := range alarmStates {
		row := alarmStates[i]
		if row == nil || !isCommunityShortsOutboxKind(row.Kind) {
			continue
		}
		identityKey := fmt.Sprintf("%s::%s", row.Kind, strings.TrimSpace(row.ContentID))
		if authorizedAt, ok := authorizedAtByIdentity[identityKey]; ok {
			switch {
			case row.AuthorizedAt == nil:
				authorizedAtCopy := authorizedAt
				row.AuthorizedAt = &authorizedAtCopy
			case authorizedAt.Before(*row.AuthorizedAt):
				authorizedAtCopy := authorizedAt
				row.AuthorizedAt = &authorizedAtCopy
			}
		}
		row.DeliveryStatus = domain.ResolveYouTubeCommunityShortsAlarmStateStatus(row.AuthorizedAt, row.AlarmSentAt)
	}

	return nil
}
