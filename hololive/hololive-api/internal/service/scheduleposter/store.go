package scheduleposter

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/dbx"
)

type ReviewStatus string

const (
	ReviewPending   ReviewStatus = "pending_review"
	ReviewConfirmed ReviewStatus = "confirmed"
	ReviewRejected  ReviewStatus = "rejected"
	ReviewExpired   ReviewStatus = "expired"
)

func (s ReviewStatus) Valid() bool {
	switch s {
	case ReviewPending, ReviewConfirmed, ReviewRejected, ReviewExpired:
		return true
	default:
		return false
	}
}

type Reviewer string

const (
	ReviewerAuto  Reviewer = "auto"
	ReviewerAdmin Reviewer = "admin"
)

type ReviewRequestReason string

const (
	ReviewReasonUncertain       ReviewRequestReason = "uncertain"
	ReviewReasonUnjudgeable     ReviewRequestReason = "unjudgeable"
	ReviewReasonImageChanged    ReviewRequestReason = "image_changed"
	ReviewReasonPeriodUnread    ReviewRequestReason = "period_unread"
	ReviewReasonWeekRejected    ReviewRequestReason = "week_rejected"
	ReviewReasonWeekOverlapping ReviewRequestReason = "week_overlapping"
	ReviewReasonStale           ReviewRequestReason = "stale"
	ReviewReasonAutoReviewOff   ReviewRequestReason = "auto_review_off"
)

const (
	maxListLimit = 100
	maxWeekDays  = 6
)

var (
	ErrNotFound         = errors.New("schedule poster not found")
	ErrRevisionConflict = errors.New("schedule poster revision conflict")
	ErrInvalidState     = errors.New("schedule poster state does not allow this change")
	ErrInvalidWeek      = errors.New("schedule poster week is out of range")
)

type Candidate struct {
	ID          int64
	ChannelID   string
	XUserID     string
	PostURL     string
	ImageURL    string
	TextExcerpt string
	PostedAt    time.Time
}

type RowsStatus string

const (
	RowsPending   RowsStatus = "pending"
	RowsSucceeded RowsStatus = "succeeded"
	RowsFailed    RowsStatus = "failed"
)

type Poster struct {
	Candidate

	ReviewStatus        ReviewStatus
	WeekStart           *time.Time
	WeekEnd             *time.Time
	ReviewedAt          *time.Time
	ReviewedBy          Reviewer
	ReviewRequestReason ReviewRequestReason
	Revision            int64
	CreatedAt           time.Time
	UpdatedAt           time.Time
	RowsStatus          RowsStatus
	RowsError           string
	RowsExtractedAt     *time.Time
	RowsCount           int
}

type ReviewCandidate struct {
	ID          int64
	ChannelID   string
	ImageURL    string
	ImageSHA256 string
	PostedAt    time.Time
	Revision    int64
}

type WeekCoverage int

const (
	WeekUncovered WeekCoverage = iota
	WeekSame
	WeekOverlapping
)

type Summary struct {
	PendingCount      int64
	LatestCandidateID int64
}

type Store struct {
	pool *pgxpool.Pool
}

type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func NewStore(pool *pgxpool.Pool) (*Store, error) {
	if pool == nil {
		return nil, errors.New("schedule poster store requires database pool")
	}

	return &Store{pool: pool}, nil
}

func ValidateWeek(start, end time.Time) error {
	days := int(end.Sub(start).Hours() / 24)
	if end.Before(start) || days > maxWeekDays {
		return ErrInvalidWeek
	}

	return nil
}

func (s *Store) InsertCandidates(ctx context.Context, candidates []Candidate, autoReview bool) (int64, error) {
	if len(candidates) == 0 {
		return 0, nil
	}

	ids := make([]int64, len(candidates))
	channels := make([]string, len(candidates))
	users := make([]string, len(candidates))
	posts := make([]string, len(candidates))
	images := make([]string, len(candidates))
	postedAt := make([]time.Time, len(candidates))
	excerpts := make([]string, len(candidates))

	for i, candidate := range candidates {
		ids[i] = candidate.ID
		channels[i] = candidate.ChannelID
		users[i] = candidate.XUserID
		posts[i] = candidate.PostURL
		images[i] = candidate.ImageURL
		postedAt[i] = candidate.PostedAt
		excerpts[i] = candidate.TextExcerpt
	}

	tag, err := s.pool.Exec(ctx, insertCandidatesSQL, ids, channels, users, posts, images, postedAt, excerpts, autoReview)
	if err != nil {
		return 0, fmt.Errorf("insert schedule poster candidates: %w", err)
	}

	return tag.RowsAffected(), nil
}

