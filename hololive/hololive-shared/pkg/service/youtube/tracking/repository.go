package tracking

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/alarmtiming"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

const alarmLatencyExceededThresholdMillis = alarmtiming.LatencyExceededThresholdMillis

// ReadRepository: 추적 레코드 조회 경로를 담당한다.
type ReadRepository interface {
	FindByIdentity(ctx context.Context, kind domain.OutboxKind, contentID string) (*domain.YouTubeContentAlarmTracking, error)
	ListPendingPublishedAtResolutionsPage(ctx context.Context, detectedBefore time.Time, cursor *PublishedAtResolutionCursor, limit int) ([]PublishedAtResolutionCandidate, *PublishedAtResolutionCursor, error)
	ListPendingPublishedAtResolutions(ctx context.Context, detectedBefore time.Time, limit int) ([]PublishedAtResolutionCandidate, error)
}

// WriteRepository: 추적 레코드 업서트 경로를 담당한다.
type WriteRepository interface {
	Upsert(ctx context.Context, record *domain.YouTubeContentAlarmTracking) error
	UpsertBatch(ctx context.Context, records []*domain.YouTubeContentAlarmTracking) error
	MarkAlarmSentBatch(ctx context.Context, marks []AlarmSentMark) error
}

// Repository: community/shorts 알람 추적 저장소 최소 계약.
type Repository interface {
	ReadRepository
	WriteRepository
}

// GormRepository: GORM 기반 추적 저장소 구현.
type GormRepository struct {
	db *gorm.DB
}

type PublishedAtResolutionCandidate struct {
	Kind       domain.OutboxKind
	PostID     string
	ContentID  string
	ChannelID  string
	DetectedAt time.Time
}

type PublishedAtResolutionCursor struct {
	DetectedAt time.Time
	PostID     string
}

type AlarmSentMark struct {
	Kind         domain.OutboxKind
	ContentID    string
	AlarmSentAt  time.Time
	AuthorizedAt *time.Time
}

func NewRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

func (r *GormRepository) FindByIdentity(ctx context.Context, kind domain.OutboxKind, contentID string) (*domain.YouTubeContentAlarmTracking, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("find tracking by identity: db is nil")
	}

	normalizedKind, normalizedContentID, err := normalizeIdentity(kind, contentID)
	if err != nil {
		return nil, fmt.Errorf("find tracking by identity: %w", err)
	}

	candidates := trackingIdentityCandidates(normalizedKind, normalizedContentID)
	preferredContentID := canonicalTrackingIdentity(normalizedKind, normalizedContentID)

	var records []domain.YouTubeContentAlarmTracking
	query := r.db.WithContext(ctx).Where("kind = ?", normalizedKind)
	if len(candidates) == 1 {
		query = query.Where("(canonical_content_id = ? OR content_id = ?)", preferredContentID, candidates[0])
	} else {
		query = query.Where("(canonical_content_id = ? OR content_id IN ?)", preferredContentID, candidates)
	}
	if err := query.Find(&records).Error; err != nil {
		return nil, fmt.Errorf("find tracking by identity: query row: %w", err)
	}
	if len(records) == 0 {
		return nil, nil
	}
	for i := range records {
		if strings.TrimSpace(records[i].ContentID) == preferredContentID {
			return &records[i], nil
		}
	}
	for i := range records {
		if strings.TrimSpace(records[i].CanonicalContentID) == preferredContentID {
			return &records[i], nil
		}
	}

	return &records[0], nil
}

func (r *GormRepository) Upsert(ctx context.Context, record *domain.YouTubeContentAlarmTracking) error {
	if record == nil {
		return fmt.Errorf("upsert tracking: record is nil")
	}
	return r.UpsertBatch(ctx, []*domain.YouTubeContentAlarmTracking{record})
}

