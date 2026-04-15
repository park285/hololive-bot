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

package poller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"
)

type failedNotificationOutboxRow struct {
	ID        int64             `gorm:"column:id"`
	Kind      domain.OutboxKind `gorm:"column:kind"`
	ContentID string            `gorm:"column:content_id"`
}

func loadFailedNotificationOutboxRows(ctx context.Context, tx *gorm.DB, notifications []*domain.YouTubeNotificationOutbox) ([]failedNotificationOutboxRow, error) {
	clauses := make([]string, 0, len(notifications))
	args := make([]any, 0, len(notifications)*2)
	seen := make(map[string]struct{}, len(notifications))
	for i := range notifications {
		notification := notifications[i]
		if notification == nil || !isCommunityShortsOutboxKind(notification.Kind) {
			continue
		}
		contentID := strings.TrimSpace(notification.ContentID)
		if contentID == "" {
			continue
		}
		identityKey := notificationIdentityKey(notification.Kind, contentID)
		if _, ok := seen[identityKey]; ok {
			continue
		}
		seen[identityKey] = struct{}{}
		clauses = append(clauses, "(kind = ? AND content_id = ?)")
		args = append(args, notification.Kind, contentID)
	}
	if len(clauses) == 0 {
		return nil, nil
	}

	var rows []failedNotificationOutboxRow
	if err := tx.WithContext(ctx).
		Model(&domain.YouTubeNotificationOutbox{}).
		Select("id, kind, content_id").
		Where("status = ?", domain.OutboxStatusFailed).
		Where(strings.Join(clauses, " OR "), args...).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("query failed outbox rows: %w", err)
	}
	return rows, nil
}

func loadCompletedNotificationSentAtByIdentity(ctx context.Context, tx *gorm.DB, notifications []*domain.YouTubeNotificationOutbox) (map[string]time.Time, error) {
	completed := make(map[string]time.Time)
	if len(notifications) == 0 || tx == nil {
		return completed, nil
	}

	repo := trackingrepo.NewRepository(tx)
	seen := make(map[string]struct{}, len(notifications))
	for i := range notifications {
		notification := notifications[i]
		if notification == nil || !isCommunityShortsOutboxKind(notification.Kind) {
			continue
		}
		contentID := strings.TrimSpace(notification.ContentID)
		if contentID == "" {
			continue
		}
		identityKey := notificationIdentityKey(notification.Kind, contentID)
		if _, ok := seen[identityKey]; ok {
			continue
		}
		seen[identityKey] = struct{}{}

		trackingRow, err := repo.FindByIdentity(ctx, notification.Kind, contentID)
		if err != nil {
			return nil, fmt.Errorf("load notification tracking row: %w", err)
		}
		if trackingRow != nil && trackingRow.AlarmSentAt != nil && !trackingRow.AlarmSentAt.IsZero() {
			completed[identityKey] = yttimestamp.Normalize(*trackingRow.AlarmSentAt)
		}

		postID := resolveNotificationReactivationPostID(notification.Kind, contentID, notification.Payload)
		if postID == "" {
			continue
		}
		stateRow, err := repo.FindAlarmStateByPostID(ctx, notification.Kind, postID)
		if err != nil {
			return nil, fmt.Errorf("load notification alarm state: %w", err)
		}
		if stateRow != nil && stateRow.AlarmSentAt != nil && !stateRow.AlarmSentAt.IsZero() {
			completed[identityKey] = selectEarlierSentAt(completed[identityKey], yttimestamp.Normalize(*stateRow.AlarmSentAt))
		}
	}

	return completed, nil
}

func partitionFailedNotificationOutboxRows(rows []failedNotificationOutboxRow, completedSentAtByIdentity map[string]time.Time) ([]failedNotificationOutboxRow, []failedNotificationOutboxRow) {
	completed := make([]failedNotificationOutboxRow, 0, len(rows))
	reactivations := make([]failedNotificationOutboxRow, 0, len(rows))
	for i := range rows {
		identityKey := notificationIdentityKey(rows[i].Kind, rows[i].ContentID)
		if _, ok := completedSentAtByIdentity[identityKey]; ok {
			completed = append(completed, rows[i])
			continue
		}
		reactivations = append(reactivations, rows[i])
	}
	return completed, reactivations
}