func (s *Store) InsertCommunityCandidates(ctx context.Context, channelIDs []string, since time.Time, textPattern string, autoReview bool) (int64, error) {
	if len(channelIDs) == 0 {
		return 0, nil
	}

	tag, err := s.pool.Exec(ctx, insertCommunityCandidatesSQL, channelIDs, since, textPattern, autoReview)
	if err != nil {
		return 0, fmt.Errorf("insert schedule poster community candidates: %w", err)
	}

	return tag.RowsAffected(), nil
}

type ThumbnailCandidate struct {
	ChannelID string
	VideoID   string
	Image     Image
	PostedAt  time.Time
}

func (s *Store) InsertThumbnailCandidates(ctx context.Context, candidates []ThumbnailCandidate, autoReview bool) (int64, error) {
	if len(candidates) == 0 {
		return 0, nil
	}

	channels := make([]string, len(candidates))
	videos := make([]string, len(candidates))
	data := make([][]byte, len(candidates))
	mimes := make([]string, len(candidates))
	postedAt := make([]time.Time, len(candidates))

	for i, candidate := range candidates {
		channels[i] = candidate.ChannelID
		videos[i] = candidate.VideoID
		data[i] = candidate.Image.Data
		mimes[i] = candidate.Image.MIME
		postedAt[i] = candidate.PostedAt
	}

	tag, err := s.pool.Exec(ctx, insertThumbnailCandidatesSQL, channels, videos, data, mimes, postedAt, autoReview)
	if err != nil {
		return 0, fmt.Errorf("insert schedule poster thumbnail candidates: %w", err)
	}

	return tag.RowsAffected(), nil
}

func (s *Store) UnreviewedCandidates(ctx context.Context, limit int) ([]ReviewCandidate, error) {
	rows, err := s.pool.Query(ctx, unreviewedCandidatesSQL, min(max(limit, 1), maxListLimit))
	if err != nil {
		return nil, fmt.Errorf("list unreviewed schedule poster candidates: %w", err)
	}

	candidates, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ReviewCandidate, error) {
		var item ReviewCandidate

		return item, row.Scan(&item.ID, &item.ChannelID, &item.ImageURL, &item.ImageSHA256, &item.PostedAt, &item.Revision)
	})
	if err != nil {
		return nil, fmt.Errorf("list unreviewed schedule poster candidates: %w", err)
	}

	return candidates, nil
}

func (s *Store) RequestReview(ctx context.Context, id int64, reason ReviewRequestReason) (bool, error) {
	tag, err := s.pool.Exec(ctx, requestReviewSQL, id, reason)
	if err != nil {
		return false, fmt.Errorf("request schedule poster review: %w", err)
	}

	return tag.RowsAffected() == 1, nil
}

func (s *Store) HandOverUnreviewed(ctx context.Context, createdBefore time.Time, reason ReviewRequestReason) (int64, error) {
	tag, err := s.pool.Exec(ctx, handOverUnreviewedSQL, createdBefore, reason)
	if err != nil {
		return 0, fmt.Errorf("hand over unreviewed schedule posters: %w", err)
	}

	return tag.RowsAffected(), nil
}

func (s *Store) AutoConfirm(ctx context.Context, id, expectedRevision int64, weekStart, weekEnd time.Time) (WeekCoverage, int64, error) {
	if err := ValidateWeek(weekStart, weekEnd); err != nil {
		return WeekUncovered, 0, err
	}

	var (
		coverage WeekCoverage
		revision int64
	)

	err := s.withChannelLock(ctx, id, func(tx pgx.Tx) error {
		if err := lockUnreviewed(ctx, tx, id); err != nil {
			return err
		}

		var same, overlapping bool

		if err := tx.QueryRow(ctx, weekCoverageSQL, id, weekStart, weekEnd).Scan(&same, &overlapping); err != nil {
			return fmt.Errorf("load confirmed schedule poster weeks: %w", err)
		}

		var err error

		switch {
		case same:
			coverage = WeekSame
			revision, err = mutate(ctx, tx, "expire schedule poster", expireCandidateSQL, id, expectedRevision)
		case overlapping:
			coverage = WeekOverlapping
		default:
			revision, err = changeWeek(ctx, tx, confirmSQL, id, expectedRevision, weekStart, weekEnd, ReviewerAuto)
		}

		return err
	})
	if err != nil {
		return coverage, 0, fmt.Errorf("auto confirm schedule poster: %w", err)
	}

	return coverage, revision, nil
}