func (r *GormRepository) UpsertBatch(ctx context.Context, records []*domain.YouTubeContentAlarmTracking) error {
	if len(records) == 0 {
		return nil
	}
	if r == nil || r.db == nil {
		return fmt.Errorf("upsert tracking batch: db is nil")
	}

	normalizedByIdentity := make(map[string]*domain.YouTubeContentAlarmTracking, len(records))
	normalizedOrder := make([]string, 0, len(records))
	for i, record := range records {
		normalizedRecord, err := normalizeRecord(record)
		if err != nil {
			return fmt.Errorf("upsert tracking batch: normalize record at index %d: %w", i, err)
		}

		identityKey := trackingCanonicalKey(normalizedRecord.Kind, normalizedRecord.CanonicalContentID)
		if existing, ok := normalizedByIdentity[identityKey]; ok {
			normalizedByIdentity[identityKey] = mergeNormalizedTrackingRecord(existing, normalizedRecord)
			continue
		}

		normalizedByIdentity[identityKey] = normalizedRecord
		normalizedOrder = append(normalizedOrder, identityKey)
	}

	normalized := make([]*domain.YouTubeContentAlarmTracking, 0, len(normalizedOrder))
	for _, identityKey := range normalizedOrder {
		normalized = append(normalized, normalizedByIdentity[identityKey])
	}

	now := yttimestamp.Normalize(time.Now())
	args := make([]any, 0, len(normalized)*12)
	var sb strings.Builder
	sb.WriteString(`
		INSERT INTO youtube_content_alarm_tracking
			(kind, content_id, canonical_content_id, channel_id, actual_published_at, detected_at, alarm_sent_at, alarm_latency_millis, alarm_latency_exceeded, delivery_status, created_at, updated_at)
		VALUES
	`)

	for i, record := range normalized {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString("(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
		args = append(
			args,
			record.Kind,
			record.ContentID,
			record.CanonicalContentID,
			record.ChannelID,
			record.ActualPublishedAt,
			record.DetectedAt,
			record.AlarmSentAt,
			record.AlarmLatencyMillis,
			record.AlarmLatencyExceeded,
			record.DeliveryStatus,
			now,
			now,
		)
	}

	finalActualPublishedExpr := `CASE
		        WHEN EXCLUDED.actual_published_at IS NULL THEN youtube_content_alarm_tracking.actual_published_at
		        ELSE EXCLUDED.actual_published_at
		    END`
	finalAlarmSentExpr := `CASE
		        WHEN youtube_content_alarm_tracking.alarm_sent_at IS NULL THEN EXCLUDED.alarm_sent_at
		        WHEN EXCLUDED.alarm_sent_at IS NULL THEN youtube_content_alarm_tracking.alarm_sent_at
		        WHEN EXCLUDED.alarm_sent_at < youtube_content_alarm_tracking.alarm_sent_at THEN EXCLUDED.alarm_sent_at
		        ELSE youtube_content_alarm_tracking.alarm_sent_at
		    END`
	latencyMillisExpr := buildLatencyMillisExpr(r.db, finalActualPublishedExpr, finalAlarmSentExpr)
	latencyExceededExpr := buildLatencyExceededExpr(latencyMillisExpr)
	deliveryStatusExpr := buildDeliveryStatusExpr(finalAlarmSentExpr)

	sb.WriteString(`
		ON CONFLICT (kind, canonical_content_id) DO UPDATE
		SET channel_id = EXCLUDED.channel_id,
		    actual_published_at = CASE
		        WHEN EXCLUDED.actual_published_at IS NULL THEN youtube_content_alarm_tracking.actual_published_at
		        ELSE EXCLUDED.actual_published_at
		    END,
		    detected_at = CASE
		        WHEN EXCLUDED.detected_at < youtube_content_alarm_tracking.detected_at THEN EXCLUDED.detected_at
		        ELSE youtube_content_alarm_tracking.detected_at
		    END,
		    alarm_sent_at = CASE
		        WHEN youtube_content_alarm_tracking.alarm_sent_at IS NULL THEN EXCLUDED.alarm_sent_at
		        WHEN EXCLUDED.alarm_sent_at IS NULL THEN youtube_content_alarm_tracking.alarm_sent_at
		        WHEN EXCLUDED.alarm_sent_at < youtube_content_alarm_tracking.alarm_sent_at THEN EXCLUDED.alarm_sent_at
		        ELSE youtube_content_alarm_tracking.alarm_sent_at
		    END,
		    alarm_latency_millis = `)
	sb.WriteString(latencyMillisExpr)
	sb.WriteString(`,
		    alarm_latency_exceeded = `)
	sb.WriteString(latencyExceededExpr)
	sb.WriteString(`,
		    delivery_status = `)
	sb.WriteString(deliveryStatusExpr)
	sb.WriteString(`,
		    updated_at = EXCLUDED.updated_at
	`)

	if err := r.db.WithContext(ctx).Exec(sb.String(), args...).Error; err != nil {
		return fmt.Errorf("upsert tracking batch: exec query: %w", err)
	}

	return nil
}

func (r *GormRepository) UpsertSourcePost(ctx context.Context, record *domain.YouTubeCommunityShortsSourcePost) error {
	if record == nil {
		return fmt.Errorf("upsert source post: record is nil")
	}
	return r.UpsertSourcePostsBatch(ctx, []*domain.YouTubeCommunityShortsSourcePost{record})
}

func (r *GormRepository) UpsertSourcePostsBatch(ctx context.Context, records []*domain.YouTubeCommunityShortsSourcePost) error {
	if len(records) == 0 {
		return nil
	}
	if r == nil || r.db == nil {
		return fmt.Errorf("upsert source posts batch: db is nil")
	}

	normalized := make([]*domain.YouTubeCommunityShortsSourcePost, 0, len(records))
	for i, record := range records {
		normalizedRecord, err := normalizeSourcePost(record)
		if err != nil {
			return fmt.Errorf("upsert source posts batch: normalize record at index %d: %w", i, err)
		}
		normalized = append(normalized, normalizedRecord)
	}

	now := yttimestamp.Normalize(time.Now())
	args := make([]any, 0, len(normalized)*7)
	var sb strings.Builder
	sb.WriteString(`
		INSERT INTO youtube_community_shorts_source_posts
			(kind, post_id, channel_id, actual_published_at, detected_at, created_at, updated_at)
		VALUES
	`)

	for i, record := range normalized {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString("(?, ?, ?, ?, ?, ?, ?)")
		args = append(args, record.Kind, record.PostID, record.ChannelID, record.ActualPublishedAt, record.DetectedAt, now, now)
	}

	sb.WriteString(`
		ON CONFLICT (kind, post_id) DO UPDATE
		SET channel_id = EXCLUDED.channel_id,
		    actual_published_at = CASE
		        WHEN EXCLUDED.actual_published_at IS NULL THEN youtube_community_shorts_source_posts.actual_published_at
		        ELSE EXCLUDED.actual_published_at
		    END,
		    detected_at = CASE
		        WHEN EXCLUDED.detected_at < youtube_community_shorts_source_posts.detected_at THEN EXCLUDED.detected_at
		        ELSE youtube_community_shorts_source_posts.detected_at
		    END,
		    updated_at = EXCLUDED.updated_at
	`)

	if err := r.db.WithContext(ctx).Exec(sb.String(), args...).Error; err != nil {
		return fmt.Errorf("upsert source posts batch: exec query: %w", err)
	}

	return nil
}

func (r *GormRepository) ListSourcePostsDetectedWithinWindow(
	ctx context.Context,
	windowStart time.Time,
	windowEnd time.Time,
) ([]domain.YouTubeCommunityShortsSourcePost, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("list source posts within detected window: db is nil")
	}
	if windowStart.IsZero() {
		return nil, fmt.Errorf("list source posts within detected window: window start is empty")
	}
	if windowEnd.IsZero() {
		return nil, fmt.Errorf("list source posts within detected window: window end is empty")
	}

	startUTC := yttimestamp.Normalize(windowStart)
	endUTC := yttimestamp.Normalize(windowEnd)
	if !startUTC.Before(endUTC) {
		return nil, fmt.Errorf("list source posts within detected window: window start must be before window end")
	}

	var rows []domain.YouTubeCommunityShortsSourcePost
	if err := r.db.WithContext(ctx).
		Where("kind IN ?", []domain.OutboxKind{domain.OutboxKindCommunityPost, domain.OutboxKindNewShort}).
		Where("detected_at >= ?", startUTC).
		Where("detected_at < ?", endUTC).
		Order("detected_at DESC").
		Order("post_id ASC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list source posts within detected window: query rows: %w", err)
	}

	return rows, nil
}

