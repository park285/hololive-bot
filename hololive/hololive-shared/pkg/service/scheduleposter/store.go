// Package scheduleposter의 쓰기는 admin plane만 하고, bot plane은 행 묶음을 저장한 확정본만 읽는다.
package scheduleposter

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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

// Reviewer는 확정·거절을 누가 했는지다. 관리자 검토로 넘기기 전에 자동 검토가 정했으면 auto다.
type Reviewer string

const (
	ReviewerAuto  Reviewer = "auto"
	ReviewerAdmin Reviewer = "admin"
)

// ReviewRequestReason은 자동 검토가 후보를 관리자 검토로 넘긴 이유이며 migration 018의 CHECK와 같은 값이다.
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
	// ErrInvalidWeek는 주 범위가 7일을 넘거나 게시일과 7일 넘게 떨어졌다는 뜻이다.
	ErrInvalidWeek = errors.New("schedule poster week is out of range")
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

// RowsStatus는 확정본의 일정표 행 추출 상태이며 migration 019의 CHECK와 같은 값이다. 확정본이 아니면 뜻이 없다.
type RowsStatus string

const (
	RowsPending   RowsStatus = "pending"
	RowsSucceeded RowsStatus = "succeeded"
	RowsFailed    RowsStatus = "failed"
)

type Poster struct {
	Candidate

	ReviewStatus ReviewStatus
	WeekStart    *time.Time
	WeekEnd      *time.Time
	ReviewedAt   *time.Time
	// ReviewedBy와 ReviewRequestReason은 값이 없으면 빈 문자열이다.
	ReviewedBy          Reviewer
	ReviewRequestReason ReviewRequestReason
	Revision            int64
	CreatedAt           time.Time
	UpdatedAt           time.Time
	RowsStatus          RowsStatus
	// RowsError는 마지막 추출 실패의 사유 코드이며 없으면 빈 문자열이다.
	RowsError string
	// RowsExtractedAt은 행 묶음을 마지막으로 저장한 시각이며, 재추출을 기다리거나 실패해도 이전 묶음과 함께 남는다.
	RowsExtractedAt *time.Time
	RowsCount       int
}

// ReviewCandidate는 자동 검토를 기다리는 검토 대기 후보다.
type ReviewCandidate struct {
	ID        int64
	ChannelID string
	ImageURL  string
	// ImageSHA256은 썸네일 후보만 채운다.
	ImageSHA256 string
	PostedAt    time.Time
	Revision    int64
}

// WeekCoverage는 같은 멤버의 확정본이 후보의 주 범위를 얼마나 덮는지다.
type WeekCoverage int

const (
	WeekUncovered WeekCoverage = iota
	// WeekSame은 관리자가 확정한 같은 주 범위의 확정본이 있고 후보가 그 수정본일 수 없다는 뜻이다(다른 출처의 같은 일정, 확정본보다 먼저 올린 글).
	WeekSame
	// WeekOverlapping은 겹치는 확정본이 있지만 후보를 닫을 근거가 없다는 뜻이다. 범위가 다르거나, 덮는 확정본이 자동 확정본이거나,
	// 후보가 같은 출처에서 확정본보다 늦게 올린 수정본일 수 있는 경우다.
	WeekOverlapping
)

