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
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubeoutbox/deliverysql"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubeoutbox/telemetry"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubeoutbox/timeline"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
	"github.com/kapu/hololive-shared/pkg/service/youtube/tracking/observation"
)

type deliveryAlarmSentTarget struct {
	Kind      domain.OutboxKind `db:"kind"`
	ContentID string            `db:"content_id"`
}

// LoadAlarmSentMarksForDeliveryIDs는 community/shorts 발송 행마다 같은 발송 시각의 tracking 발송 표시를 만든다.
func LoadAlarmSentMarksForDeliveryIDs(ctx context.Context, db dbx.Querier, ids []int64, sentAt time.Time) ([]observation.AlarmSentMark, error) {
	uniqueIDs := deliverysql.UniqueInt64s(ids)
	if len(uniqueIDs) == 0 {
		return nil, nil
	}

	postKinds := []domain.OutboxKind{domain.OutboxKindCommunityPost, domain.OutboxKindNewShort}

	var targets []deliveryAlarmSentTarget

	if err := dbx.SelectSQL(ctx, db, &targets, "query delivery alarm sent targets",
		mustSQL("delivery_repository_tracking_0066_01.sql"), uniqueIDs, deliverysql.Texts(postKinds)); err != nil {
		return nil, fmt.Errorf("query delivery alarm sent targets: %w", err)
	}

	marks := make([]observation.AlarmSentMark, 0, len(targets))
	for i := range targets {
		marks = append(marks, observation.AlarmSentMark{
			Kind:        targets[i].Kind,
			ContentID:   targets[i].ContentID,
			AlarmSentAt: sentAt,
		})
	}

	return marks, nil
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

func markSentDeliveryTracking(
	ctx context.Context,
	tx dbx.Querier,
	trackingMarks []observation.AlarmSentMark,
) error {
	if err := observation.NewRepository(tx).MarkAlarmSentBatch(ctx, trackingMarks); err != nil {
		return fmt.Errorf("update tracking rows: %w", err)
	}

	return nil
}

// persistSentTrackingLatencyClassifications는 같은 CompleteSent 트랜잭션의 성공 시도 telemetry까지 분류해 저장한다.
func persistSentTrackingLatencyClassifications(
	ctx context.Context,
	tx dbx.Querier,
	trackingMarks []observation.AlarmSentMark,
) error {
	if len(trackingMarks) == 0 {
		return nil
	}

	identities := make([]timeline.PostTrackingIdentity, 0, len(trackingMarks))
	for i := range trackingMarks {
		identities = append(identities, timeline.PostTrackingIdentity{
			Kind: trackingMarks[i].Kind, ContentID: trackingMarks[i].ContentID,
		})
	}

	repository, err := telemetry.NewRepository(tx)
	if err != nil {
		return fmt.Errorf("persist tracking latency classifications: new telemetry repository: %w", err)
	}

	if err := repository.PersistPostLatencyClassificationsByIdentities(ctx, identities); err != nil {
		return fmt.Errorf("persist tracking latency classifications: %w", err)
	}

	return nil
}