func (r *GormRepository) ListSourcePostsWithinObservationWindow(
	ctx context.Context,
	windowStart time.Time,
	windowEnd time.Time,
	detectedBefore time.Time,
) ([]domain.YouTubeCommunityShortsSourcePost, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("list source posts within observation window: db is nil")
	}
	if windowStart.IsZero() {
		return nil, fmt.Errorf("list source posts within observation window: window start is empty")
	}
	if windowEnd.IsZero() {
		return nil, fmt.Errorf("list source posts within observation window: window end is empty")
	}
	if detectedBefore.IsZero() {
		return nil, fmt.Errorf("list source posts within observation window: detected before is empty")
	}

	startUTC := yttimestamp.Normalize(windowStart)
	endUTC := yttimestamp.Normalize(windowEnd)
	detectedBeforeUTC := yttimestamp.Normalize(detectedBefore)
	if !startUTC.Before(endUTC) {
		return nil, fmt.Errorf("list source posts within observation window: window start must be before window end")
	}
	if detectedBeforeUTC.Before(endUTC) {
		return nil, fmt.Errorf("list source posts within observation window: detected before must be on or after window end")
	}

	var rows []domain.YouTubeCommunityShortsSourcePost
	if err := r.db.WithContext(ctx).
		Where("kind IN ?", []domain.OutboxKind{domain.OutboxKindCommunityPost, domain.OutboxKindNewShort}).
		Where("COALESCE(actual_published_at, detected_at) >= ?", startUTC).
		Where("COALESCE(actual_published_at, detected_at) < ?", endUTC).
		Where("detected_at < ?", detectedBeforeUTC).
		Order("COALESCE(actual_published_at, detected_at) DESC").
		Order("detected_at DESC").
		Order("post_id ASC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list source posts within observation window: query rows: %w", err)
	}

	return rows, nil
}

