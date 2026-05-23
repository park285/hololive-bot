package observation

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

type ReadRepository interface {
	FindByIdentity(ctx context.Context, kind domain.OutboxKind, contentID string) (*domain.YouTubeContentAlarmTracking, error)
	ListPendingPublishedAtResolutionsPage(ctx context.Context, referenceNow time.Time, detectedBefore time.Time, cursor *PublishedAtResolutionCursor, limit int) ([]PublishedAtResolutionCandidate, *PublishedAtResolutionCursor, error)
	ListPendingPublishedAtResolutions(ctx context.Context, detectedBefore time.Time, limit int) ([]PublishedAtResolutionCandidate, error)
}

type WriteRepository interface {
	Upsert(ctx context.Context, record *domain.YouTubeContentAlarmTracking) error
	UpsertBatch(ctx context.Context, records []*domain.YouTubeContentAlarmTracking) error
	MarkAlarmSentBatch(ctx context.Context, marks []AlarmSentMark) error
}

type Repository interface {
	ReadRepository
	WriteRepository
}

type alarmStateRepository struct {
	db                       *gorm.DB
	hasPublishedAtRetryAfter bool
}

type windowRepository struct {
	db    *gorm.DB
	owner *GormRepository
}

type baselineRepository struct {
	db    *gorm.DB
	owner *GormRepository
}

type historyRepository struct {
	db *gorm.DB
}

type deliveryStateRepository struct {
	db    *gorm.DB
	owner *GormRepository
}

type identityRepository struct {
	db *gorm.DB
}

type sourcePostRepository struct {
	db *gorm.DB
}

type compareMetadataRepository struct {
	db *gorm.DB
}

type GormRepository struct {
	db                       *gorm.DB
	hasPublishedAtRetryAfter bool

	alarm           *alarmStateRepository
	window          *windowRepository
	baseline        *baselineRepository
	history         *historyRepository
	delivery        *deliveryStateRepository
	identity        *identityRepository
	source          *sourcePostRepository
	compareMetadata *compareMetadataRepository
}

type PublishedAtResolutionCandidate struct {
	Kind       domain.OutboxKind
	PostID     string
	ContentID  string
	ChannelID  string
	DetectedAt time.Time
}

type PublishedAtResolutionCursor struct {
	PriorityBucket int
	DetectedAt     time.Time
	PostID         string
}

type AlarmSentMark struct {
	Kind         domain.OutboxKind
	ContentID    string
	AlarmSentAt  time.Time
	AuthorizedAt *time.Time
}

func NewRepository(db *gorm.DB) *GormRepository {
	hasRetryAfter := hasPublishedAtRetryAfterColumn(db)
	repo := &GormRepository{
		db:                       db,
		hasPublishedAtRetryAfter: hasRetryAfter,
		alarm: &alarmStateRepository{
			db:                       db,
			hasPublishedAtRetryAfter: hasRetryAfter,
		},
		history:         &historyRepository{db: db},
		identity:        &identityRepository{db: db},
		source:          &sourcePostRepository{db: db},
		compareMetadata: &compareMetadataRepository{db: db},
	}
	repo.window = &windowRepository{db: db, owner: repo}
	repo.baseline = &baselineRepository{db: db, owner: repo}
	repo.delivery = &deliveryStateRepository{db: db, owner: repo}
	return repo
}

// --- delegation: AlarmState ---

func (r *GormRepository) ListPendingPublishedAtResolutions(ctx context.Context, detectedBefore time.Time, limit int) ([]PublishedAtResolutionCandidate, error) {
	return r.alarm.ListPendingPublishedAtResolutions(ctx, detectedBefore, limit)
}

func (r *GormRepository) ListPendingPublishedAtResolutionsPage(ctx context.Context, referenceNow time.Time, detectedBefore time.Time, cursor *PublishedAtResolutionCursor, limit int) ([]PublishedAtResolutionCandidate, *PublishedAtResolutionCursor, error) {
	return r.alarm.ListPendingPublishedAtResolutionsPage(ctx, referenceNow, detectedBefore, cursor, limit)
}

func (r *GormRepository) MarkPublishedAtRetryAfter(ctx context.Context, kind domain.OutboxKind, postID string, retryAfter time.Time) error {
	return r.alarm.MarkPublishedAtRetryAfter(ctx, kind, postID, retryAfter)
}