// Summary는 관리자 검토로 넘긴 후보만 센다. 자동 검토를 기다리거나 자동 확정·거절한 후보는 Console 알림을 바꾸지 않는다.
type Summary struct {
	PendingCount int64
	// LatestCandidateID는 상태와 무관하게 가장 늦게 관리자 검토로 넘긴 후보이므로 처리 뒤에도 뒤로 가지 않는다.
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

// ValidateWeek는 길이만 본다. 게시일과의 거리는 Confirm이 DB에서 검사한다.
func ValidateWeek(start, end time.Time) error {
	days := int(end.Sub(start).Hours() / 24)
	if end.Before(start) || days > maxWeekDays {
		return ErrInvalidWeek
	}

	return nil
}

// InsertCandidates의 autoReview가 true면 넣은 후보를 관리자 검토로 넘기지 않고 자동 검토(UnreviewedCandidates)를 기다리게 둔다.
// 다른 Insert*Candidates도 같다.
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

// InsertCommunityCandidates는 youtube-collector가 저장한 커뮤니티 글 가운데 since 이후에 올라오고 textPattern(대소문자 무시)에 맞으며
// 이미지가 있는 글을 후보로 넣는다. 빈 textPattern은 본문과 상관없이 받는다. 확정된 주 범위는 InsertCandidates와 같은 규칙으로 뺀다.
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

// ThumbnailCandidate의 ImageSHA256은 수집 때 받은 그림의 sha256(소문자 16진수)이다.
type ThumbnailCandidate struct {
	ChannelID   string
	VideoID     string
	ImageSHA256 string
	PostedAt    time.Time
}

// InsertThumbnailCandidates는 유튜브 영상 썸네일을 후보로 넣는다. 확정된 주 범위는 InsertCandidates와 같은 규칙으로 뺀다.
func (s *Store) InsertThumbnailCandidates(ctx context.Context, candidates []ThumbnailCandidate, autoReview bool) (int64, error) {
	if len(candidates) == 0 {
		return 0, nil
	}

	channels := make([]string, len(candidates))
	videos := make([]string, len(candidates))
	hashes := make([]string, len(candidates))
	postedAt := make([]time.Time, len(candidates))

	for i, candidate := range candidates {
		channels[i] = candidate.ChannelID
		videos[i] = candidate.VideoID
		hashes[i] = candidate.ImageSHA256
		postedAt[i] = candidate.PostedAt
	}

	tag, err := s.pool.Exec(ctx, insertThumbnailCandidatesSQL, channels, videos, hashes, postedAt, autoReview)
	if err != nil {
		return 0, fmt.Errorf("insert schedule poster thumbnail candidates: %w", err)
	}

	return tag.RowsAffected(), nil
}

// UnreviewedCandidates는 관리자 검토로 넘기지 않은 검토 대기 후보를 최근 게시물부터 돌려준다.
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

// RequestReview는 자동 검토가 확정·거절하지 못한 후보를 관리자 검토로 넘기고, 이미 넘겼거나 검토 대기가 아니면 false를 돌려준다.
// 관리자의 진행 중 처리와 겹쳐도 되도록 revision은 바꾸지 않는다.
func (s *Store) RequestReview(ctx context.Context, id int64, reason ReviewRequestReason) (bool, error) {
	tag, err := s.pool.Exec(ctx, requestReviewSQL, id, reason)
	if err != nil {
		return false, fmt.Errorf("request schedule poster review: %w", err)
	}

	return tag.RowsAffected() == 1, nil
}

// HandOverUnreviewed는 createdBefore 전에 들어와 아직 관리자 검토로 넘기지 않은 검토 대기 후보를 모두 넘긴다.
func (s *Store) HandOverUnreviewed(ctx context.Context, createdBefore time.Time, reason ReviewRequestReason) (int64, error) {
	tag, err := s.pool.Exec(ctx, handOverUnreviewedSQL, createdBefore, reason)
	if err != nil {
		return 0, fmt.Errorf("hand over unreviewed schedule posters: %w", err)
	}

	return tag.RowsAffected(), nil
}

// AutoConfirm은 자동 검토가 읽은 주를 확정한다. 같은 멤버의 확정본이 그 주를 덮으면 확정하지 않고 덮는 정도를 돌려주며,
// WeekSame이면 후보를 expired로 닫는다. Confirm·Reject와 같은 멤버 잠금 아래에서 확인하고 바꾸므로 그 사이 다른 확정·거절이 끼지 않는다.
// 그 사이 관리자 검토로 넘어간 후보는 관리자가 정하므로 바꾸지 않고 ErrInvalidState다.
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

// Purge는 주 종료일(없으면 생성일)이 before보다 이른 행을 지우되 검토 대기 행은 남긴다.
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

// Confirm은 같은 멤버의 대기 후보 가운데 확정한 주의 마지막 날 전에 올라온 글을 함께 expired로 닫는다.
// 마지막 날은 다음 주 포스터를 미리 올리는 날이라 남긴다. InsertCandidates도 같은 범위의 새 후보를 받지 않는다.
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

// UpdateWeek은 확정본의 주 범위를 고치고 행 묶음을 지워 다시 뽑게 한다. Confirm과 달리 같은 멤버의 다른 후보를 닫지 않는다.
// AutoConfirm이 확정본의 주를 읽고 판단하므로 같은 멤버 잠금 아래에서 바꾼다.
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
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
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

// AutoReject는 자동 검토의 거절이다. AutoConfirm처럼 그 사이 관리자 검토로 넘어간 후보는 바꾸지 않고 ErrInvalidState다.
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

// lockUnreviewed는 없는 후보를 통과시켜 이어지는 변경이 ErrNotFound를 내게 한다.
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

// changeWeek의 query(confirm.sql, update_week.sql)는 변경 전 revision, 주 말고는 바꿀 수 있는 행인지, 게시일 7일 이내 여부와
// 바뀐 revision을 이 순서로 돌려줘야 한다.
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

// Reject는 확정본의 취소에도 쓰며 행 묶음을 함께 지운다. 확정본 취소가 AutoConfirm의 판단 근거를 바꾸므로 Confirm과 같은 멤버 잠금 아래에서 한다.
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

// RequestRows는 일정표 행만 다시 뽑게 예약하며 revision은 그대로 둔다. 새 묶음이 저장될 때까지 이전 묶음이 게시된다.
// 확정본이고 추출이 끝났으며(succeeded·failed) 주가 끝나지 않았을 때만 받고, 아니면 ErrInvalidState다.
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

// mutationOutcome은 CAS UPDATE가 바꾸지 못한 이유를 같은 문장이 읽은 변경 전 행으로 판정한다.
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
