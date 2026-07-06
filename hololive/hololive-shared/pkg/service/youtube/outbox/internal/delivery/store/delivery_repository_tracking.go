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

package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/deliverysql"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatchstate"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

func LoadAlarmSentMarksForPendingDeliveryIDs(ctx context.Context, db dbx.Querier, ids []int64, sentAt time.Time, claimTokens []dispatchstate.ClaimToken) ([]trackingrepo.AlarmSentMark, error) {
	status := domain.OutboxStatusPending
	return loadAlarmSentMarksForDeliveryIDsWithStatus(ctx, db, ids, sentAt, claimTokens, &status)
}

func LoadAlarmSentMarksForDeliveryIDs(ctx context.Context, db dbx.Querier, ids []int64, sentAt time.Time, claimTokens []dispatchstate.ClaimToken) ([]trackingrepo.AlarmSentMark, error) {
	return loadAlarmSentMarksForDeliveryIDsWithStatus(ctx, db, ids, sentAt, claimTokens, nil)
}

func loadAlarmSentMarksForDeliveryIDsWithStatus(ctx context.Context, db dbx.Querier, ids []int64, sentAt time.Time, claimTokens []dispatchstate.ClaimToken, status *domain.OutboxStatus) ([]trackingrepo.AlarmSentMark, error) {
	uniqueIDs := deliverysql.UniqueInt64s(ids)
	if len(uniqueIDs) == 0 {
		return nil, nil
	}

	claimTokensByIdentity, err := collectClaimTokensByIdentity(claimTokens)
	if err != nil {
		return nil, err
	}

	postKinds := []domain.OutboxKind{domain.OutboxKindCommunityPost, domain.OutboxKindNewShort}
	var targets []deliveryAlarmSentTarget
	args := deliverysql.AppendDeliveryInt64Args(nil, uniqueIDs)
	args = deliverysql.AppendDeliveryOutboxKindArgs(args, postKinds...)
	statusClause := ""
	if status != nil {
		statusClause = " AND d.status = ?"
		args = append(args, *status)
	}
	if err := deliverysql.SelectDeliverySQL(ctx, db, &targets, "query delivery alarm sent targets", mustSQL("delivery_repository_tracking_0066_01.sql")+deliverysql.DeliveryInClause("d.id", len(uniqueIDs))+`
		  AND `+deliverysql.DeliveryInClause("o.kind", len(postKinds))+`
		`+statusClause, args...); err != nil {
		return nil, fmt.Errorf("query delivery alarm sent targets: %w", err)
	}

	marks := make([]trackingrepo.AlarmSentMark, 0, len(targets))
	for i := range targets {
		mark := trackingrepo.AlarmSentMark{
			Kind:        targets[i].Kind,
			ContentID:   targets[i].ContentID,
			AlarmSentAt: sentAt,
		}
		claimIdentity := DeliveryClaimIdentityKey(targets[i].Kind, CanonicalDeliveryPostID(targets[i].Kind, targets[i].ContentID))
		if authorizedAt, ok := claimTokensByIdentity[claimIdentity]; ok {
			authorizedAtCopy := authorizedAt
			mark.AuthorizedAt = &authorizedAtCopy
		}
		marks = append(marks, mark)
	}

	return marks, nil
}

func collectClaimTokensByIdentity(claimTokens []dispatchstate.ClaimToken) (map[string]time.Time, error) {
	if len(claimTokens) == 0 {
		return map[string]time.Time{}, nil
	}

	collected := make(map[string]time.Time, len(claimTokens))
	for i := range claimTokens {
		identity, authorizedAt, err := claimTokenIdentityAndAuthorizedAt(claimTokens[i], i)
		if err != nil {
			return nil, err
		}
		if err := collectClaimTokenAuthorizedAt(collected, identity, authorizedAt); err != nil {
			return nil, err
		}
	}

	return collected, nil
}

func claimTokenIdentityAndAuthorizedAt(claimToken dispatchstate.ClaimToken, index int) (string, time.Time, error) {
	postID := strings.TrimSpace(claimToken.PostID)
	if postID == "" {
		return "", time.Time{}, fmt.Errorf("collect claim tokens: post id is empty at index %d", index)
	}
	if claimToken.AuthorizedAt.IsZero() {
		return "", time.Time{}, fmt.Errorf("collect claim tokens: authorized_at is empty at index %d", index)
	}
	return DeliveryClaimIdentityKey(claimToken.Kind, postID), claimToken.AuthorizedAt.UTC(), nil
}

func collectClaimTokenAuthorizedAt(collected map[string]time.Time, identity string, authorizedAt time.Time) error {
	existingAuthorizedAt, ok := collected[identity]
	if !ok {
		collected[identity] = authorizedAt
		return nil
	}
	if !existingAuthorizedAt.Equal(authorizedAt) {
		return fmt.Errorf("collect claim tokens: conflicting authorized_at for %s", identity)
	}
	return nil
}

func DeliveryClaimIdentityKey(kind domain.OutboxKind, postID string) string {
	return string(kind) + "\x00" + strings.TrimSpace(postID)
}

func CanonicalDeliveryPostID(kind domain.OutboxKind, contentID string) string {
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

func parseStatusCounts(counts []deliveryStatusCount) (pending, sent, failed int64) {
	for _, item := range counts {
		applyStatusCount(item, &pending, &sent, &failed)
	}
	return pending, sent, failed
}

func applyStatusCount(item deliveryStatusCount, pending, sent, failed *int64) {
	switch item.Status {
	case domain.OutboxStatusPending, DeliveryStatusSending:
		*pending += item.Count
	case domain.OutboxStatusSent:
		*sent += item.Count
	case domain.OutboxStatusFailed, DeliveryStatusQuarantined:
		*failed += item.Count
	}
}

func resolveOutboxStatus(pending, sent, failed int64) domain.OutboxStatus {
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

func UniqueStrings(values []string) []string {
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