func (r *GormRepository) MarkAlarmSentBatch(ctx context.Context, marks []AlarmSentMark) error {
	if len(marks) == 0 {
		return nil
	}
	if r == nil || r.db == nil {
		return fmt.Errorf("mark alarm sent batch: db is nil")
	}

	normalized, err := normalizeAlarmSentMarks(marks)
	if err != nil {
		return fmt.Errorf("mark alarm sent batch: %w", err)
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txRepo := NewRepository(tx)
		updatedAt := yttimestamp.Normalize(time.Now())
		for i, mark := range normalized {
			if err := txRepo.applyAlarmSentMark(ctx, mark, updatedAt); err != nil {
				return fmt.Errorf("update mark at index %d: %w", i, err)
			}
		}

		return nil
	})
}

func (r *GormRepository) applyAlarmSentMark(ctx context.Context, mark AlarmSentMark, updatedAt time.Time) error {
	trackingRow, err := r.FindByIdentity(ctx, mark.Kind, mark.ContentID)
	if err != nil {
		return fmt.Errorf("load tracking row: %w", err)
	}
	latencyMillis, latencyExceeded := calculateLatencyResult(nil, &mark.AlarmSentAt)
	targetContentID := mark.ContentID
	if trackingRow != nil {
		targetContentID = trackingRow.ContentID
		latencyMillis, latencyExceeded = calculateLatencyResult(trackingRow.ActualPublishedAt, &mark.AlarmSentAt)
	}

	result := r.db.WithContext(ctx).
		Model(&domain.YouTubeContentAlarmTracking{}).
		Where("kind = ? AND content_id = ?", mark.Kind, targetContentID).
		Where("alarm_sent_at IS NULL OR alarm_sent_at > ?", mark.AlarmSentAt).
		Updates(map[string]any{
			"alarm_sent_at":          mark.AlarmSentAt,
			"alarm_latency_millis":   nullableInt64Value(latencyMillis),
			"alarm_latency_exceeded": nullableBoolValue(latencyExceeded),
			"delivery_status":        domain.YouTubeContentAlarmDeliveryStatusSent,
			"updated_at":             updatedAt,
		})
	if result.Error != nil {
		return result.Error
	}

	if !isCommunityShortsAlarmStateKind(mark.Kind) {
		return nil
	}

	postID := canonicalTrackingIdentity(mark.Kind, targetContentID)
	if trackingRow != nil && strings.TrimSpace(trackingRow.CanonicalContentID) != "" {
		postID = strings.TrimSpace(trackingRow.CanonicalContentID)
	}

	return r.applyAlarmStateSentMark(ctx, mark, postID, targetContentID, trackingRow, updatedAt)
}

