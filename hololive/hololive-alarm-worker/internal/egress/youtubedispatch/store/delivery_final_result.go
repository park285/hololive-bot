package store

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubeoutbox/deliverysql"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type TerminalCommunityShortsOutboxResult struct {
	OutboxID             int64
	Kind                 domain.OutboxKind
	ChannelID            string
	ContentID            string
	Payload              string
	Status               domain.OutboxStatus
	SentAt               *time.Time
	Error                string
	TargetRoomCount      int
	SuccessfulRoomCount  int
	FailedRoomCount      int
	AggregatedFailReason string
	Tracked              bool
	ActualPublishedAt    *time.Time
	AlarmSentAt          *time.Time
	AlarmLatencyMillis   *int64
}

type terminalCommunityShortsTracking struct {
	Kind               domain.OutboxKind `db:"kind"`
	CanonicalContentID string            `db:"canonical_content_id"`
	ActualPublishedAt  *time.Time        `db:"actual_published_at"`
	AlarmSentAt        *time.Time        `db:"alarm_sent_at"`
	AlarmLatencyMillis *int64            `db:"alarm_latency_millis"`
}

type terminalTrackingKey struct {
	kind               domain.OutboxKind
	canonicalContentID string
}

var (
	terminalCommunityShortsOutboxKinds    = deliverysql.Texts([]domain.OutboxKind{domain.OutboxKindNewShort, domain.OutboxKindCommunityPost})
	terminalCommunityShortsOutboxStatuses = deliverysql.Texts([]domain.OutboxStatus{domain.OutboxStatusSent, domain.OutboxStatusFailed})
)

func (r *DeliveryRepository) LoadTerminalCommunityShortsOutboxResults(ctx context.Context, outboxIDs []int64) ([]TerminalCommunityShortsOutboxResult, error) {
	uniqueIDs := deliverysql.UniqueInt64s(outboxIDs)
	if r == nil || r.db == nil || len(uniqueIDs) == 0 {
		return nil, nil
	}

	var outboxes []domain.YouTubeNotificationOutbox

	if err := dbx.SelectSQL(ctx, r.db, &outboxes, "load terminal community/shorts outboxes", loadTerminalCommunityShortsOutboxResultsSQL,
		uniqueIDs, terminalCommunityShortsOutboxKinds, terminalCommunityShortsOutboxStatuses); err != nil {
		return nil, fmt.Errorf("load terminal community/shorts outboxes: %w", err)
	}

	if len(outboxes) == 0 {
		return nil, nil
	}

	outboxResultIDs := make([]int64, 0, len(outboxes))
	for i := range outboxes {
		outboxResultIDs = append(outboxResultIDs, outboxes[i].ID)
	}

	var deliveries []domain.YouTubeNotificationDelivery

	if err := dbx.SelectSQL(ctx, r.db, &deliveries, "load terminal community/shorts deliveries", loadTerminalCommunityShortsOutboxResults2SQL, outboxResultIDs); err != nil {
		return nil, fmt.Errorf("load terminal community/shorts deliveries: %w", err)
	}

	deliveriesByOutbox := make(map[int64][]domain.YouTubeNotificationDelivery, len(outboxResultIDs))

	for i := range deliveries {
		row := deliveries[i]

		deliveriesByOutbox[row.OutboxID] = append(deliveriesByOutbox[row.OutboxID], row)
	}

	results := make([]TerminalCommunityShortsOutboxResult, 0, len(outboxes))
	for i := range outboxes {
		results = append(results, summarizeTerminalCommunityShortsOutbox(&outboxes[i], deliveriesByOutbox[outboxes[i].ID]))
	}

	return results, nil
}

func terminalTrackingKeyFor(result *TerminalCommunityShortsOutboxResult) (terminalTrackingKey, bool) {
	canonicalContentID, err := CanonicalDeliveryPostID(result.Kind, result.ContentID)
	if err != nil {
		return terminalTrackingKey{}, false
	}

	return terminalTrackingKey{kind: result.Kind, canonicalContentID: canonicalContentID}, true
}

