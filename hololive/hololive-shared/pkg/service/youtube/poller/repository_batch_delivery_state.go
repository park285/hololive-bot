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

	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

type failedNotificationOutboxRow struct {
	ID        int64             `gorm:"column:id"`
	Kind      domain.OutboxKind `gorm:"column:kind"`
	ContentID string            `gorm:"column:content_id"`
}

type completedNotificationIdentityCandidate struct {
	notification *domain.YouTubeNotificationOutbox
	contentID    string
	identityKey  string
}

func loadFailedNotificationOutboxRows(ctx context.Context, tx *gorm.DB, notifications []*domain.YouTubeNotificationOutbox) ([]failedNotificationOutboxRow, error) {
	clauses, args := failedNotificationOutboxQueryArgs(notifications)
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

func failedNotificationOutboxQueryArgs(notifications []*domain.YouTubeNotificationOutbox) ([]string, []any) {
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
	return clauses, args
}

func loadCompletedNotificationSentAtByIdentity(ctx context.Context, tx *gorm.DB, notifications []*domain.YouTubeNotificationOutbox) (map[string]time.Time, error) {
	completed := make(map[string]time.Time)
	if len(notifications) == 0 || tx == nil {
		return completed, nil
	}

	repo := trackingrepo.NewRepository(tx)
	candidates := collectCompletedNotificationIdentityCandidates(notifications)
	for i := range candidates {
		if err := recordCompletedNotificationSentAtByCandidate(ctx, repo, completed, candidates[i]); err != nil {
			return nil, err
		}
	}

	return completed, nil
}

func recordCompletedNotificationSentAtByCandidate(
	ctx context.Context,
	repo *trackingrepo.GormRepository,
	completed map[string]time.Time,
	candidate completedNotificationIdentityCandidate,
) error {
	trackingRow, err := repo.FindByIdentity(ctx, candidate.notification.Kind, candidate.contentID)
	if err != nil {
		return fmt.Errorf("load notification tracking row: %w", err)
	}
	recordCompletedNotificationTrackingSentAt(completed, candidate.identityKey, trackingRow)

	postID := resolveNotificationReactivationPostID(candidate.notification.Kind, candidate.contentID, candidate.notification.Payload)
	if postID == "" {
		return nil
	}
	stateRow, err := repo.FindAlarmStateByPostID(ctx, candidate.notification.Kind, postID)
	if err != nil {
		return fmt.Errorf("load notification alarm state: %w", err)
	}
	recordCompletedNotificationAlarmStateSentAt(completed, candidate.identityKey, stateRow)
	return nil
}

func collectCompletedNotificationIdentityCandidates(notifications []*domain.YouTubeNotificationOutbox) []completedNotificationIdentityCandidate {
	candidates := make([]completedNotificationIdentityCandidate, 0, len(notifications))
	seen := make(map[string]struct{}, len(notifications))
	for i := range notifications {
		notification := notifications[i]
		candidate, ok := completedNotificationIdentityCandidateFor(notification)
		if !ok {
			continue
		}
		if _, ok := seen[candidate.identityKey]; ok {
			continue
		}
		seen[candidate.identityKey] = struct{}{}
		candidates = append(candidates, candidate)
	}
	return candidates
}

func completedNotificationIdentityCandidateFor(notification *domain.YouTubeNotificationOutbox) (completedNotificationIdentityCandidate, bool) {
	if notification == nil || !isCommunityShortsOutboxKind(notification.Kind) {
		return completedNotificationIdentityCandidate{}, false
	}
	contentID := strings.TrimSpace(notification.ContentID)
	if contentID == "" {
		return completedNotificationIdentityCandidate{}, false
	}
	return completedNotificationIdentityCandidate{
		notification: notification,
		contentID:    contentID,
		identityKey:  notificationIdentityKey(notification.Kind, contentID),
	}, true
}

func recordCompletedNotificationTrackingSentAt(completed map[string]time.Time, identityKey string, row *domain.YouTubeContentAlarmTracking) {
	if row == nil || row.AlarmSentAt == nil || row.AlarmSentAt.IsZero() {
		return
	}
	completed[identityKey] = yttimestamp.Normalize(*row.AlarmSentAt)
}

func recordCompletedNotificationAlarmStateSentAt(completed map[string]time.Time, identityKey string, row *domain.YouTubeCommunityShortsAlarmState) {
	if row == nil || row.AlarmSentAt == nil || row.AlarmSentAt.IsZero() {
		return
	}
	completed[identityKey] = selectEarlierSentAt(completed[identityKey], yttimestamp.Normalize(*row.AlarmSentAt))
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
		if err := finalizeCompletedFailedNotificationRow(ctx, tx, rows[i], completedSentAtByIdentity); err != nil {
			return err
		}
	}

	return nil
}