func (r *GormRepository) applyAlarmStateSentMark(
	ctx context.Context,
	mark AlarmSentMark,
	postID string,
	targetContentID string,
	trackingRow *domain.YouTubeContentAlarmTracking,
	updatedAt time.Time,
) error {
	if mark.AuthorizedAt != nil {
		result := r.db.WithContext(ctx).
			Model(&domain.YouTubeCommunityShortsAlarmState{}).
			Where("kind = ? AND post_id = ?", mark.Kind, postID).
			Where("authorized_at = ?", *mark.AuthorizedAt).
			Where("alarm_sent_at IS NULL").
			Updates(map[string]any{
				"authorized_at":   nil,
				"alarm_sent_at":   mark.AlarmSentAt,
				"delivery_status": domain.YouTubeCommunityShortsAlarmStateStatusSent,
				"updated_at":      updatedAt,
			})
		if result.Error != nil {
			return fmt.Errorf("finalize claimed alarm state: update row: %w", result.Error)
		}
		if result.RowsAffected > 0 {
			return nil
		}
	}

	stateRow, err := r.FindAlarmStateByPostID(ctx, mark.Kind, postID)
	if err != nil {
		return fmt.Errorf("load alarm state row: %w", err)
	}

	if stateRow != nil {
		if stateRow.AlarmSentAt != nil && !stateRow.AlarmSentAt.IsZero() {
			if err := r.updateAlarmStateSentRow(ctx, mark.Kind, postID, mark.AlarmSentAt, updatedAt); err != nil {
				return fmt.Errorf("refresh existing sent alarm state row: %w", err)
			}
			return nil
		}
		if mark.AuthorizedAt != nil {
			return fmt.Errorf("finalize claimed alarm state: claim authorization mismatch")
		}
		if err := r.updateAlarmStateSentRow(ctx, mark.Kind, postID, mark.AlarmSentAt, updatedAt); err != nil {
			return fmt.Errorf("mark existing alarm state row sent: %w", err)
		}
		return nil
	}

	if trackingRow == nil {
		if mark.AuthorizedAt != nil {
			return fmt.Errorf("finalize claimed alarm state: tracking row missing")
		}
		return nil
	}

	alarmSentAt := mark.AlarmSentAt
	if err := r.UpsertAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:              mark.Kind,
		PostID:            postID,
		ContentID:         targetContentID,
		ChannelID:         trackingRow.ChannelID,
		ActualPublishedAt: trackingRow.ActualPublishedAt,
		DetectedAt:        trackingRow.DetectedAt,
		AlarmSentAt:       &alarmSentAt,
	}); err != nil {
		return fmt.Errorf("upsert fallback alarm state row: %w", err)
	}

	return nil
}

func (r *GormRepository) updateAlarmStateSentRow(ctx context.Context, kind domain.OutboxKind, postID string, alarmSentAt time.Time, updatedAt time.Time) error {
	normalizedKind, normalizedPostID, err := normalizeSourcePostIdentity(kind, postID)
	if err != nil {
		return err
	}

	result := r.db.WithContext(ctx).
		Model(&domain.YouTubeCommunityShortsAlarmState{}).
		Where("kind = ? AND post_id = ?", normalizedKind, normalizedPostID).
		Where("alarm_sent_at IS NULL OR alarm_sent_at > ? OR authorized_at IS NOT NULL", alarmSentAt).
		Updates(map[string]any{
			"authorized_at": nil,
			"alarm_sent_at": gorm.Expr(
				"CASE WHEN alarm_sent_at IS NULL OR alarm_sent_at > ? THEN ? ELSE alarm_sent_at END",
				alarmSentAt,
				alarmSentAt,
			),
			"delivery_status": domain.YouTubeCommunityShortsAlarmStateStatusSent,
			"updated_at":      updatedAt,
		})
	if result.Error != nil {
		return result.Error
	}

	return nil
}