func (s *Store) ExpirePending(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, expirePendingSQL, before)
	if err != nil {
		return 0, fmt.Errorf("expire schedule poster candidates: %w", err)
	}

	return tag.RowsAffected(), nil
}

func (s *Store) Purge(ctx context.Context, before time.Time, limit int) (int64, error) {
	tag, err := s.pool.Exec(ctx, purgeSQL, before, limit)
	if err != nil {
		return 0, fmt.Errorf("purge schedule posters: %w", err)
	}

	return tag.RowsAffected(), nil
}

func (s *Store) Summary(ctx context.Context) (Summary, error) {
	var (
		summary Summary
		latest  *int64
	)

	if err := s.pool.QueryRow(ctx, summarySQL).Scan(&summary.PendingCount, &latest); err != nil {
		return Summary{}, fmt.Errorf("load schedule poster summary: %w", err)
	}

	if latest != nil {
		summary.LatestCandidateID = *latest
	}

	return summary, nil
}

func (s *Store) List(ctx context.Context, status ReviewStatus, limit int) ([]Poster, error) {
	if !status.Valid() {
		return nil, fmt.Errorf("list schedule posters: invalid review status %q", status)
	}

	rows, err := s.pool.Query(ctx, listSQL, string(status), min(max(limit, 1), maxListLimit))
	if err != nil {
		return nil, fmt.Errorf("list schedule posters: %w", err)
	}

	posters, err := pgx.CollectRows(rows, scanPoster)
	if err != nil {
		return nil, fmt.Errorf("list schedule posters: %w", err)
	}

	return posters, nil
}

func (s *Store) Get(ctx context.Context, id int64) (Poster, error) {
	rows, err := s.pool.Query(ctx, getSQL, id)
	if err != nil {
		return Poster{}, fmt.Errorf("get schedule poster: %w", err)
	}

	poster, err := pgx.CollectExactlyOneRow(rows, scanPoster)
	if errors.Is(err, pgx.ErrNoRows) {
		return Poster{}, ErrNotFound
	}

	if err != nil {
		return Poster{}, fmt.Errorf("get schedule poster: %w", err)
	}

	return poster, nil
}

func (s *Store) Confirm(ctx context.Context, id, expectedRevision int64, weekStart, weekEnd time.Time) (int64, error) {
	if err := ValidateWeek(weekStart, weekEnd); err != nil {
		return 0, err
	}

	var revision int64

	err := s.withChannelLock(ctx, id, func(tx pgx.Tx) error {
		var err error

		revision, err = changeWeek(ctx, tx, confirmSQL, id, expectedRevision, weekStart, weekEnd, ReviewerAdmin)

		return err
	})
	if err != nil {
		return 0, fmt.Errorf("confirm schedule poster: %w", err)
	}

	return revision, nil
}

func (s *Store) UpdateWeek(ctx context.Context, id, expectedRevision int64, weekStart, weekEnd time.Time) (int64, error) {
	if err := ValidateWeek(weekStart, weekEnd); err != nil {
		return 0, err
	}

	var revision int64

	err := s.withChannelLock(ctx, id, func(tx pgx.Tx) error {
		var err error

		revision, err = changeWeek(ctx, tx, updateWeekSQL, id, expectedRevision, weekStart, weekEnd)
		if err != nil {
			return err
		}

		return deleteRows(ctx, tx, id)
	})
	if err != nil {
		return 0, fmt.Errorf("update schedule poster week: %w", err)
	}

	return revision, nil
}

func (s *Store) withChannelLock(ctx context.Context, id int64, fn func(tx pgx.Tx) error) error {
	err := dbx.InPgxTxOptions(ctx, s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		if err := lockChannelConfirm(ctx, tx, id); err != nil {
			return err
		}

		return fn(tx)
	})
	if err != nil {
		return fmt.Errorf("run under schedule poster channel lock: %w", err)
	}

	return nil
}

func (s *Store) AutoReject(ctx context.Context, id, expectedRevision int64) (int64, error) {
	var revision int64

	err := s.withChannelLock(ctx, id, func(tx pgx.Tx) error {
		if err := lockUnreviewed(ctx, tx, id); err != nil {
			return err
		}

		var err error

		revision, err = reject(ctx, tx, id, expectedRevision, ReviewerAuto)

		return err
	})
	if err != nil {
		return 0, fmt.Errorf("auto reject schedule poster: %w", err)
	}

	return revision, nil
}

