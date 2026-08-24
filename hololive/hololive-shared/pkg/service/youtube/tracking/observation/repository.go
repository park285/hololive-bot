package observation

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type ReadRepository interface {
	FindByIdentity(ctx context.Context, kind domain.OutboxKind, contentID string) (*domain.YouTubeContentAlarmTracking, error)
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
	db trackingDB
}

type deliveryStateRepository struct {
	db    trackingDB
	owner *PgxRepository
}

type identityRepository struct {
	db trackingDB
}

type sourcePostRepository struct {
	db trackingDB
}

type trackingDB interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type trackingTxBeginner interface {
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

type PgxRepository struct {
	db trackingDB

	alarm    *alarmStateRepository
	delivery *deliveryStateRepository
	identity *identityRepository
	source   *sourcePostRepository
}

type AlarmSentMark struct {
	Kind         domain.OutboxKind
	ContentID    string
	AlarmSentAt  time.Time
	AuthorizedAt *time.Time
}

func NewRepository(db trackingDB) *PgxRepository {
	return NewRepositoryContext(context.Background(), db)
}

func NewRepositoryContext(_ context.Context, db trackingDB) *PgxRepository {
	repo := &PgxRepository{
		db: db,
		alarm: &alarmStateRepository{
			db: db,
		},
		identity: &identityRepository{db: db},
		source:   &sourcePostRepository{db: db},
	}

	repo.delivery = &deliveryStateRepository{db: db, owner: repo}

	return repo
}

// --- 위임: AlarmState ---

func (r *PgxRepository) FindAlarmStateByPostID(ctx context.Context, kind domain.OutboxKind, postID string) (*domain.YouTubeCommunityShortsAlarmState, error) {
	out, err := r.alarm.FindAlarmStateByPostID(ctx, kind, postID)
	if err != nil {
		return nil, fmt.Errorf("find alarm state by post ID: %w", err)
	}

	return out, nil
}

func (r *PgxRepository) UpsertAlarmState(ctx context.Context, record *domain.YouTubeCommunityShortsAlarmState) error {
	if err := r.alarm.UpsertAlarmState(ctx, record); err != nil {
		return fmt.Errorf("upsert alarm state: %w", err)
	}

	return nil
}

func (r *PgxRepository) UpsertAlarmStateBatch(ctx context.Context, records []*domain.YouTubeCommunityShortsAlarmState) error {
	if err := r.alarm.UpsertAlarmStateBatch(ctx, records); err != nil {
		return fmt.Errorf("upsert alarm state batch: %w", err)
	}

	return nil
}

func (r *PgxRepository) TryClaimAlarmState(ctx context.Context, record *domain.YouTubeCommunityShortsAlarmState) (bool, error) {
	out, err := r.alarm.TryClaimAlarmState(ctx, record)
	if err != nil {
		return out, fmt.Errorf("try claim alarm state: %w", err)
	}

	return out, nil
}

func (r *PgxRepository) ReleaseAlarmStateClaim(ctx context.Context, kind domain.OutboxKind, postID string, authorizedAt time.Time) (bool, error) {
	out, err := r.alarm.ReleaseAlarmStateClaim(ctx, kind, postID, authorizedAt)
	if err != nil {
		return out, fmt.Errorf("release alarm state claim: %w", err)
	}

	return out, nil
}

// --- 위임: DeliveryState ---

func (r *PgxRepository) MarkAlarmSentBatch(ctx context.Context, marks []AlarmSentMark) error {
	if err := r.delivery.MarkAlarmSentBatch(ctx, marks); err != nil {
		return fmt.Errorf("mark alarm sent batch: %w", err)
	}

	return nil
}

// --- 위임: Identity ---

func (r *PgxRepository) FindByIdentity(ctx context.Context, kind domain.OutboxKind, contentID string) (*domain.YouTubeContentAlarmTracking, error) {
	out, err := r.identity.FindByIdentity(ctx, kind, contentID)
	if err != nil {
		return nil, fmt.Errorf("find by identity: %w", err)
	}

	return out, nil
}

func (r *PgxRepository) Upsert(ctx context.Context, record *domain.YouTubeContentAlarmTracking) error {
	if err := r.identity.Upsert(ctx, record); err != nil {
		return fmt.Errorf("upsert: %w", err)
	}

	return nil
}

func (r *PgxRepository) UpsertBatch(ctx context.Context, records []*domain.YouTubeContentAlarmTracking) error {
	if err := r.identity.UpsertBatch(ctx, records); err != nil {
		return fmt.Errorf("upsert batch: %w", err)
	}

	return nil
}

// --- 위임: SourcePost ---

func (r *PgxRepository) UpsertSourcePost(ctx context.Context, record *domain.YouTubeCommunityShortsSourcePost) error {
	if err := r.source.UpsertSourcePost(ctx, record); err != nil {
		return fmt.Errorf("upsert source post: %w", err)
	}

	return nil
}

func (r *PgxRepository) UpsertSourcePostsBatch(ctx context.Context, records []*domain.YouTubeCommunityShortsSourcePost) error {
	if err := r.source.UpsertSourcePostsBatch(ctx, records); err != nil {
		return fmt.Errorf("upsert source posts batch: %w", err)
	}

	return nil
}
