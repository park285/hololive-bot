package polling

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/georgysavva/scany/v2/pgxscan"

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal/batchrepo"

	"github.com/kapu/hololive-shared/pkg/domain"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

type publishedAtResolverRepository struct {
	db              publishedAtResolverDB
	batchRepository *batchrepo.PgxBatchRepository
}

type publishedAtFinalizeResult struct {
	enqueued bool
	reason   string
}

type publishedAtFinalizeEligibility struct {
	enqueuable bool
	reason     string
}

type resolvedPublishedAtDispatchGap struct {
	candidate   trackingrepo.PublishedAtResolutionCandidate
	publishedAt time.Time
}

type resolvedPublishedAtDispatchGapRow struct {
	Kind              domain.OutboxKind `db:"kind"`
	PostID            string            `db:"post_id"`
	ContentID         string            `db:"content_id"`
	ChannelID         string            `db:"channel_id"`
	DetectedAt        time.Time         `db:"detected_at"`
	ActualPublishedAt time.Time         `db:"actual_published_at"`
}

const (
	publishedAtClaimFreshWindow              = 30 * time.Second
	resolvedPublishedAtDispatchGapRecoverFor = time.Hour
)

func newPublishedAtResolverRepository(db any) *publishedAtResolverRepository {
	querier := normalizePublishedAtResolverDB(db)
	return &publishedAtResolverRepository{
		db:              querier,
		batchRepository: batchrepo.NewPgxBatchRepositoryWithPersister(querier, newPublishedAtResolverLatencyPersisterAdapter(querier)),
	}
}

// newPublishedAtResolverLatencyPersisterAdapter는 published_at_resolver 계층을 위한
// outbox 어댑터를 생성합니다.
func newPublishedAtResolverLatencyPersisterAdapter(db dbx.Querier) batchrepo.PostLatencyClassificationPersister {
	return &publishedAtResolverLatencyPersisterAdapter{db: db}
}

type publishedAtResolverLatencyPersisterAdapter struct {
	db dbx.Querier
}

func (a *publishedAtResolverLatencyPersisterAdapter) PersistPostLatencyClassificationsByIdentities(
	ctx context.Context,
	identities []batchrepo.LatencyClassificationIdentity,
) error {
	outboxIdentities := make([]outbox.PostTrackingIdentity, 0, len(identities))
	for i := range identities {
		outboxIdentities = append(outboxIdentities, outbox.PostTrackingIdentity{
			Kind:      identities[i].Kind,
			ContentID: identities[i].ContentID,
		})
	}
	return outbox.NewDeliveryTelemetryRepository(a.db).PersistPostLatencyClassificationsByIdentities(ctx, outboxIdentities)
}

func (r *publishedAtResolverRepository) FinalizePublishedAtAndMaybeEnqueue(
	ctx context.Context,
	candidate trackingrepo.PublishedAtResolutionCandidate,
	publishedAt time.Time,
	routeDecider NotificationRouteDecider,
) (publishedAtFinalizeResult, error) {
	if r == nil || r.db == nil {
		return publishedAtFinalizeResult{}, fmt.Errorf("finalize published_at: db is nil")
	}
	if publishedAt.IsZero() {
		return publishedAtFinalizeResult{}, fmt.Errorf("finalize published_at: published_at is empty")
	}

	normalizedPublishedAt := yttimestamp.Normalize(publishedAt)
	result := publishedAtFinalizeResult{}
	err := inPublishedAtResolverTx(ctx, r.db, func(tx dbx.Querier) error {
		txRepository := trackingrepo.NewRepository(tx)
		eligibility, err := r.loadFinalizeEligibility(ctx, tx, txRepository, candidate)
		if err != nil {
			return err
		}

		notification, reason, err := r.finalizeCandidateState(
			ctx,
			tx,
			txRepository,
			candidate,
			normalizedPublishedAt,
			routeDecider,
			eligibility.enqueuable,
		)
		if err != nil {
			return err
		}
		result.reason = selectPublishedAtFinalizeReason(eligibility.reason, reason)

		return r.completeFinalizePublishedAt(ctx, tx, txRepository, candidate, notification, &result)
	})
	if err != nil {
		return publishedAtFinalizeResult{}, fmt.Errorf("finalize published_at transaction: %w", err)
	}
	return result, nil
}

func (r *publishedAtResolverRepository) finalizeCandidateState(
	ctx context.Context,
	tx dbx.Querier,
	txRepository *trackingrepo.PgxRepository,
	candidate trackingrepo.PublishedAtResolutionCandidate,
	publishedAt time.Time,
	routeDecider NotificationRouteDecider,
	enqueueAllowed bool,
) (*domain.YouTubeNotificationOutbox, string, error) {
	switch candidate.Kind {
	case domain.OutboxKindNewShort:
		return r.finalizeShort(ctx, tx, txRepository, candidate, publishedAt, routeDecider, enqueueAllowed)
	case domain.OutboxKindCommunityPost:
		return r.finalizeCommunity(ctx, tx, txRepository, candidate, publishedAt, routeDecider, enqueueAllowed)
	default:
		return nil, "", fmt.Errorf("finalize published_at: unsupported kind %s", candidate.Kind)
	}
}