func (r *DeliveryRepository) AttachTerminalCommunityShortsTracking(ctx context.Context, results []TerminalCommunityShortsOutboxResult) error {
	tracking, err := r.loadTerminalCommunityShortsTracking(ctx, results)
	if err != nil {
		return err
	}

	for i := range results {
		key, ok := terminalTrackingKeyFor(&results[i])
		if !ok {
			continue
		}

		if track, found := tracking[key]; found {
			results[i].Tracked = true
			results[i].ActualPublishedAt = track.ActualPublishedAt
			results[i].AlarmSentAt = track.AlarmSentAt
			results[i].AlarmLatencyMillis = track.AlarmLatencyMillis
		}
	}

	return nil
}

func (r *DeliveryRepository) loadTerminalCommunityShortsTracking(
	ctx context.Context,
	results []TerminalCommunityShortsOutboxResult,
) (map[terminalTrackingKey]terminalCommunityShortsTracking, error) {
	kinds := make([]string, 0, len(results))
	canonicalIDs := make([]string, 0, len(results))

	for i := range results {
		if key, ok := terminalTrackingKeyFor(&results[i]); ok {
			kinds = append(kinds, string(key.kind))
			canonicalIDs = append(canonicalIDs, key.canonicalContentID)
		}
	}

	if len(kinds) == 0 {
		return map[terminalTrackingKey]terminalCommunityShortsTracking{}, nil
	}

	var rows []terminalCommunityShortsTracking

	if err := dbx.SelectSQL(ctx, r.db, &rows, "load terminal community/shorts tracking", loadTerminalCommunityShortsTrackingSQL, kinds, canonicalIDs); err != nil {
		return nil, fmt.Errorf("load terminal community/shorts tracking: %w", err)
	}

	tracking := make(map[terminalTrackingKey]terminalCommunityShortsTracking, len(rows))
	for i := range rows {
		tracking[terminalTrackingKey{kind: rows[i].Kind, canonicalContentID: rows[i].CanonicalContentID}] = rows[i]
	}

	return tracking, nil
}

func summarizeTerminalCommunityShortsOutbox(
	outbox *domain.YouTubeNotificationOutbox,
	deliveries []domain.YouTubeNotificationDelivery,
) TerminalCommunityShortsOutboxResult {
	result := TerminalCommunityShortsOutboxResult{
		OutboxID:        outbox.ID,
		Kind:            outbox.Kind,
		ChannelID:       outbox.ChannelID,
		ContentID:       strings.TrimSpace(outbox.ContentID),
		Payload:         outbox.Payload,
		Status:          outbox.Status,
		SentAt:          outbox.SentAt,
		Error:           strings.TrimSpace(outbox.Error),
		TargetRoomCount: len(deliveries),
	}

	result.SuccessfulRoomCount, result.FailedRoomCount, result.AggregatedFailReason = summarizeTerminalCommunityShortsDeliveries(deliveries)
	if result.AggregatedFailReason == "" {
		result.AggregatedFailReason = result.Error
	}

	return result
}

func summarizeTerminalCommunityShortsDeliveries(deliveries []domain.YouTubeNotificationDelivery) (int, int, string) {
	reasons := make([]string, 0)
	seenReasons := make(map[string]struct{}, len(deliveries))
	successfulRoomCount := 0
	failedRoomCount := 0

	for i := range deliveries {
		row := deliveries[i]
		successIncrement, failureIncrement := terminalCommunityShortsDeliveryStatusCounts(row.Status)

		successfulRoomCount += successIncrement
		failedRoomCount += failureIncrement

		reasons = appendUniqueTerminalCommunityShortsDeliveryReason(reasons, seenReasons, row.Error)
	}

	if len(reasons) > 0 {
		slices.Sort(reasons)

		return successfulRoomCount, failedRoomCount, strings.Join(reasons, " | ")
	}

	return successfulRoomCount, failedRoomCount, ""
}

func terminalCommunityShortsDeliveryStatusCounts(status domain.OutboxStatus) (sent, failed int) {
	switch status {
	case domain.OutboxStatusSent:
		return 1, 0
	case domain.OutboxStatusFailed, DeliveryStatusQuarantined:
		return 0, 1
	case domain.OutboxStatusPending, DeliveryStatusSending:
		return 0, 0
	default:
		return 0, 0
	}
}

func appendUniqueTerminalCommunityShortsDeliveryReason(reasons []string, seenReasons map[string]struct{}, rawReason string) []string {
	reason := strings.TrimSpace(rawReason)
	if reason == "" {
		return reasons
	}

	if _, exists := seenReasons[reason]; exists {
		return reasons
	}

	seenReasons[reason] = struct{}{}

	return append(reasons, reason)
}