func finalizeCompletedFailedNotificationRow(ctx context.Context, tx *gorm.DB, row failedNotificationOutboxRow, completedSentAtByIdentity map[string]time.Time) error {
	sentAt, ok := completedSentAtForFailedNotification(row, completedSentAtByIdentity)
	if !ok {
		return nil
	}
	if err := updateCompletedFailedNotificationOutboxRow(ctx, tx, row.ID, sentAt); err != nil {
		return err
	}
	return updateCompletedFailedNotificationDeliveryRows(ctx, tx, row.ID, sentAt)
}

func completedSentAtForFailedNotification(row failedNotificationOutboxRow, completedSentAtByIdentity map[string]time.Time) (time.Time, bool) {
	identityKey := notificationIdentityKey(row.Kind, row.ContentID)
	sentAt, ok := completedSentAtByIdentity[identityKey]
	if !ok || sentAt.IsZero() {
		return time.Time{}, false
	}
	return yttimestamp.Normalize(sentAt), true
}

func updateCompletedFailedNotificationOutboxRow(ctx context.Context, tx *gorm.DB, id int64, sentAt time.Time) error {
	if err := tx.WithContext(ctx).
		Model(&domain.YouTubeNotificationOutbox{}).
		Where("id = ? AND status = ?", id, domain.OutboxStatusFailed).
		Updates(completedFailedNotificationUpdates(sentAt)).Error; err != nil {
		return fmt.Errorf("update completed outbox row %d: %w", id, err)
	}
	return nil
}

func updateCompletedFailedNotificationDeliveryRows(ctx context.Context, tx *gorm.DB, outboxID int64, sentAt time.Time) error {
	if err := tx.WithContext(ctx).
		Model(&domain.YouTubeNotificationDelivery{}).
		Where("outbox_id = ? AND status = ?", outboxID, domain.OutboxStatusFailed).
		Updates(completedFailedNotificationUpdates(sentAt)).Error; err != nil {
		return fmt.Errorf("update completed delivery rows for outbox %d: %w", outboxID, err)
	}
	return nil
}

func completedFailedNotificationUpdates(sentAt time.Time) map[string]any {
	return map[string]any{
		"status":    domain.OutboxStatusSent,
		"locked_at": nil,
		"sent_at": gorm.Expr(
			"CASE WHEN sent_at IS NULL OR sent_at > ? THEN ? ELSE sent_at END",
			sentAt,
			sentAt,
		),
		"error": "",
	}
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
		return resolveShortNotificationReactivationPostID(contentID, payload)
	case domain.OutboxKindCommunityPost:
		return resolveCommunityNotificationReactivationPostID(contentID, payload)
	}

	return strings.TrimSpace(contentID)
}

func resolveShortNotificationReactivationPostID(contentID, payload string) string {
	var parsed shortNotificationPublishedAtPayload
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
		return strings.TrimSpace(contentID)
	}
	return firstNonBlankNotificationPostID(parsed.CanonicalPostID, contentID, parsed.VideoID)
}

func resolveCommunityNotificationReactivationPostID(contentID, payload string) string {
	var parsed communityNotificationPublishedAtPayload
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
		return strings.TrimSpace(contentID)
	}
	return firstNonBlankNotificationPostID(parsed.CanonicalPostID, contentID, parsed.PostID)
}

func firstNonBlankNotificationPostID(values ...string) string {
	for i := range values {
		if postID := strings.TrimSpace(values[i]); postID != "" {
			return postID
		}
	}
	return ""
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
