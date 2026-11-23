package telemetry

import (
	"context"
	"errors"
	"fmt"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubeoutbox/deliverysql"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubeoutbox/timeline"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func (r *Repository) ListPostDeliveryTimelinesByOutboxIDs(ctx context.Context, outboxIDs []int64) ([]timeline.PostDeliveryTimeline, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("list post delivery timelines by outbox ids: db is nil")
	}

	uniqueIDs := deliverysql.UniqueInt64s(outboxIDs)
	if len(uniqueIDs) == 0 {
		return []timeline.PostDeliveryTimeline{}, nil
	}

	rows, err := r.listPostDeliveryTimelines(ctx, postDeliveryTimelineByOutboxIDsSQL, uniqueIDs)
	if err != nil {
		return nil, fmt.Errorf("list post delivery timelines by outbox ids: %w", err)
	}

	return rows, nil
}

func (r *Repository) ListPostDeliveryTimelinesByTrackingIdentities(
	ctx context.Context,
	identities []timeline.PostTrackingIdentity,
) ([]timeline.PostDeliveryTimeline, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("list post delivery timelines by tracking identities: db is nil")
	}

	normalized, err := timeline.NormalizePostTrackingIdentities(identities)
	if err != nil {
		return nil, fmt.Errorf("list post delivery timelines by tracking identities: %w", err)
	}

	if len(normalized) == 0 {
		return []timeline.PostDeliveryTimeline{}, nil
	}

	kinds, contentIDs := postTrackingIdentityArrays(normalized)

	rows, err := r.listPostDeliveryTimelines(ctx, postDeliveryTimelineByTrackingIdentitiesSQL, kinds, contentIDs)
	if err != nil {
		return nil, fmt.Errorf("list post delivery timelines by tracking identities: %w", err)
	}

	return rows, nil
}

var postDeliveryTimelineKinds = deliverysql.Texts([]domain.OutboxKind{domain.OutboxKindCommunityPost, domain.OutboxKindNewShort})

func (r *Repository) listPostDeliveryTimelines(
	ctx context.Context,
	query string,
	filterArgs ...any,
) ([]timeline.PostDeliveryTimeline, error) {
	var scanned []postDeliveryTimelineScanRow

	args := append([]any{postDeliveryTimelineKinds}, filterArgs...)

	if err := dbx.SelectSQL(ctx, r.db, &scanned, "scan rows", query, args...); err != nil {
		return nil, fmt.Errorf("scan rows: %w", err)
	}

	return buildPostDeliveryTimelinesFromScanRows(scanned), nil
}

func postTrackingIdentityArrays(identities []timeline.PostTrackingIdentity) (kinds, contentIDs []string) {
	kinds = make([]string, len(identities))
	contentIDs = make([]string, len(identities))

	for i := range identities {
		kinds[i] = string(identities[i].Kind)
		contentIDs[i] = identities[i].ContentID
	}

	return kinds, contentIDs
}