func finalizeCompletedFailedNotificationRows(ctx context.Context, tx *gorm.DB, rows []failedNotificationOutboxRow, completedSentAtByIdentity map[string]time.Time) error {
	for i := range rows {
		identityKey := notificationIdentityKey(rows[i].Kind, rows[i].ContentID)
		sentAt, ok := completedSentAtByIdentity[identityKey]
		if !ok || sentAt.IsZero() {
			continue
		}
		sentAt = yttimestamp.Normalize(sentAt)

		if err := tx.WithContext(ctx).
			Model(&domain.YouTubeNotificationOutbox{}).
			Where("id = ? AND status = ?", rows[i].ID, domain.OutboxStatusFailed).
			Updates(map[string]any{
				"status":    domain.OutboxStatusSent,
				"locked_at": nil,
				"sent_at": gorm.Expr(
					"CASE WHEN sent_at IS NULL OR sent_at > ? THEN ? ELSE sent_at END",
					sentAt,
					sentAt,
				),
				"error": "",
			}).Error; err != nil {
			return fmt.Errorf("update completed outbox row %d: %w", rows[i].ID, err)
		}

		if err := tx.WithContext(ctx).
			Model(&domain.YouTubeNotificationDelivery{}).
			Where("outbox_id = ? AND status = ?", rows[i].ID, domain.OutboxStatusFailed).
			Updates(map[string]any{
				"status":    domain.OutboxStatusSent,
				"locked_at": nil,
				"sent_at": gorm.Expr(
					"CASE WHEN sent_at IS NULL OR sent_at > ? THEN ? ELSE sent_at END",
					sentAt,
					sentAt,
				),
				"error": "",
			}).Error; err != nil {
			return fmt.Errorf("update completed delivery rows for outbox %d: %w", rows[i].ID, err)
		}
	}

	return nil
}

func filterCompletedNotifications(notifications []*domain.YouTubeNotificationOutbox, completedSentAtByIdentity map[string]time.Time) []*domain.YouTubeNotificationOutbox {
	if len(notifications) == 0 {
		return nil
	}

	filtered := make([]*domain.YouTubeNotificationOutbox, 0, len(notifications))
	for i := range notifications {
		notification := notifications[i]
		if notification == nil || !isCommunityShortsOutboxKind(notification.Kind) {
			filtered = append(filtered, notification)
			continue
		}
		if _, ok := completedSentAtByIdentity[notificationIdentityKey(notification.Kind, notification.ContentID)]; ok {
			continue
		}
		filtered = append(filtered, notification)
	}
	return filtered
}

func collectFailedNotificationOutboxIDs(rows []failedNotificationOutboxRow) []int64 {
	if len(rows) == 0 {
		return nil
	}

	ids := make([]int64, 0, len(rows))
	seen := make(map[int64]struct{}, len(rows))
	for i := range rows {
		if _, ok := seen[rows[i].ID]; ok {
			continue
		}
		seen[rows[i].ID] = struct{}{}
		ids = append(ids, rows[i].ID)
	}
	return ids
}

func resolveNotificationReactivationPostID(kind domain.OutboxKind, contentID, payload string) string {
	switch kind {
	case domain.OutboxKindNewVideo, domain.OutboxKindNewShort:
		var parsed shortNotificationPublishedAtPayload
		if err := json.Unmarshal([]byte(payload), &parsed); err == nil {
			if postID := strings.TrimSpace(parsed.CanonicalPostID); postID != "" {
				return postID
			}
			if postID := strings.TrimSpace(contentID); postID != "" {
				return postID
			}
			if postID := strings.TrimSpace(parsed.VideoID); postID != "" {
				return postID
			}
		}
	case domain.OutboxKindCommunityPost:
		var parsed communityNotificationPublishedAtPayload
		if err := json.Unmarshal([]byte(payload), &parsed); err == nil {
			if postID := strings.TrimSpace(parsed.CanonicalPostID); postID != "" {
				return postID
			}
			if postID := strings.TrimSpace(contentID); postID != "" {
				return postID
			}
			if postID := strings.TrimSpace(parsed.PostID); postID != "" {
				return postID
			}
		}
	}

	return strings.TrimSpace(contentID)
}

func notificationIdentityKey(kind domain.OutboxKind, contentID string) string {
	return fmt.Sprintf("%s::%s", kind, strings.TrimSpace(contentID))
}

func selectEarlierSentAt(current time.Time, candidate time.Time) time.Time {
	if current.IsZero() {
		return candidate
	}
	if candidate.IsZero() {
		return current
	}
	if candidate.Before(current) {
		return candidate
	}
	return current
}

func rearmFailedDeliveryRows(ctx context.Context, tx *gorm.DB, outboxIDs []int64, nextAttemptAt time.Time) error {
	if len(outboxIDs) == 0 {
		return nil
	}

	result := tx.WithContext(ctx).
		Model(&domain.YouTubeNotificationDelivery{}).
		Where("outbox_id IN ? AND status = ?", outboxIDs, domain.OutboxStatusFailed).
		Updates(map[string]any{
			"status":          domain.OutboxStatusPending,
			"attempt_count":   0,
			"next_attempt_at": nextAttemptAt,
			"locked_at":       nil,
			"sent_at":         nil,
			"error":           "",
		})
	if result.Error != nil {
		return fmt.Errorf("update delivery rows: %w", result.Error)
	}
	return nil
}