func normalizeRecord(record *domain.YouTubeContentAlarmTracking) (*domain.YouTubeContentAlarmTracking, error) {
	if record == nil {
		return nil, fmt.Errorf("record is nil")
	}

	normalizedKind, normalizedContentID, err := normalizeIdentity(record.Kind, record.ContentID)
	if err != nil {
		return nil, err
	}

	normalizedChannelID := strings.TrimSpace(record.ChannelID)
	if normalizedChannelID == "" {
		return nil, fmt.Errorf("channel id is empty")
	}
	if record.DetectedAt.IsZero() {
		return nil, fmt.Errorf("detected_at is empty")
	}

	actualPublishedAt := yttimestamp.NormalizePtr(record.ActualPublishedAt)
	timing := alarmtiming.Build(actualPublishedAt, record.AlarmSentAt)
	actualPublishedAt = timing.ActualPublishedAt
	alarmSentAt := timing.AlarmSentAt
	latencyMillis := timing.AlarmLatencyMillis
	latencyExceeded := timing.AlarmLatencyExceeded
	canonicalContentID := canonicalTrackingIdentity(normalizedKind, normalizedContentID)

	return &domain.YouTubeContentAlarmTracking{
		Kind:                 normalizedKind,
		ContentID:            normalizedContentID,
		CanonicalContentID:   canonicalContentID,
		ChannelID:            normalizedChannelID,
		ActualPublishedAt:    actualPublishedAt,
		DetectedAt:           yttimestamp.Normalize(record.DetectedAt),
		AlarmSentAt:          alarmSentAt,
		AlarmLatencyMillis:   latencyMillis,
		AlarmLatencyExceeded: latencyExceeded,
		DeliveryStatus:       domain.ResolveYouTubeContentAlarmDeliveryStatus(alarmSentAt),
	}, nil
}

func normalizeSourcePost(record *domain.YouTubeCommunityShortsSourcePost) (*domain.YouTubeCommunityShortsSourcePost, error) {
	if record == nil {
		return nil, fmt.Errorf("record is nil")
	}

	normalizedKind, normalizedPostID, err := normalizeSourcePostIdentity(record.Kind, record.PostID)
	if err != nil {
		return nil, err
	}

	normalizedChannelID := strings.TrimSpace(record.ChannelID)
	if normalizedChannelID == "" {
		return nil, fmt.Errorf("channel id is empty")
	}
	if record.DetectedAt.IsZero() {
		return nil, fmt.Errorf("detected_at is empty")
	}

	return &domain.YouTubeCommunityShortsSourcePost{
		Kind:              normalizedKind,
		PostID:            normalizedPostID,
		ChannelID:         normalizedChannelID,
		ActualPublishedAt: yttimestamp.NormalizePtr(record.ActualPublishedAt),
		DetectedAt:        yttimestamp.Normalize(record.DetectedAt),
	}, nil
}

func normalizeSourcePostIdentity(kind domain.OutboxKind, postID string) (domain.OutboxKind, string, error) {
	normalizedKind, normalizedPostID, err := normalizeIdentity(kind, postID)
	if err != nil {
		return "", "", err
	}

	canonicalPostID, err := ytcontentid.ForOutboxKind(normalizedKind, normalizedPostID)
	if err == nil && strings.TrimSpace(canonicalPostID) != "" {
		return normalizedKind, canonicalPostID, nil
	}
	return normalizedKind, strings.TrimSpace(normalizedPostID), nil
}

