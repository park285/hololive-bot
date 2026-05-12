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

package outbox

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

func loadAlarmSentMarksForPendingDeliveryIDs(ctx context.Context, db *gorm.DB, ids []int64, sentAt time.Time, claimTokens []deliveryClaimToken) ([]trackingrepo.AlarmSentMark, error) {
	uniqueIDs := uniqueInt64s(ids)
	if len(uniqueIDs) == 0 {
		return nil, nil
	}

	claimTokensByIdentity, err := collectClaimTokensByIdentity(claimTokens)
	if err != nil {
		return nil, err
	}

	var targets []deliveryAlarmSentTarget
	if err := db.WithContext(ctx).
		Table("youtube_notification_delivery AS d").
		Select("o.kind AS kind, o.content_id AS content_id").
		Joins("JOIN youtube_notification_outbox o ON o.id = d.outbox_id").
		Where("d.id IN ? AND d.status = ?", uniqueIDs, domain.OutboxStatusPending).
		Where("o.kind IN ?", []domain.OutboxKind{domain.OutboxKindCommunityPost, domain.OutboxKindNewShort}).
		Scan(&targets).Error; err != nil {
		return nil, fmt.Errorf("query delivery alarm sent targets: %w", err)
	}

	marks := make([]trackingrepo.AlarmSentMark, 0, len(targets))
	for i := range targets {
		mark := trackingrepo.AlarmSentMark{
			Kind:        targets[i].Kind,
			ContentID:   targets[i].ContentID,
			AlarmSentAt: sentAt,
		}
		claimIdentity := deliveryClaimIdentityKey(targets[i].Kind, canonicalDeliveryPostID(targets[i].Kind, targets[i].ContentID))
		if authorizedAt, ok := claimTokensByIdentity[claimIdentity]; ok {
			authorizedAtCopy := authorizedAt
			mark.AuthorizedAt = &authorizedAtCopy
		}
		marks = append(marks, mark)
	}

	return marks, nil
}

func collectClaimTokensByIdentity(claimTokens []deliveryClaimToken) (map[string]time.Time, error) {
	if len(claimTokens) == 0 {
		return map[string]time.Time{}, nil
	}

	collected := make(map[string]time.Time, len(claimTokens))
	for i := range claimTokens {
		postID := strings.TrimSpace(claimTokens[i].postID)
		if postID == "" {
			return nil, fmt.Errorf("collect claim tokens: post id is empty at index %d", i)
		}
		if claimTokens[i].authorizedAt.IsZero() {
			return nil, fmt.Errorf("collect claim tokens: authorized_at is empty at index %d", i)
		}

		identity := deliveryClaimIdentityKey(claimTokens[i].kind, postID)
		authorizedAt := claimTokens[i].authorizedAt.UTC()
		if existingAuthorizedAt, ok := collected[identity]; ok {
			if !existingAuthorizedAt.Equal(authorizedAt) {
				return nil, fmt.Errorf("collect claim tokens: conflicting authorized_at for %s", identity)
			}
			continue
		}

		collected[identity] = authorizedAt
	}

	return collected, nil
}

func deliveryClaimIdentityKey(kind domain.OutboxKind, postID string) string {
	return string(kind) + "\x00" + strings.TrimSpace(postID)
}

func canonicalDeliveryPostID(kind domain.OutboxKind, contentID string) string {
	normalizedContentID := strings.TrimSpace(contentID)
	canonicalContentID, err := ytcontentid.ForOutboxKind(kind, normalizedContentID)
	if err != nil {
		return normalizedContentID
	}
	return canonicalContentID
}

func groupOutboxIDsByAggregateStatus(outboxIDs []int64, counts []deliveryStatusCount) map[domain.OutboxStatus][]int64 {
	perOutboxCounts := make(map[int64][]deliveryStatusCount, len(outboxIDs))
	for _, item := range counts {
		perOutboxCounts[item.OutboxID] = append(perOutboxCounts[item.OutboxID], item)
	}

	grouped := make(map[domain.OutboxStatus][]int64, 3)
	for _, outboxID := range outboxIDs {
		pendingCount, sentCount, failedCount := parseStatusCounts(perOutboxCounts[outboxID])
		status := resolveOutboxStatus(pendingCount, sentCount, failedCount)
		grouped[status] = append(grouped[status], outboxID)
	}

	return grouped
}

func parseStatusCounts(counts []deliveryStatusCount) (pending int64, sent int64, failed int64) {
	for _, item := range counts {
		switch item.Status {
		case domain.OutboxStatusPending:
			pending = item.Count
		case domain.OutboxStatusSent:
			sent = item.Count
		case domain.OutboxStatusFailed:
			failed = item.Count
		}
	}
	return pending, sent, failed
}

func resolveOutboxStatus(pending int64, sent int64, failed int64) domain.OutboxStatus {
	switch {
	case pending > 0:
		return domain.OutboxStatusPending
	case failed > 0:
		return domain.OutboxStatusFailed
	case sent > 0:
		return domain.OutboxStatusSent
	default:
		return domain.OutboxStatusPending
	}
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	unique := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return unique
}