type persistedOutboxSentStateRow struct {
	ID        int64             `gorm:"column:id"`
	Kind      domain.OutboxKind `gorm:"column:kind"`
	ContentID string            `gorm:"column:content_id"`
	SentAt    *time.Time        `gorm:"column:sent_at"`
}

type persistedDeliverySentStateRow struct {
	OutboxID int64      `gorm:"column:outbox_id"`
	SentAt   *time.Time `gorm:"column:sent_at"`
}

func reconcileTrackingRowsWithPersistedSendState(ctx context.Context, tx *gorm.DB, trackingRows []*domain.YouTubeContentAlarmTracking) error {
	if len(trackingRows) == 0 || tx == nil {
		return nil
	}

	clauses := make([]string, 0, len(trackingRows))
	args := make([]any, 0, len(trackingRows)*2)
	identitySeen := make(map[string]struct{}, len(trackingRows))
	for i := range trackingRows {
		row := trackingRows[i]
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

	var outboxRows []persistedOutboxSentStateRow
	if err := tx.WithContext(ctx).
		Model(&domain.YouTubeNotificationOutbox{}).
		Select("id, kind, content_id, sent_at").
		Where(strings.Join(clauses, " OR "), args...).
		Find(&outboxRows).Error; err != nil {
		return fmt.Errorf("query outbox rows: %w", err)
	}
	if len(outboxRows) == 0 {
		return nil
	}

	sentAtByIdentity := make(map[string]time.Time, len(outboxRows))
	outboxIDByIdentity := make(map[string]int64, len(outboxRows))
	outboxIDs := make([]int64, 0, len(outboxRows))
	for i := range outboxRows {
		contentID := strings.TrimSpace(outboxRows[i].ContentID)
		if contentID == "" {
			continue
		}
		identityKey := fmt.Sprintf("%s::%s", outboxRows[i].Kind, contentID)
		outboxIDByIdentity[identityKey] = outboxRows[i].ID
		outboxIDs = append(outboxIDs, outboxRows[i].ID)
		if outboxRows[i].SentAt != nil && !outboxRows[i].SentAt.IsZero() {
			updateIdentitySentAtMin(sentAtByIdentity, identityKey, yttimestamp.Normalize(*outboxRows[i].SentAt))
		}
	}

	if len(outboxIDs) > 0 {
		var deliveryRows []persistedDeliverySentStateRow
		if err := tx.WithContext(ctx).
			Model(&domain.YouTubeNotificationDelivery{}).
			Select("outbox_id, sent_at").
			Where("outbox_id IN ? AND status = ? AND sent_at IS NOT NULL", outboxIDs, domain.OutboxStatusSent).
			Scan(&deliveryRows).Error; err != nil {
			return fmt.Errorf("query sent delivery rows: %w", err)
		}

		identityByOutboxID := make(map[int64]string, len(outboxIDByIdentity))
		for identityKey, outboxID := range outboxIDByIdentity {
			identityByOutboxID[outboxID] = identityKey
		}
		for i := range deliveryRows {
			identityKey, ok := identityByOutboxID[deliveryRows[i].OutboxID]
			if !ok || deliveryRows[i].SentAt == nil || deliveryRows[i].SentAt.IsZero() {
				continue
			}
			updateIdentitySentAtMin(sentAtByIdentity, identityKey, yttimestamp.Normalize(*deliveryRows[i].SentAt))
		}
	}

	if len(sentAtByIdentity) == 0 {
		return nil
	}
	for i := range trackingRows {
		row := trackingRows[i]
		if row == nil || !isCommunityShortsOutboxKind(row.Kind) {
			continue
		}
		contentID := strings.TrimSpace(row.ContentID)
		if contentID == "" {
			continue
		}
		identityKey := fmt.Sprintf("%s::%s", row.Kind, contentID)
		sentAt, ok := sentAtByIdentity[identityKey]
		if !ok {
			continue
		}
		switch {
		case row.AlarmSentAt == nil:
			sentAtCopy := sentAt
			row.AlarmSentAt = &sentAtCopy
		case sentAt.Before(*row.AlarmSentAt):
			sentAtCopy := sentAt
			row.AlarmSentAt = &sentAtCopy
		}
	}

	return nil
}

func updateIdentitySentAtMin(sentAtByIdentity map[string]time.Time, identityKey string, candidate time.Time) {
	if candidate.IsZero() {
		return
	}
	if existing, ok := sentAtByIdentity[identityKey]; ok {
		if candidate.Before(existing) {
			sentAtByIdentity[identityKey] = candidate
		}
		return
	}
	sentAtByIdentity[identityKey] = candidate
}

func isCommunityShortsOutboxKind(kind domain.OutboxKind) bool {
	switch kind {
	case domain.OutboxKindCommunityPost, domain.OutboxKindNewShort:
		return true
	default:
		return false
	}
}