func buildDeliveryStatusExpr(alarmSentExpr string) string {
	return fmt.Sprintf(`CASE
	        WHEN %s IS NULL THEN '%s'
	        ELSE '%s'
	    END`,
		alarmSentExpr,
		domain.YouTubeContentAlarmDeliveryStatusPending,
		domain.YouTubeContentAlarmDeliveryStatusSent,
	)
}

func normalizeAlarmSentMarks(marks []AlarmSentMark) ([]AlarmSentMark, error) {
	normalized := make([]AlarmSentMark, 0, len(marks))
	indexByIdentity := make(map[string]int, len(marks))

	for i, mark := range marks {
		normalizedKind, normalizedContentID, err := normalizeIdentity(mark.Kind, mark.ContentID)
		if err != nil {
			return nil, fmt.Errorf("normalize mark at index %d: %w", i, err)
		}
		if mark.AlarmSentAt.IsZero() {
			return nil, fmt.Errorf("normalize mark at index %d: alarm sent at is empty", i)
		}

		var normalizedAuthorizedAt *time.Time
		if mark.AuthorizedAt != nil {
			if mark.AuthorizedAt.IsZero() {
				return nil, fmt.Errorf("normalize mark at index %d: authorized at is empty", i)
			}
			authorizedAt := yttimestamp.Normalize(*mark.AuthorizedAt)
			normalizedAuthorizedAt = &authorizedAt
		}

		normalizedAlarmSentAt := yttimestamp.Normalize(mark.AlarmSentAt)
		identity := string(normalizedKind) + "\x00" + canonicalTrackingIdentity(normalizedKind, normalizedContentID)
		if existingIndex, ok := indexByIdentity[identity]; ok {
			if normalizedAuthorizedAt != nil {
				existingAuthorizedAt := normalized[existingIndex].AuthorizedAt
				switch {
				case existingAuthorizedAt == nil:
					normalized[existingIndex].AuthorizedAt = normalizedAuthorizedAt
				case !existingAuthorizedAt.UTC().Equal(normalizedAuthorizedAt.UTC()):
					return nil, fmt.Errorf("normalize mark at index %d: conflicting authorized_at for %s", i, identity)
				}
			}
			if normalizedAlarmSentAt.Before(normalized[existingIndex].AlarmSentAt) {
				normalized[existingIndex].AlarmSentAt = normalizedAlarmSentAt
			}
			continue
		}

		indexByIdentity[identity] = len(normalized)
		normalized = append(normalized, AlarmSentMark{
			Kind:         normalizedKind,
			ContentID:    normalizedContentID,
			AlarmSentAt:  normalizedAlarmSentAt,
			AuthorizedAt: normalizedAuthorizedAt,
		})
	}

	return normalized, nil
}

func isCommunityShortsAlarmStateKind(kind domain.OutboxKind) bool {
	switch kind {
	case domain.OutboxKindCommunityPost, domain.OutboxKindNewShort:
		return true
	default:
		return false
	}
}

func trackingCanonicalKey(kind domain.OutboxKind, canonicalContentID string) string {
	return string(kind) + "\x00" + strings.TrimSpace(canonicalContentID)
}

func mergeNormalizedTrackingRecord(existing *domain.YouTubeContentAlarmTracking, next *domain.YouTubeContentAlarmTracking) *domain.YouTubeContentAlarmTracking {
	if existing == nil {
		return next
	}
	if next == nil {
		return existing
	}

	merged := *existing
	if strings.TrimSpace(next.ChannelID) != "" {
		merged.ChannelID = next.ChannelID
	}
	if next.ActualPublishedAt != nil {
		merged.ActualPublishedAt = next.ActualPublishedAt
	}
	if next.DetectedAt.Before(merged.DetectedAt) {
		merged.DetectedAt = next.DetectedAt
	}
	switch {
	case merged.AlarmSentAt == nil:
		merged.AlarmSentAt = next.AlarmSentAt
	case next.AlarmSentAt != nil && next.AlarmSentAt.Before(*merged.AlarmSentAt):
		merged.AlarmSentAt = next.AlarmSentAt
	}

	timing := alarmtiming.Build(merged.ActualPublishedAt, merged.AlarmSentAt)
	merged.ActualPublishedAt = timing.ActualPublishedAt
	merged.AlarmSentAt = timing.AlarmSentAt
	merged.AlarmLatencyMillis = timing.AlarmLatencyMillis
	merged.AlarmLatencyExceeded = timing.AlarmLatencyExceeded
	merged.DeliveryStatus = domain.ResolveYouTubeContentAlarmDeliveryStatus(merged.AlarmSentAt)

	return &merged
}

