package observation

import (
	"context"
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
	return r.alarm.FindAlarmStateByPostID(ctx, kind, postID)
}

func (r *PgxRepository) UpsertAlarmState(ctx context.Context, record *domain.YouTubeCommunityShortsAlarmState) error {
	return r.alarm.UpsertAlarmState(ctx, record)
}

func (r *PgxRepository) UpsertAlarmStateBatch(ctx context.Context, records []*domain.YouTubeCommunityShortsAlarmState) error {
	return r.alarm.UpsertAlarmStateBatch(ctx, records)
}

func (r *PgxRepository) TryClaimAlarmState(ctx context.Context, record *domain.YouTubeCommunityShortsAlarmState) (bool, error) {
	return r.alarm.TryClaimAlarmState(ctx, record)
}

func (r *PgxRepository) ReleaseAlarmStateClaim(ctx context.Context, kind domain.OutboxKind, postID string, authorizedAt time.Time) (bool, error) {
	return r.alarm.ReleaseAlarmStateClaim(ctx, kind, postID, authorizedAt)
}

// --- 위임: DeliveryState ---

func (r *PgxRepository) MarkAlarmSentBatch(ctx context.Context, marks []AlarmSentMark) error {
	return r.delivery.MarkAlarmSentBatch(ctx, marks)
}

// --- 위임: Identity ---

func (r *PgxRepository) FindByIdentity(ctx context.Context, kind domain.OutboxKind, contentID string) (*domain.YouTubeContentAlarmTracking, error) {
	return r.identity.FindByIdentity(ctx, kind, contentID)
}

func (r *PgxRepository) Upsert(ctx context.Context, record *domain.YouTubeContentAlarmTracking) error {
	return r.identity.Upsert(ctx, record)
}

func (r *PgxRepository) UpsertBatch(ctx context.Context, records []*domain.YouTubeContentAlarmTracking) error {
	return r.identity.UpsertBatch(ctx, records)
}

// --- 위임: SourcePost ---

func (r *PgxRepository) UpsertSourcePost(ctx context.Context, record *domain.YouTubeCommunityShortsSourcePost) error {
	return r.source.UpsertSourcePost(ctx, record)
}

func (r *PgxRepository) UpsertSourcePostsBatch(ctx context.Context, records []*domain.YouTubeCommunityShortsSourcePost) error {
	return r.source.UpsertSourcePostsBatch(ctx, records)
}