func (r *GormRepository) ClearPublishedAtRetryAfter(ctx context.Context, kind domain.OutboxKind, postID string) error {
	return r.alarm.ClearPublishedAtRetryAfter(ctx, kind, postID)
}

func (r *GormRepository) FindAlarmStateByPostID(ctx context.Context, kind domain.OutboxKind, postID string) (*domain.YouTubeCommunityShortsAlarmState, error) {
	return r.alarm.FindAlarmStateByPostID(ctx, kind, postID)
}

func (r *GormRepository) UpsertAlarmState(ctx context.Context, record *domain.YouTubeCommunityShortsAlarmState) error {
	return r.alarm.UpsertAlarmState(ctx, record)
}

func (r *GormRepository) UpsertAlarmStateBatch(ctx context.Context, records []*domain.YouTubeCommunityShortsAlarmState) error {
	return r.alarm.UpsertAlarmStateBatch(ctx, records)
}

func (r *GormRepository) TryClaimAlarmState(ctx context.Context, record *domain.YouTubeCommunityShortsAlarmState) (bool, error) {
	return r.alarm.TryClaimAlarmState(ctx, record)
}

func (r *GormRepository) ReleaseAlarmStateClaim(ctx context.Context, kind domain.OutboxKind, postID string, authorizedAt time.Time) (bool, error) {
	return r.alarm.ReleaseAlarmStateClaim(ctx, kind, postID, authorizedAt)
}

// --- delegation: Window ---

func (r *GormRepository) FindCommunityShortsObservationWindow(ctx context.Context, runtimeName string, bigBangCutoverAt time.Time) (*domain.YouTubeCommunityShortsObservationWindow, error) {
	return r.window.FindCommunityShortsObservationWindow(ctx, runtimeName, bigBangCutoverAt)
}

func (r *GormRepository) FindClosedCommunityShortsObservationWindow(ctx context.Context, runtimeName string, bigBangCutoverAt time.Time, now time.Time) (*domain.YouTubeCommunityShortsObservationWindow, error) {
	return r.window.FindClosedCommunityShortsObservationWindow(ctx, runtimeName, bigBangCutoverAt, now)
}

func (r *GormRepository) EnsureCommunityShortsObservationWindow(ctx context.Context, window *domain.YouTubeCommunityShortsObservationWindow) error {
	return r.window.EnsureCommunityShortsObservationWindow(ctx, window)
}

// --- delegation: Baseline ---

func (r *GormRepository) ListCommunityShortsObservationPostBaselines(ctx context.Context, runtimeName string, bigBangCutoverAt time.Time) ([]domain.YouTubeCommunityShortsObservationPostBaseline, error) {
	return r.baseline.ListCommunityShortsObservationPostBaselines(ctx, runtimeName, bigBangCutoverAt)
}

// --- delegation: History ---

func (r *GormRepository) ListCommunityAlarmSentHistoriesByFinalizedObservationWindow(ctx context.Context, runtimeName string, bigBangCutoverAt time.Time) ([]CommunityAlarmSentHistoryRow, error) {
	return r.history.ListCommunityAlarmSentHistoriesByFinalizedObservationWindow(ctx, runtimeName, bigBangCutoverAt)
}

func (r *GormRepository) ListShortsAlarmSentHistoriesByFinalizedObservationWindow(ctx context.Context, runtimeName string, bigBangCutoverAt time.Time) ([]ShortsAlarmSentHistoryRow, error) {
	return r.history.ListShortsAlarmSentHistoriesByFinalizedObservationWindow(ctx, runtimeName, bigBangCutoverAt)
}

func (r *GormRepository) ListCommunityAlarmSentHistoriesWithinObservationWindow(ctx context.Context, windowStart, windowEnd, detectedBefore time.Time) ([]CommunityAlarmSentHistoryRow, error) {
	return r.history.ListCommunityAlarmSentHistoriesWithinObservationWindow(ctx, windowStart, windowEnd, detectedBefore)
}

func (r *GormRepository) ListShortsAlarmSentHistoriesWithinObservationWindow(ctx context.Context, windowStart, windowEnd, detectedBefore time.Time) ([]ShortsAlarmSentHistoryRow, error) {
	return r.history.ListShortsAlarmSentHistoriesWithinObservationWindow(ctx, windowStart, windowEnd, detectedBefore)
}

// --- delegation: DeliveryState ---

func (r *GormRepository) MarkAlarmSentBatch(ctx context.Context, marks []AlarmSentMark) error {
	return r.delivery.MarkAlarmSentBatch(ctx, marks)
}