func normalizeIdentity(kind domain.OutboxKind, contentID string) (domain.OutboxKind, string, error) {
	normalizedContentID := strings.TrimSpace(contentID)
	if normalizedContentID == "" {
		return "", "", fmt.Errorf("content id is empty")
	}

	switch kind {
	case domain.OutboxKindNewShort, domain.OutboxKindCommunityPost:
		return kind, normalizedContentID, nil
	default:
		return "", "", fmt.Errorf("unsupported tracking kind: %s", kind)
	}
}

func trackingIdentityCandidates(kind domain.OutboxKind, contentID string) []string {
	normalizedContentID := strings.TrimSpace(contentID)
	switch kind {
	case domain.OutboxKindNewShort:
		canonicalContentID := canonicalTrackingIdentity(kind, normalizedContentID)
		rawContentID, err := ytcontentid.NormalizeShortVideoID(normalizedContentID)
		if err != nil || strings.TrimSpace(rawContentID) == "" {
			return []string{canonicalContentID}
		}
		if canonicalContentID == rawContentID {
			return []string{canonicalContentID}
		}

		return []string{canonicalContentID, rawContentID}
	case domain.OutboxKindCommunityPost:
		canonicalContentID := canonicalTrackingIdentity(kind, normalizedContentID)
		rawContentID, err := ytcontentid.NormalizeCommunityPostID(normalizedContentID)
		if err != nil || strings.TrimSpace(rawContentID) == "" {
			return []string{canonicalContentID}
		}
		if canonicalContentID == rawContentID {
			return []string{canonicalContentID}
		}

		return []string{canonicalContentID, rawContentID}
	default:
		return []string{normalizedContentID}
	}
}

func canonicalTrackingIdentity(kind domain.OutboxKind, contentID string) string {
	normalizedContentID := strings.TrimSpace(contentID)
	canonicalContentID, err := ytcontentid.ForOutboxKind(kind, normalizedContentID)
	if err != nil {
		return normalizedContentID
	}
	return canonicalContentID
}

func buildLatencyMillisExpr(db *gorm.DB, startExpr string, endExpr string) string {
	switch dialectName(db) {
	case "sqlite":
		return fmt.Sprintf(`CASE
		        WHEN (%s) IS NULL OR (%s) IS NULL THEN NULL
		        ELSE CAST(ROUND((julianday((%s)) - julianday((%s))) * 86400000.0) AS INTEGER)
		    END`, startExpr, endExpr, endExpr, startExpr)
	default:
		return fmt.Sprintf(`CASE
		        WHEN (%s) IS NULL OR (%s) IS NULL THEN NULL
		        ELSE CAST(ROUND(EXTRACT(EPOCH FROM (((%s)) - ((%s)))) * 1000) AS BIGINT)
		    END`, startExpr, endExpr, endExpr, startExpr)
	}
}

func buildLatencyExceededExpr(latencyMillisExpr string) string {
	return fmt.Sprintf(`CASE
		        WHEN (%s) IS NULL THEN NULL
		        WHEN (%s) > %d THEN TRUE
		        ELSE FALSE
		    END`, latencyMillisExpr, latencyMillisExpr, alarmLatencyExceededThresholdMillis)
}

func calculateLatencyResult(start *time.Time, end *time.Time) (*int64, *bool) {
	return alarmtiming.CalculateLatency(start, end)
}

func nullableInt64Value(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableBoolValue(value *bool) any {
	if value == nil {
		return nil
	}
	return *value
}

func dialectName(db *gorm.DB) string {
	if db == nil || db.Dialector == nil {
		return ""
	}
	return db.Dialector.Name()
}
