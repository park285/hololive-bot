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

var alarmSentMarkOutboxKinds = deliverysql.Texts([]domain.OutboxKind{domain.OutboxKindCommunityPost, domain.OutboxKindNewShort})

func LoadAlarmSentMarksForDeliveryIDs(ctx context.Context, db dbx.Querier, ids []int64, sentAt time.Time) ([]observation.AlarmSentMark, error) {
	uniqueIDs := deliverysql.UniqueInt64s(ids)
	if len(uniqueIDs) == 0 {
		return nil, nil
	}

	var targets []deliveryAlarmSentTarget

	if err := dbx.SelectSQL(ctx, db, &targets, "query delivery alarm sent targets",
		loadAlarmSentMarksForDeliveryIDsSQL, uniqueIDs, alarmSentMarkOutboxKinds); err != nil {
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