// --- delegation: Identity ---

func (r *GormRepository) FindByIdentity(ctx context.Context, kind domain.OutboxKind, contentID string) (*domain.YouTubeContentAlarmTracking, error) {
	return r.identity.FindByIdentity(ctx, kind, contentID)
}

func (r *GormRepository) Upsert(ctx context.Context, record *domain.YouTubeContentAlarmTracking) error {
	return r.identity.Upsert(ctx, record)
}

func (r *GormRepository) UpsertBatch(ctx context.Context, records []*domain.YouTubeContentAlarmTracking) error {
	return r.identity.UpsertBatch(ctx, records)
}

// --- delegation: SourcePost ---

func (r *GormRepository) UpsertSourcePost(ctx context.Context, record *domain.YouTubeCommunityShortsSourcePost) error {
	return r.source.UpsertSourcePost(ctx, record)
}

func (r *GormRepository) UpsertSourcePostsBatch(ctx context.Context, records []*domain.YouTubeCommunityShortsSourcePost) error {
	return r.source.UpsertSourcePostsBatch(ctx, records)
}

func (r *GormRepository) ListSourcePostsDetectedWithinWindow(ctx context.Context, windowStart, windowEnd time.Time) ([]domain.YouTubeCommunityShortsSourcePost, error) {
	return r.source.ListSourcePostsDetectedWithinWindow(ctx, windowStart, windowEnd)
}

func (r *GormRepository) ListSourcePostsWithinObservationWindow(ctx context.Context, windowStart, windowEnd, detectedBefore time.Time) ([]domain.YouTubeCommunityShortsSourcePost, error) {
	return r.source.ListSourcePostsWithinObservationWindow(ctx, windowStart, windowEnd, detectedBefore)
}

// --- delegation: CompareMetadata ---

func (r *GormRepository) EnrichObservationPostComparisonInputs(ctx context.Context, inputs []ObservationPostComparisonInput) ([]ObservationPostComparisonInput, error) {
	return r.compareMetadata.EnrichObservationPostComparisonInputs(ctx, inputs)
}

// --- shared helpers ---

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
	mergeTrackingRecordFields(&merged, next)

	return normalizeMergedTrackingRecord(merged)
}

func mergeTrackingRecordFields(merged *domain.YouTubeContentAlarmTracking, next *domain.YouTubeContentAlarmTracking) {
	if strings.TrimSpace(next.ChannelID) != "" {
		merged.ChannelID = next.ChannelID
	}
	if next.ActualPublishedAt != nil {
		merged.ActualPublishedAt = next.ActualPublishedAt
	}
	if next.DetectedAt.Before(merged.DetectedAt) {
		merged.DetectedAt = next.DetectedAt
	}

	mergeTrackingAlarmSentAt(merged, next.AlarmSentAt)
}

func mergeTrackingAlarmSentAt(merged *domain.YouTubeContentAlarmTracking, nextAlarmSentAt *time.Time) {
	switch {
	case merged.AlarmSentAt == nil:
		merged.AlarmSentAt = nextAlarmSentAt
	case nextAlarmSentAt != nil && nextAlarmSentAt.Before(*merged.AlarmSentAt):
		merged.AlarmSentAt = nextAlarmSentAt
	}
}

func normalizeMergedTrackingRecord(merged domain.YouTubeContentAlarmTracking) *domain.YouTubeContentAlarmTracking {
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
		return trackingIdentityCandidatePair(canonicalContentID, rawContentID, err)
	case domain.OutboxKindCommunityPost:
		canonicalContentID := canonicalTrackingIdentity(kind, normalizedContentID)
		rawContentID, err := ytcontentid.NormalizeCommunityPostID(normalizedContentID)
		return trackingIdentityCandidatePair(canonicalContentID, rawContentID, err)
	default:
		return []string{normalizedContentID}
	}
}

func trackingIdentityCandidatePair(canonicalContentID string, rawContentID string, err error) []string {
	if err != nil || strings.TrimSpace(rawContentID) == "" {
		return []string{canonicalContentID}
	}
	if canonicalContentID == rawContentID {
		return []string{canonicalContentID}
	}

	return []string{canonicalContentID, rawContentID}
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

func dialectName(db *gorm.DB) string {
	if db == nil || db.Dialector == nil {
		return ""
	}
	return db.Name()
}