func lockUnreviewed(ctx context.Context, tx pgx.Tx, id int64) error {
	var unreviewed bool

	err := tx.QueryRow(ctx, lockUnreviewedCandidateSQL, id).Scan(&unreviewed)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("lock schedule poster candidate: %w", err)
	case !unreviewed:
		return ErrInvalidState
	default:
		return nil
	}
}

func lockChannelConfirm(ctx context.Context, tx pgx.Tx, id int64) error {
	if _, err := tx.Exec(ctx, lockChannelConfirmSQL, id); err != nil {
		return fmt.Errorf("lock schedule poster channel: %w", err)
	}

	return nil
}

func changeWeek(
	ctx context.Context, tx pgx.Tx, query string, id, expectedRevision int64, weekStart, weekEnd time.Time, args ...any,
) (int64, error) {
	var (
		current    *int64
		changeable *bool
		nearPost   *bool
		updated    *int64
	)

	if err := tx.QueryRow(ctx, query, append([]any{id, expectedRevision, weekStart, weekEnd}, args...)...).
		Scan(&current, &changeable, &nearPost, &updated); err != nil {
		return 0, fmt.Errorf("update schedule poster: %w", err)
	}

	revision, err := mutationOutcome(current, updated, expectedRevision)
	if errors.Is(err, ErrInvalidState) && changeable != nil && *changeable && nearPost != nil && !*nearPost {
		return 0, ErrInvalidWeek
	}

	return revision, err
}

func (s *Store) Reject(ctx context.Context, id, expectedRevision int64) (int64, error) {
	var revision int64

	err := s.withChannelLock(ctx, id, func(tx pgx.Tx) error {
		var err error

		revision, err = reject(ctx, tx, id, expectedRevision, ReviewerAdmin)

		return err
	})
	if err != nil {
		return 0, fmt.Errorf("reject schedule poster: %w", err)
	}

	return revision, nil
}

func reject(ctx context.Context, tx pgx.Tx, id, expectedRevision int64, reviewer Reviewer) (int64, error) {
	revision, err := mutate(ctx, tx, "update schedule poster", rejectSQL, id, expectedRevision, reviewer)
	if err != nil {
		return 0, err
	}

	if err := deleteRows(ctx, tx, id); err != nil {
		return 0, err
	}

	return revision, nil
}

func deleteRows(ctx context.Context, tx pgx.Tx, id int64) error {
	if _, err := tx.Exec(ctx, deleteRowsSQL, id); err != nil {
		return fmt.Errorf("delete schedule poster rows: %w", err)
	}

	return nil
}

func (s *Store) RequestRows(ctx context.Context, id, expectedRevision int64) (int64, error) {
	return s.mutate(ctx, "request schedule poster rows", requestRowsSQL, id, expectedRevision)
}

func (s *Store) mutate(ctx context.Context, action, query string, id, expectedRevision int64) (int64, error) {
	return mutate(ctx, s.pool, action, query, id, expectedRevision)
}

func mutate(ctx context.Context, db rowQuerier, action, query string, id, expectedRevision int64, args ...any) (int64, error) {
	var current, updated *int64

	if err := db.QueryRow(ctx, query, append([]any{id, expectedRevision}, args...)...).Scan(&current, &updated); err != nil {
		return 0, fmt.Errorf("%s: %w", action, err)
	}

	return mutationOutcome(current, updated, expectedRevision)
}

func mutationOutcome(current, updated *int64, expectedRevision int64) (int64, error) {
	switch {
	case current == nil:
		return 0, ErrNotFound
	case updated != nil:
		return *updated, nil
	case *current != expectedRevision:
		return 0, ErrRevisionConflict
	default:
		return 0, ErrInvalidState
	}
}

func scanPoster(row pgx.CollectableRow) (Poster, error) {
	var (
		poster        Poster
		reviewStatus  string
		reviewedBy    string
		requestReason string
		rowsStatus    string
	)

	err := row.Scan(
		&poster.ID, &poster.ChannelID, &poster.XUserID, &poster.PostURL, &poster.ImageURL, &poster.PostedAt,
		&poster.TextExcerpt, &reviewStatus, &poster.WeekStart, &poster.WeekEnd, &poster.ReviewedAt, &poster.Revision,
		&poster.CreatedAt, &poster.UpdatedAt, &reviewedBy, &requestReason,
		&rowsStatus, &poster.RowsError, &poster.RowsExtractedAt, &poster.RowsCount,
	)

	poster.ReviewStatus = ReviewStatus(reviewStatus)
	poster.ReviewedBy = Reviewer(reviewedBy)
	poster.ReviewRequestReason = ReviewRequestReason(requestReason)
	poster.RowsStatus = RowsStatus(rowsStatus)

	return poster, err
}
