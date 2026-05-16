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

package polling

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

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

	clauses, args := collectTrackingIdentityClauses(trackingRows)
	if len(clauses) == 0 {
		return nil
	}

	outboxRows, err := loadPersistedOutboxSentState(ctx, tx, clauses, args)
	if err != nil {
		return err
	}
	if len(outboxRows) == 0 {
		return nil
	}

	sentAtByIdentity, identityByOutboxID, outboxIDs := buildPersistedSentStateMaps(outboxRows)
	if err := mergePersistedDeliverySentState(ctx, tx, outboxIDs, identityByOutboxID, sentAtByIdentity); err != nil {
		return err
	}
	if len(sentAtByIdentity) == 0 {
		return nil
	}
	applyPersistedSentStateToTrackingRows(trackingRows, sentAtByIdentity)
	return nil
}

func collectTrackingIdentityClauses(trackingRows []*domain.YouTubeContentAlarmTracking) ([]string, []any) {
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
		identityKey := notificationIdentityKey(row.Kind, contentID)
		if _, ok := identitySeen[identityKey]; ok {
			continue
		}
		identitySeen[identityKey] = struct{}{}
		clauses = append(clauses, "(kind = ? AND content_id = ?)")
		args = append(args, row.Kind, contentID)
	}
	return clauses, args
}

func loadPersistedOutboxSentState(
	ctx context.Context,
	tx *gorm.DB,
	clauses []string,
	args []any,
) ([]persistedOutboxSentStateRow, error) {
	var outboxRows []persistedOutboxSentStateRow
	if err := tx.WithContext(ctx).
		Model(&domain.YouTubeNotificationOutbox{}).
		Select("id, kind, content_id, sent_at").
		Where(strings.Join(clauses, " OR "), args...).
		Find(&outboxRows).Error; err != nil {
		return nil, fmt.Errorf("query outbox rows: %w", err)
	}
	return outboxRows, nil
}

func buildPersistedSentStateMaps(
	outboxRows []persistedOutboxSentStateRow,
) (map[string]time.Time, map[int64]string, []int64) {
	sentAtByIdentity := make(map[string]time.Time, len(outboxRows))
	identityByOutboxID := make(map[int64]string, len(outboxRows))
	outboxIDs := make([]int64, 0, len(outboxRows))
	for i := range outboxRows {
		contentID := strings.TrimSpace(outboxRows[i].ContentID)
		if contentID == "" {
			continue
		}
		identityKey := notificationIdentityKey(outboxRows[i].Kind, contentID)
		identityByOutboxID[outboxRows[i].ID] = identityKey
		outboxIDs = append(outboxIDs, outboxRows[i].ID)
		if outboxRows[i].SentAt != nil && !outboxRows[i].SentAt.IsZero() {
			updateIdentitySentAtMin(sentAtByIdentity, identityKey, yttimestamp.Normalize(*outboxRows[i].SentAt))
		}
	}
	return sentAtByIdentity, identityByOutboxID, outboxIDs
}

func mergePersistedDeliverySentState(
	ctx context.Context,
	tx *gorm.DB,
	outboxIDs []int64,
	identityByOutboxID map[int64]string,
	sentAtByIdentity map[string]time.Time,
) error {
	if len(outboxIDs) == 0 {
		return nil
	}

	var deliveryRows []persistedDeliverySentStateRow
	if err := tx.WithContext(ctx).
		Model(&domain.YouTubeNotificationDelivery{}).
		Select("outbox_id, sent_at").
		Where("outbox_id IN ? AND status = ? AND sent_at IS NOT NULL", outboxIDs, domain.OutboxStatusSent).
		Scan(&deliveryRows).Error; err != nil {
		return fmt.Errorf("query sent delivery rows: %w", err)
	}
	for i := range deliveryRows {
		identityKey, ok := identityByOutboxID[deliveryRows[i].OutboxID]
		if !ok || deliveryRows[i].SentAt == nil || deliveryRows[i].SentAt.IsZero() {
			continue
		}
		updateIdentitySentAtMin(sentAtByIdentity, identityKey, yttimestamp.Normalize(*deliveryRows[i].SentAt))
	}
	return nil
}

func applyPersistedSentStateToTrackingRows(
	trackingRows []*domain.YouTubeContentAlarmTracking,
	sentAtByIdentity map[string]time.Time,
) {
	for i := range trackingRows {
		applyPersistedSentStateToTrackingRow(trackingRows[i], sentAtByIdentity)
	}
}

func applyPersistedSentStateToTrackingRow(
	row *domain.YouTubeContentAlarmTracking,
	sentAtByIdentity map[string]time.Time,
) {
	contentID, ok := persistedSentStateContentID(row)
	if !ok {
		return
	}
	sentAt, ok := sentAtByIdentity[notificationIdentityKey(row.Kind, contentID)]
	if !ok {
		return
	}
	applyTrackingAlarmSentAt(row, sentAt)
}

func persistedSentStateContentID(row *domain.YouTubeContentAlarmTracking) (string, bool) {
	if row == nil || !isCommunityShortsOutboxKind(row.Kind) {
		return "", false
	}
	contentID := strings.TrimSpace(row.ContentID)
	return contentID, contentID != ""
}

func applyTrackingAlarmSentAt(row *domain.YouTubeContentAlarmTracking, sentAt time.Time) {
	if row.AlarmSentAt != nil && !sentAt.Before(*row.AlarmSentAt) {
		return
	}
	sentAtCopy := sentAt
	row.AlarmSentAt = &sentAtCopy
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