func (r *publishedAtResolverRepository) loadFinalizeEligibility(
	ctx context.Context,
	tx dbx.Querier,
	txRepository *trackingrepo.PgxRepository,
	candidate trackingrepo.PublishedAtResolutionCandidate,
) (publishedAtFinalizeEligibility, error) {
	eligibility := publishedAtFinalizeEligibility{enqueuable: true}

	stateEligibility, resolved, err := r.loadFinalizeAlarmStateEligibility(ctx, tx, txRepository, candidate)
	if err != nil {
		return publishedAtFinalizeEligibility{}, err
	}
	if resolved {
		return stateEligibility, nil
	}

	trackingEligibility, resolved, err := loadFinalizeTrackingEligibility(ctx, txRepository, candidate)
	if err != nil {
		return publishedAtFinalizeEligibility{}, err
	}
	if resolved {
		return trackingEligibility, nil
	}

	return eligibility, nil
}

func (r *publishedAtResolverRepository) loadFinalizeAlarmStateEligibility(
	ctx context.Context,
	tx dbx.Querier,
	txRepository *trackingrepo.PgxRepository,
	candidate trackingrepo.PublishedAtResolutionCandidate,
) (publishedAtFinalizeEligibility, bool, error) {
	stateRow, err := txRepository.FindAlarmStateByPostID(ctx, candidate.Kind, candidate.PostID)
	if err != nil {
		return publishedAtFinalizeEligibility{}, false, fmt.Errorf("load alarm state row: %w", err)
	}
	if stateRow == nil {
		return publishedAtFinalizeEligibility{}, false, nil
	}
	if isPublishedAtAlarmStateSent(stateRow) {
		return publishedAtFinalizeEligibility{reason: "already_sent"}, true, nil
	}
	if !isPublishedAtAlarmStateClaimed(stateRow) {
		return publishedAtFinalizeEligibility{}, false, nil
	}

	return r.resolveFinalizeAlarmStateClaimEligibility(ctx, tx, txRepository, candidate, *stateRow.AuthorizedAt)
}

func isPublishedAtAlarmStateSent(stateRow *domain.YouTubeCommunityShortsAlarmState) bool {
	return stateRow.AlarmSentAt != nil && !stateRow.AlarmSentAt.IsZero()
}

func isPublishedAtAlarmStateClaimed(stateRow *domain.YouTubeCommunityShortsAlarmState) bool {
	return stateRow.AuthorizedAt != nil && !stateRow.AuthorizedAt.IsZero()
}

func (r *publishedAtResolverRepository) resolveFinalizeAlarmStateClaimEligibility(
	ctx context.Context,
	tx dbx.Querier,
	txRepository *trackingrepo.PgxRepository,
	candidate trackingrepo.PublishedAtResolutionCandidate,
	authorizedAt time.Time,
) (publishedAtFinalizeEligibility, bool, error) {
	if isPublishedAtClaimFresh(authorizedAt) {
		return publishedAtFinalizeEligibility{reason: "already_claimed"}, true, nil
	}

	exists, err := r.outboxExistsForCandidate(ctx, tx, txRepository, candidate)
	if err != nil {
		return publishedAtFinalizeEligibility{}, false, err
	}
	if exists {
		return publishedAtFinalizeEligibility{reason: "already_claimed"}, true, nil
	}

	released, err := txRepository.ReleaseAlarmStateClaim(ctx, candidate.Kind, candidate.PostID, authorizedAt)
	if err != nil {
		return publishedAtFinalizeEligibility{}, false, fmt.Errorf("release stale alarm state claim: %w", err)
	}
	if !released {
		return publishedAtFinalizeEligibility{reason: "already_claimed"}, true, nil
	}

	return publishedAtFinalizeEligibility{}, false, nil
}

func loadFinalizeTrackingEligibility(
	ctx context.Context,
	txRepository *trackingrepo.PgxRepository,
	candidate trackingrepo.PublishedAtResolutionCandidate,
) (publishedAtFinalizeEligibility, bool, error) {
	trackingRow, err := txRepository.FindByIdentity(ctx, candidate.Kind, candidate.ContentID)
	if err != nil {
		return publishedAtFinalizeEligibility{}, false, fmt.Errorf("load tracking row: %w", err)
	}
	if isPublishedAtTrackingSent(trackingRow) {
		return publishedAtFinalizeEligibility{reason: "already_sent"}, true, nil
	}

	return publishedAtFinalizeEligibility{}, false, nil
}

func isPublishedAtTrackingSent(trackingRow *domain.YouTubeContentAlarmTracking) bool {
	return trackingRow != nil && trackingRow.AlarmSentAt != nil && !trackingRow.AlarmSentAt.IsZero()
}

