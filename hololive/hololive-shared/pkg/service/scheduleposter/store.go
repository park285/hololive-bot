// Package scheduleposter의 쓰기는 admin plane만 하고, bot plane은 번역까지 끝난 확정본만 읽는다.
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

type TranslationStatus string

const (
	TranslationNotRequested TranslationStatus = "not_requested"
	TranslationPending      TranslationStatus = "pending"
	TranslationSucceeded    TranslationStatus = "succeeded"
	TranslationFailed       TranslationStatus = "failed"
)

const (
	// MaxImageBytes는 migration 010의 translated_image 크기 CHECK와 같다.
	MaxImageBytes = 4 << 20
	maxListLimit  = 100
	maxWeekDays   = 6
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

type Poster struct {
	Candidate

	ReviewStatus      ReviewStatus
	WeekStart         *time.Time
	WeekEnd           *time.Time
	ReviewedAt        *time.Time
	TranslationStatus TranslationStatus
	TranslationError  string
	TranslatedAt      *time.Time
	Revision          int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type Image struct {
	MIME string
	Data []byte
}

// PendingTranslation의 Revision이 그 사이 바뀌면 번역 결과를 기록하지 않는다.
type PendingTranslation struct {
	ID        int64
	ChannelID string
	ImageURL  string
	Revision  int64
}

type Summary struct {
	PendingCount int64
	// LatestCandidateID는 상태와 무관하게 가장 늦게 들어온 후보이므로 처리 뒤에도 뒤로 가지 않는다.
	LatestCandidateID int64
}

type Store struct {
	pool *pgxpool.Pool
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

func (s *Store) InsertCandidates(ctx context.Context, candidates []Candidate) (int64, error) {
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

	tag, err := s.pool.Exec(ctx, insertCandidatesSQL, ids, channels, users, posts, images, postedAt, excerpts)
	if err != nil {
		return 0, fmt.Errorf("insert schedule poster candidates: %w", err)
	}

	return tag.RowsAffected(), nil
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

// TranslatedImage는 번역 전이거나 실패한 행에도 ErrNotFound를 돌려준다.
func (s *Store) TranslatedImage(ctx context.Context, id int64) (Image, error) {
	return s.image(ctx, "load translated schedule poster", translatedImageSQL, id)
}

// PublishedImage의 day는 JST 날짜다.
func (s *Store) PublishedImage(ctx context.Context, channelID string, day time.Time) (Image, bool, error) {
	image, err := s.image(ctx, "load published schedule poster", publishedImageSQL, channelID, day)
	if errors.Is(err, ErrNotFound) {
		return Image{}, false, nil
	}

	if err != nil {
		return Image{}, false, err
	}

	return image, true, nil
}

func (s *Store) image(ctx context.Context, action, query string, args ...any) (Image, error) {
	var image Image

	err := s.pool.QueryRow(ctx, query, args...).Scan(&image.MIME, &image.Data)
	if errors.Is(err, pgx.ErrNoRows) {
		return Image{}, ErrNotFound
	}

	if err != nil {
		return Image{}, fmt.Errorf("%s: %w", action, err)
	}

	return image, nil
}

func (s *Store) Confirm(ctx context.Context, id, expectedRevision int64, weekStart, weekEnd time.Time) (int64, error) {
	if err := ValidateWeek(weekStart, weekEnd); err != nil {
		return 0, err
	}

	var (
		current  *int64
		status   *string
		nearPost *bool
		updated  *int64
	)

	if err := s.pool.QueryRow(ctx, confirmSQL, id, expectedRevision, weekStart, weekEnd).
		Scan(&current, &status, &nearPost, &updated); err != nil {
		return 0, fmt.Errorf("confirm schedule poster: %w", err)
	}

	revision, err := mutationOutcome(current, updated, expectedRevision)
	if errors.Is(err, ErrInvalidState) && status != nil && ReviewStatus(*status) == ReviewPending && nearPost != nil && !*nearPost {
		return 0, ErrInvalidWeek
	}

	return revision, err
}

// Reject는 확정본의 취소에도 쓰며 번역 결과를 함께 지운다.
func (s *Store) Reject(ctx context.Context, id, expectedRevision int64) (int64, error) {
	return s.mutate(ctx, "reject schedule poster", rejectSQL, id, expectedRevision)
}

// RequestTranslation은 이전 번역본을 지우므로 새 번역이 끝날 때까지 !일정에 포스터가 붙지 않는다.
func (s *Store) RequestTranslation(ctx context.Context, id, expectedRevision int64) (int64, error) {
	return s.mutate(ctx, "request schedule poster translation", requestTranslationSQL, id, expectedRevision)
}

func (s *Store) mutate(ctx context.Context, action, query string, id, expectedRevision int64) (int64, error) {
	var current, updated *int64

	if err := s.pool.QueryRow(ctx, query, id, expectedRevision).Scan(&current, &updated); err != nil {
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

func (s *Store) PendingTranslations(ctx context.Context, limit int) ([]PendingTranslation, error) {
	rows, err := s.pool.Query(ctx, pendingTranslationsSQL, min(max(limit, 1), maxListLimit))
	if err != nil {
		return nil, fmt.Errorf("list pending schedule poster translations: %w", err)
	}

	pending, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (PendingTranslation, error) {
		var item PendingTranslation

		return item, row.Scan(&item.ID, &item.ChannelID, &item.ImageURL, &item.Revision)
	})
	if err != nil {
		return nil, fmt.Errorf("list pending schedule poster translations: %w", err)
	}

	return pending, nil
}

// RecordTranslation은 그 사이 거절·재번역으로 revision이 바뀌었으면 false를 돌려준다.
func (s *Store) RecordTranslation(ctx context.Context, item PendingTranslation, image Image) (bool, error) {
	if len(image.Data) == 0 || len(image.Data) > MaxImageBytes || image.MIME == "" {
		return false, fmt.Errorf("record schedule poster translation: image must be 1..%d bytes with a media type", MaxImageBytes)
	}

	tag, err := s.pool.Exec(ctx, recordTranslationSQL, item.ID, item.Revision, image.Data, image.MIME)
	if err != nil {
		return false, fmt.Errorf("record schedule poster translation: %w", err)
	}

	return tag.RowsAffected() == 1, nil
}

func (s *Store) RecordTranslationFailure(ctx context.Context, item PendingTranslation, reason string) (bool, error) {
	tag, err := s.pool.Exec(ctx, recordTranslationFailureSQL, item.ID, item.Revision, reason)
	if err != nil {
		return false, fmt.Errorf("record schedule poster translation failure: %w", err)
	}

	return tag.RowsAffected() == 1, nil
}

func scanPoster(row pgx.CollectableRow) (Poster, error) {
	var (
		poster            Poster
		reviewStatus      string
		translationStatus string
	)

	err := row.Scan(
		&poster.ID, &poster.ChannelID, &poster.XUserID, &poster.PostURL, &poster.ImageURL, &poster.PostedAt,
		&poster.TextExcerpt, &reviewStatus, &poster.WeekStart, &poster.WeekEnd, &poster.ReviewedAt,
		&translationStatus, &poster.TranslationError, &poster.TranslatedAt, &poster.Revision,
		&poster.CreatedAt, &poster.UpdatedAt,
	)

	poster.ReviewStatus = ReviewStatus(reviewStatus)
	poster.TranslationStatus = TranslationStatus(translationStatus)

	return poster, err
}
