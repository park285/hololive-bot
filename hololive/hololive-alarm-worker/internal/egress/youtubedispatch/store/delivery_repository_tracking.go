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
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/deliverysql"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/telemetry"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/timeline"
	"github.com/kapu/hololive-shared/pkg/service/youtube/tracking/observation"
)

type deliveryAlarmSentTarget struct {
	Kind      domain.OutboxKind `db:"kind"`
	ContentID string            `db:"content_id"`
}

func LoadAlarmSentMarksForDeliveryIDs(ctx context.Context, db dbx.Querier, ids []int64, sentAt time.Time, claimTokens []dispatchstate.ClaimToken) ([]observation.AlarmSentMark, error) {
	out, err := loadAlarmSentMarksForDeliveryIDsWithStatus(ctx, db, ids, sentAt, claimTokens, nil)
	if err != nil {
		return out, fmt.Errorf("load alarm sent marks for delivery IDs with status: %w", err)
	}

	return out, nil
}

func loadAlarmSentMarksForDeliveryIDsWithStatus(ctx context.Context, db dbx.Querier, ids []int64, sentAt time.Time, claimTokens []dispatchstate.ClaimToken, status *domain.OutboxStatus) ([]observation.AlarmSentMark, error) {
	uniqueIDs := deliverysql.UniqueInt64s(ids)
	if len(uniqueIDs) == 0 {
		return nil, nil
	}

	claimTokensByIdentity, err := collectClaimTokensByIdentity(claimTokens)
	if err != nil {
		return nil, fmt.Errorf("collect claim tokens by identity: %w", err)
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

	marks := make([]observation.AlarmSentMark, 0, len(targets))
	for i := range targets {
		mark := observation.AlarmSentMark{
			Kind:        targets[i].Kind,
			ContentID:   targets[i].ContentID,
			AlarmSentAt: sentAt,
		}

		canonicalPostID, err := CanonicalDeliveryPostID(targets[i].Kind, targets[i].ContentID)
		if err != nil {
			return nil, fmt.Errorf("canonical delivery post id: %w", err)
		}

		claimIdentity := DeliveryClaimIdentityKey(targets[i].Kind, canonicalPostID)

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
			return nil, fmt.Errorf("claim token identity and authorized at: %w", err)
		}

		if err := collectClaimTokenAuthorizedAt(collected, identity, authorizedAt); err != nil {
			return nil, fmt.Errorf("collect claim token authorized at: %w", err)
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
		return errors.New("collect claim tokens: conflicting authorized_at for logical identity")
	}

	return nil
}

func DeliveryClaimIdentityKey(kind domain.OutboxKind, postID string) string {
	return string(kind) + "\x00" + strings.TrimSpace(postID)
}

func CanonicalDeliveryPostID(kind domain.OutboxKind, contentID string) (string, error) {
	canonicalContentID, err := ytcontentid.ForOutboxKind(kind, contentID)
	if err != nil {
		return "", fmt.Errorf("canonicalize delivery content id: %w", err)
	}

	return canonicalContentID, nil
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

func persistSentDeliveryTracking(
	ctx context.Context,
	tx dbx.Querier,
	trackingMarks []observation.AlarmSentMark,
) error {
	if err := observation.NewRepositoryContext(ctx, tx).MarkAlarmSentBatch(ctx, trackingMarks); err != nil {
		return fmt.Errorf("update tracking rows: %w", err)
	}

	if len(trackingMarks) == 0 {
		return nil
	}

	identities := make([]timeline.PostTrackingIdentity, 0, len(trackingMarks))
	for i := range trackingMarks {
		identities = append(identities, timeline.PostTrackingIdentity{
			Kind: trackingMarks[i].Kind, ContentID: trackingMarks[i].ContentID,
		})
	}

	if err := telemetry.NewRepository(tx).PersistPostLatencyClassificationsByIdentities(ctx, identities); err != nil {
		return fmt.Errorf("persist tracking latency classifications: %w", err)
	}

	return nil
}
