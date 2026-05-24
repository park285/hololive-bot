package observation

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
)

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