func isPublishedAtClaimFresh(authorizedAt time.Time) bool {
	if authorizedAt.IsZero() {
		return false
	}

	return time.Since(authorizedAt.UTC()) < publishedAtClaimFreshWindow
}

func (r *publishedAtResolverRepository) outboxExistsForCandidate(
	ctx context.Context,
	tx dbx.Querier,
	txRepository *trackingrepo.PgxRepository,
	candidate trackingrepo.PublishedAtResolutionCandidate,
) (bool, error) {
	trackingRow, err := txRepository.FindByIdentity(ctx, candidate.Kind, candidate.ContentID)
	if err != nil {
		return false, fmt.Errorf("load tracking row: %w", err)
	}
	if trackingRow != nil && trackingRow.AlarmSentAt != nil && !trackingRow.AlarmSentAt.IsZero() {
		return true, nil
	}

	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM youtube_notification_outbox
			WHERE kind = $1 AND content_id = $2
		)`, candidate.Kind, candidate.ContentID).Scan(&exists); err != nil {
		return false, fmt.Errorf("load outbox row: %w", err)
	}

	return exists, nil
}

func selectPublishedAtFinalizeReason(primary string, fallback string) string {
	if primary != "" {
		return primary
	}
	return fallback
}

func (r *publishedAtResolverRepository) ListResolvedPublishedAtDispatchGaps(ctx context.Context, referenceNow time.Time, detectedBefore time.Time, limit int) ([]resolvedPublishedAtDispatchGap, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("list resolved published_at dispatch gaps: db is nil")
	}
	if err := validateResolvedPublishedAtDispatchGapRequest(referenceNow, detectedBefore, limit); err != nil {
		return nil, err
	}

	rows := make([]resolvedPublishedAtDispatchGapRow, 0)
	normalizedReferenceNow := yttimestamp.Normalize(referenceNow)
	if err := pgxscan.Select(ctx, r.db, &rows, `
		SELECT s.kind, s.post_id, s.content_id, s.channel_id, s.detected_at, s.actual_published_at
		FROM youtube_community_shorts_alarm_states AS s
		LEFT JOIN youtube_content_alarm_tracking AS t ON t.kind = s.kind AND t.canonical_content_id = s.post_id
		WHERE s.kind IN ($1, $2)
			AND s.actual_published_at IS NOT NULL
			AND s.alarm_sent_at IS NULL
			AND t.alarm_sent_at IS NULL
			AND s.detected_at < $3
			AND s.actual_published_at >= $4
			AND (s.published_at_retry_after IS NULL OR s.published_at_retry_after <= $5)
			AND NOT EXISTS (
			SELECT 1
			FROM youtube_notification_outbox AS o
			WHERE o.kind = s.kind AND (o.content_id = s.content_id OR o.content_id = s.post_id)
		)
		ORDER BY s.detected_at ASC, s.post_id ASC
		LIMIT $6`,
		domain.OutboxKindCommunityPost,
		domain.OutboxKindNewShort,
		yttimestamp.Normalize(detectedBefore),
		normalizedReferenceNow.Add(-resolvedPublishedAtDispatchGapRecoverFor),
		normalizedReferenceNow,
		limit,
	); err != nil {
		return nil, fmt.Errorf("list resolved published_at dispatch gaps: query rows: %w", err)
	}

	return buildResolvedPublishedAtDispatchGaps(rows), nil
}

func validateResolvedPublishedAtDispatchGapRequest(referenceNow time.Time, detectedBefore time.Time, limit int) error {
	if referenceNow.IsZero() {
		return fmt.Errorf("list resolved published_at dispatch gaps: reference now is empty")
	}
	if detectedBefore.IsZero() {
		return fmt.Errorf("list resolved published_at dispatch gaps: detected before is empty")
	}
	if limit <= 0 {
		return fmt.Errorf("list resolved published_at dispatch gaps: limit must be positive")
	}

	return nil
}

func buildResolvedPublishedAtDispatchGaps(rows []resolvedPublishedAtDispatchGapRow) []resolvedPublishedAtDispatchGap {
	gaps := make([]resolvedPublishedAtDispatchGap, 0, len(rows))
	for i := range rows {
		postID := NormalizeContentID(rows[i].Kind, rows[i].PostID)
		contentID := NormalizeContentID(rows[i].Kind, rows[i].ContentID)
		if postID == "" || contentID == "" {
			continue
		}
		gaps = append(gaps, resolvedPublishedAtDispatchGap{
			candidate: trackingrepo.PublishedAtResolutionCandidate{
				Kind:       rows[i].Kind,
				PostID:     postID,
				ContentID:  contentID,
				ChannelID:  strings.TrimSpace(rows[i].ChannelID),
				DetectedAt: yttimestamp.Normalize(rows[i].DetectedAt),
			},
			publishedAt: yttimestamp.Normalize(rows[i].ActualPublishedAt),
		})
	}

	return gaps
}
