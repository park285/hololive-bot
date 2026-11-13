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
	// ImageSHA256은 썸네일 후보를 만들 때 받은 그림의 해시다. 같은 주소의 그림이 바뀔 수 있는 출처만 채운다.
	ImageSHA256 string
	Revision    int64
	// Inputs는 이 행의 지난 번역 시도가 다시 쓰라고 남긴 원본 판독과 번역표이며, 없으면 zero value다.
	Inputs TranslationInputs
}

// TranslationInputs는 번역 시도 한 번이 만든 원본 판독(Transcript)과 번역표(Table), 그리고 둘을 만든 입력의 sha256이다.
// 셋을 모두 채우거나 모두 비운다.
type TranslationInputs struct {
	SHA256     []byte
	Transcript []string
	Table      string
}

// ReviewCandidate는 자동 검토를 기다리는 검토 대기 후보다.
type ReviewCandidate struct {
	ID        int64
	ChannelID string
	ImageURL  string
	// ImageSHA256은 썸네일 후보만 채운다(PendingTranslation과 같다).
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
func (s *Store) RequestReview(ctx context.Context, id int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, requestReviewSQL, id)
	if err != nil {
		return false, fmt.Errorf("request schedule poster review: %w", err)
	}

	return tag.RowsAffected() == 1, nil
}

// HandOverUnreviewed는 createdBefore 전에 들어와 아직 관리자 검토로 넘기지 않은 검토 대기 후보를 모두 넘긴다.
func (s *Store) HandOverUnreviewed(ctx context.Context, createdBefore time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, handOverUnreviewedSQL, createdBefore)
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
			revision, err = confirm(ctx, tx, id, expectedRevision, weekStart, weekEnd)
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

// Confirm은 같은 멤버의 대기 후보 가운데 확정한 주의 마지막 날 전에 올라온 글을 함께 expired로 닫는다.
// 마지막 날은 다음 주 포스터를 미리 올리는 날이라 남긴다. InsertCandidates도 같은 범위의 새 후보를 받지 않는다.
func (s *Store) Confirm(ctx context.Context, id, expectedRevision int64, weekStart, weekEnd time.Time) (int64, error) {
	if err := ValidateWeek(weekStart, weekEnd); err != nil {
		return 0, err
	}

	var revision int64

	err := s.withChannelLock(ctx, id, func(tx pgx.Tx) error {
		var err error

		revision, err = confirm(ctx, tx, id, expectedRevision, weekStart, weekEnd)

		return err
	})
	if err != nil {
		return 0, fmt.Errorf("confirm schedule poster: %w", err)
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

		revision, err = mutate(ctx, tx, "update schedule poster", rejectSQL, id, expectedRevision)

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

func confirm(ctx context.Context, tx pgx.Tx, id, expectedRevision int64, weekStart, weekEnd time.Time) (int64, error) {
	var (
		current  *int64
		status   *string
		nearPost *bool
		updated  *int64
	)

	if err := tx.QueryRow(ctx, confirmSQL, id, expectedRevision, weekStart, weekEnd).
		Scan(&current, &status, &nearPost, &updated); err != nil {
		return 0, fmt.Errorf("update schedule poster: %w", err)
	}

	revision, err := mutationOutcome(current, updated, expectedRevision)
	if errors.Is(err, ErrInvalidState) && status != nil && ReviewStatus(*status) == ReviewPending && nearPost != nil && !*nearPost {
		return 0, ErrInvalidWeek
	}

	return revision, err
}

// Reject는 확정본의 취소에도 쓰며 번역 결과를 함께 지운다. 확정본 취소가 AutoConfirm의 판단 근거를 바꾸므로 Confirm과 같은 멤버 잠금 아래에서 한다.
func (s *Store) Reject(ctx context.Context, id, expectedRevision int64) (int64, error) {
	var revision int64

	err := s.withChannelLock(ctx, id, func(tx pgx.Tx) error {
		var err error

		revision, err = mutate(ctx, tx, "update schedule poster", rejectSQL, id, expectedRevision)

		return err
	})
	if err != nil {
		return 0, fmt.Errorf("reject schedule poster: %w", err)
	}

	return revision, nil
}

// RequestTranslation은 번역본을 지우지 않으므로 새 번역본이 저장될 때까지 이전 번역본이 !스케쥴로 게시된다.
func (s *Store) RequestTranslation(ctx context.Context, id, expectedRevision int64) (int64, error) {
	return s.mutate(ctx, "request schedule poster translation", requestTranslationSQL, id, expectedRevision)
}

func (s *Store) mutate(ctx context.Context, action, query string, id, expectedRevision int64) (int64, error) {
	return mutate(ctx, s.pool, action, query, id, expectedRevision)
}

func mutate(ctx context.Context, db rowQuerier, action, query string, id, expectedRevision int64) (int64, error) {
	var current, updated *int64

	if err := db.QueryRow(ctx, query, id, expectedRevision).Scan(&current, &updated); err != nil {
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

		return item, row.Scan(&item.ID, &item.ChannelID, &item.ImageURL, &item.ImageSHA256, &item.Revision,
			&item.Inputs.SHA256, &item.Inputs.Transcript, &item.Inputs.Table)
	})
	if err != nil {
		return nil, fmt.Errorf("list pending schedule poster translations: %w", err)
	}

	return pending, nil
}

// RecordTranslation은 남긴 판독·번역표를 지우고, 그 사이 거절·재번역으로 revision이 바뀌었으면 false를 돌려준다.
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

// RecordTranslationFailure는 남긴 판독·번역표를 inputs로 바꾸며, zero value면 지운다.
func (s *Store) RecordTranslationFailure(ctx context.Context, item PendingTranslation, reason string, inputs TranslationInputs) (bool, error) {
	tag, err := s.pool.Exec(ctx, recordTranslationFailureSQL, item.ID, item.Revision, reason, inputs.SHA256, inputs.Transcript, inputs.Table)
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
